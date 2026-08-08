//go:build eval

package infrastructure

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/UrielJaloto/music-ops/domain"
	"github.com/UrielJaloto/music-ops/services"
)

const (
	evalBatchSize     = 40
	evalMinimumSpeed  = 5.0
	evalMaximumVRAMMB = 7680
	evalPort          = 8095
)

type evalCandidate struct {
	name     string
	artifact services.ArtifactRequest
}

var evalCandidates = []evalCandidate{
	{name: "Qwen3.5-9B Q4_K_M", artifact: BuildAdvisorModelArtifact()},
	{name: "Qwen3.5-4B Q8_0", artifact: services.ArtifactRequest{
		Name:      "Qwen3.5-4B-Q8_0.gguf",
		URL:       "https://huggingface.co/unsloth/Qwen3.5-4B-GGUF/resolve/e87f176479d0855a907a41277aca2f8ee7a09523/Qwen3.5-4B-Q8_0.gguf",
		SHA256:    "10cc391b403021dd11c614679d2fd92f611c3681d29e29651b717316965d61e1",
		SizeBytes: 4482403488,
	}},
	{name: "Qwen3-8B Q4_K_M", artifact: services.ArtifactRequest{
		Name:      "Qwen3-8B-Q4_K_M.gguf",
		URL:       "https://huggingface.co/unsloth/Qwen3-8B-GGUF/resolve/a6adef130ffb23ddaf1a62fec9dced968c9bc482/Qwen3-8B-Q4_K_M.gguf",
		SHA256:    "120307ba529eb2439d6c430d94104dabd578497bc7bfe7e322b5d9933b449bd4",
		SizeBytes: 5027784512,
	}},
}

type silentProgress struct{}

func (silentProgress) ArtifactProgressed(string, int64) {}

func prepareEvalArtifacts(t *testing.T, cache string) {
	t.Helper()
	downloader := NewHTTPArtifactDownloader(&http.Client{}, silentProgress{})
	runtime := filepath.Join(cache, "llama.cpp")
	if _, err := os.Stat(filepath.Join(runtime, AdvisorServerExecutable)); err != nil {
		for _, artifact := range BuildAdvisorRuntimeArtifacts() {
			archive := filepath.Join(cache, artifact.Name)
			if err := downloader.Download(context.Background(), artifact, archive); err != nil {
				t.Fatal(err)
			}
			if err := NewZipExtractor().Extract(context.Background(), archive, runtime); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, candidate := range evalCandidates {
		if err := downloader.Download(context.Background(), candidate.artifact, filepath.Join(cache, candidate.artifact.Name)); err != nil {
			t.Fatalf("%s: %v", candidate.name, err)
		}
	}
}

type genreCase struct {
	request  services.GenreRequest
	expected string
}

type creditCase struct {
	request         services.CreditRequest
	expectedTitle   string
	expectedArtists []string
}

type evalResult struct {
	candidate       string
	genreExact      float64
	mainGenre       float64
	brazilian       float64
	creditExact     float64
	rejectionRate   float64
	tracksPerSecond float64
	peakVRAMMB      int
}

func TestAdvisorEvaluation(t *testing.T) {
	library, isSet := os.LookupEnv("MUSIC_OPS_EVAL_LIBRARY")
	if !isSet || library == "" {
		t.Fatal("MUSIC_OPS_EVAL_LIBRARY is required")
	}
	cache := evalCacheFolder(t)
	prepareEvalArtifacts(t, cache)
	genres, credits := buildEvalDataset(t, library, cache)
	t.Logf("dataset: %d genre cases, %d credit cases", len(genres), len(credits))
	results := []evalResult{}
	for _, candidate := range evalCandidates {
		results = append(results, evaluateCandidate(t, cache, candidate, genres, credits))
	}
	report := renderEvalReport(results, len(genres), len(credits))
	t.Log("\n" + report)
	if err := os.WriteFile(filepath.Join(cache, "advisor-eval.md"), []byte(report), 0o600); err != nil {
		t.Fatal(err)
	}
}

func evalCacheFolder(t *testing.T) string {
	t.Helper()
	root, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, fixtureCacheFolder, "advisor")
}

func buildEvalDataset(t *testing.T, library, cache string) ([]genreCase, []creditCase) {
	t.Helper()
	_, ffprobe := requireFFmpeg(t)
	inspector := NewFFprobeInspector(NewProcessRunner(), ffprobe)
	tags := evalTagSource(t, cache)
	genres := []genreCase{}
	credits := []creditCase{}
	paths := libraryTracks(t, library)
	for index, path := range paths {
		record, err := inspector.probe(context.Background(), path)
		if err != nil {
			continue
		}
		raw := lowerTagKeys(record.Format.Tags, nil)
		relative, _ := filepath.Rel(library, path)
		artists := strings.Split(raw["artist"], "; ")
		key := "t" + strconv.Itoa(index)
		if genre := normalizedGenre(raw["genre"]); genre != "" {
			lastfm, _ := tags.FetchTrackTags(context.Background(), artists[0], raw["title"])
			genres = append(genres, genreCase{request: services.GenreRequest{Key: key, Path: filepath.ToSlash(relative), Title: raw["title"], Artists: artists, Tags: lastfm}, expected: genre})
		}
		if len(artists) > 1 {
			credits = append(credits, synthesizedCredit(key, relative, raw["title"], artists))
		}
	}
	return genres, credits
}

func libraryTracks(t *testing.T, library string) []string {
	t.Helper()
	paths := []string{}
	err := filepath.WalkDir(library, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && strings.EqualFold(filepath.Ext(path), ".m4a") {
			paths = append(paths, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func evalTagSource(t *testing.T, cache string) TagSource {
	t.Helper()
	key, _ := os.LookupEnv("MUSIC_OPS_LASTFM_API_KEY")
	store, err := OpenBoltStore(filepath.Join(cache, "eval.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	client := NewLastFMClient(LastFMSettings{APIKey: key, BaseURL: "https://ws.audioscrobbler.com/2.0/", Version: "eval"}, &http.Client{Timeout: 15 * time.Second}, rate.NewLimiter(4, 1), NewExponentialBackoff(func() float64 { return 0.5 }, NewSystemClock()))
	return NewLastFMCache(client, NewBoltCache(store, NewSystemClock()))
}

func normalizedGenre(tag string) string {
	parts := strings.Split(strings.TrimSpace(tag), "; ")
	for index, part := range parts {
		if strings.HasSuffix(strings.ToLower(part), " nacional") {
			parts[index] = part[:len(part)-len(" nacional")] + " Nacional"
		}
	}
	return strings.Join(parts, "; ")
}

func synthesizedCredit(key, relative, title string, artists []string) creditCase {
	source := domain.CreditSource{Path: filepath.ToSlash(relative), Title: title + " (feat. " + strings.Join(artists[1:], " & ") + ")", Artist: artists[0]}
	return creditCase{request: services.CreditRequest{Key: key, Source: source}, expectedTitle: title, expectedArtists: artists}
}

func evaluateCandidate(t *testing.T, cache string, candidate evalCandidate, genres []genreCase, credits []creditCase) evalResult {
	t.Helper()
	server := NewLlamaServer(NewProcessRunner(), LlamaServerSettings{
		Executable: filepath.Join(cache, "llama.cpp", "llama-server.exe"), ModelFile: filepath.Join(cache, candidate.artifact.Name),
		Port: evalPort, HTTP: &http.Client{Timeout: 2 * time.Second}, Clock: NewSystemClock(),
	})
	session, err := server.Start(context.Background())
	if err != nil {
		t.Fatalf("%s: %v", candidate.name, err)
	}
	defer func() { _ = session.Stop(context.Background()) }()
	advisor := NewLlamaAdvisor(LlamaAdvisorSettings{BaseURL: "http://127.0.0.1:" + strconv.Itoa(evalPort), Model: candidate.name, LoadingTimeout: 60 * time.Second, Clock: NewSystemClock()}, &http.Client{Timeout: 10 * time.Minute})
	monitor := startVRAMMonitor()
	result := evalResult{candidate: candidate.name}
	started := time.Now()
	genreSuggestions, genreRejected := runGenreBatches(t, advisor, genres)
	result.tracksPerSecond = float64(len(genres)) / time.Since(started).Seconds()
	creditSuggestions, creditRejected := runCreditBatches(t, advisor, credits)
	result.peakVRAMMB = monitor.stop()
	scoreGenres(&result, genres, genreSuggestions)
	result.creditExact = scoreCredits(credits, creditSuggestions)
	result.rejectionRate = ratio(genreRejected+creditRejected, len(genres)+len(credits))
	t.Logf("%+v", result)
	return result
}

func runGenreBatches(t *testing.T, advisor LlamaAdvisor, cases []genreCase) (map[string]services.GenreSuggestion, int) {
	t.Helper()
	answers := map[string]services.GenreSuggestion{}
	rejected := 0
	for start := 0; start < len(cases); start += evalBatchSize {
		batch := []services.GenreRequest{}
		for _, item := range cases[start:min(start+evalBatchSize, len(cases))] {
			batch = append(batch, item.request)
		}
		suggestions, err := advisor.SuggestGenres(context.Background(), batch)
		if err != nil {
			t.Logf("genre batch %d: %v", start, err)
			continue
		}
		for _, suggestion := range suggestions {
			if !ValidGenreSuggestion(services.GenreRequest{}, suggestion) {
				rejected++
				continue
			}
			answers[suggestion.Key] = suggestion
		}
	}
	return answers, rejected
}

func runCreditBatches(t *testing.T, advisor LlamaAdvisor, cases []creditCase) (map[string]services.CreditSuggestion, int) {
	t.Helper()
	answers := map[string]services.CreditSuggestion{}
	rejected := 0
	for start := 0; start < len(cases); start += evalBatchSize {
		batch := []services.CreditRequest{}
		byKey := map[string]services.CreditRequest{}
		for _, item := range cases[start:min(start+evalBatchSize, len(cases))] {
			batch = append(batch, item.request)
			byKey[item.request.Key] = item.request
		}
		suggestions, err := advisor.SuggestCredits(context.Background(), batch)
		if err != nil {
			t.Logf("credit batch %d: %v", start, err)
			continue
		}
		for _, suggestion := range suggestions {
			if !IsValidCreditSuggestion(byKey[suggestion.Key], suggestion) {
				rejected++
				continue
			}
			answers[suggestion.Key] = suggestion
		}
	}
	return answers, rejected
}

func scoreGenres(result *evalResult, cases []genreCase, answers map[string]services.GenreSuggestion) {
	exact, main, brazilian := 0, 0, 0
	for _, item := range cases {
		suggestion := answers[item.request.Key]
		classification, err := domain.NewClassification(domain.ClassificationDetails{Genre: suggestion.Genre, Subgenre: suggestion.Subgenre, IsBrazilian: suggestion.IsBrazilian, IsKnown: suggestion.IsKnown})
		if err != nil {
			continue
		}
		actual := classification.Genre().String()
		if actual == item.expected {
			exact++
		}
		if mainGenre(actual) == mainGenre(item.expected) {
			main++
		}
		if isBrazilianGenre(actual) == isBrazilianGenre(item.expected) {
			brazilian++
		}
	}
	result.genreExact = ratio(exact, len(cases))
	result.mainGenre = ratio(main, len(cases))
	result.brazilian = ratio(brazilian, len(cases))
}

func mainGenre(genre string) string {
	first, _, _ := strings.Cut(genre, "; ")
	return strings.TrimSuffix(first, " Nacional")
}

func isBrazilianGenre(genre string) bool {
	return strings.Contains(genre, "Nacional") || slices.Contains(domain.NationalOnlyGenreNames(), mainGenre(genre))
}

func scoreCredits(cases []creditCase, answers map[string]services.CreditSuggestion) float64 {
	exact := 0
	for _, item := range cases {
		suggestion, isAnswered := answers[item.request.Key]
		if isAnswered && suggestion.Title == item.expectedTitle && slices.Equal(suggestion.Artists, item.expectedArtists) {
			exact++
		}
	}
	return ratio(exact, len(cases))
}

func ratio(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total)
}

type vramMonitor struct {
	peak    atomic.Int64
	done    chan struct{}
	stopped sync.WaitGroup
}

func startVRAMMonitor() *vramMonitor {
	monitor := &vramMonitor{done: make(chan struct{})}
	monitor.stopped.Go(func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			monitor.sample()
			select {
			case <-monitor.done:
				return
			case <-ticker.C:
			}
		}
	})
	return monitor
}

func (monitor *vramMonitor) sample() {
	output, err := exec.CommandContext(context.Background(), "nvidia-smi", "--query-gpu=memory.used", "--format=csv,noheader,nounits").Output()
	if err != nil {
		return
	}
	used, err := strconv.ParseInt(strings.TrimSpace(string(bytes.Split(output, []byte("\n"))[0])), 10, 64)
	if err == nil && used > monitor.peak.Load() {
		monitor.peak.Store(used)
	}
}

func (monitor *vramMonitor) stop() int {
	close(monitor.done)
	monitor.stopped.Wait()
	return int(monitor.peak.Load())
}

func renderEvalReport(results []evalResult, genreCases, creditCases int) string {
	var report strings.Builder
	fmt.Fprintf(&report, "Advisor evaluation (%d genre cases, %d credit cases, batch %d)\n\n", genreCases, creditCases, evalBatchSize)
	report.WriteString("| Candidate | Genre exact | Main genre | Brazilian | Credit exact | Rejected | Tracks/s | Peak VRAM MB | Eligible |\n|---|---|---|---|---|---|---|---|---|\n")
	for _, result := range results {
		isEligible := result.tracksPerSecond >= evalMinimumSpeed && result.peakVRAMMB <= evalMaximumVRAMMB
		fmt.Fprintf(&report, "| %s | %.1f %% | %.1f %% | %.1f %% | %.1f %% | %.1f %% | %.2f | %d | %v |\n",
			result.candidate, 100*result.genreExact, 100*result.mainGenre, 100*result.brazilian, 100*result.creditExact, 100*result.rejectionRate, result.tracksPerSecond, result.peakVRAMMB, isEligible)
	}
	return report.String()
}
