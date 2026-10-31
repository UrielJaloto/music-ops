//go:build e2e || live

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/UrielJaloto/music-ops/domain"
)

type fakeLastFM struct {
	server     *httptest.Server
	mutex      sync.Mutex
	trackTags  map[string][]string
	artistTags map[string][]string
	isFailing  bool
	calls      atomic.Int64
}

func newFakeLastFM(t *testing.T) *fakeLastFM {
	t.Helper()
	fake := &fakeLastFM{trackTags: map[string][]string{}, artistTags: map[string][]string{}}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	t.Cleanup(fake.server.Close)
	return fake
}

func (fake *fakeLastFM) setTrackTags(artist, title string, tags ...string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.trackTags[strings.ToLower(artist+"|"+title)] = tags
}

func (fake *fakeLastFM) setArtistTags(artist string, tags ...string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.artistTags[strings.ToLower(artist)] = tags
}

func (fake *fakeLastFM) handle(writer http.ResponseWriter, request *http.Request) {
	fake.calls.Add(1)
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	if fake.isFailing {
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
	query := request.URL.Query()
	tags := fake.artistTags[strings.ToLower(query.Get("artist"))]
	if query.Get("method") == "track.gettoptags" {
		tags = fake.trackTags[strings.ToLower(query.Get("artist")+"|"+query.Get("track"))]
	}
	items := make([]map[string]string, 0, len(tags))
	for _, tag := range tags {
		items = append(items, map[string]string{"name": tag})
	}
	_ = json.NewEncoder(writer).Encode(map[string]any{"toptags": map[string]any{"tag": items}})
}

type oracleMode int

const (
	oracleCorrect oracleMode = iota
	oracleInventsArtist
	oracleMalformed
	oracleTruncated
)

type adviceOracle struct {
	mutex   sync.Mutex
	mode    oracleMode
	genres  map[string]map[string]any
	answers atomic.Int64
}

func newAdviceOracle() *adviceOracle {
	return &adviceOracle{genres: map[string]map[string]any{}}
}

func (oracle *adviceOracle) setMode(mode oracleMode) {
	oracle.mutex.Lock()
	defer oracle.mutex.Unlock()
	oracle.mode = mode
}

func (oracle *adviceOracle) currentMode() oracleMode {
	oracle.mutex.Lock()
	defer oracle.mutex.Unlock()
	return oracle.mode
}

func (oracle *adviceOracle) setGenre(title, genre, subgenre string, isBrazilian bool) {
	oracle.mutex.Lock()
	defer oracle.mutex.Unlock()
	oracle.genres[strings.ToLower(title)] = map[string]any{"genre": genre, "subgenre": subgenre, "is_brazilian": isBrazilian, "is_known": true}
}

func (oracle *adviceOracle) answer(isGenreTask bool, userMessage string) string {
	oracle.answers.Add(1)
	var message struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal([]byte(userMessage), &message)
	items := make([]map[string]any, 0, len(message.Items))
	for _, item := range message.Items {
		items = append(items, oracle.answerItem(isGenreTask, item))
	}
	encoded, _ := json.Marshal(map[string]any{"items": items})
	return string(encoded)
}

func (oracle *adviceOracle) answerItem(isGenreTask bool, item map[string]any) map[string]any {
	if isGenreTask {
		return oracle.genreAnswer(item)
	}
	return oracle.creditAnswer(item)
}

func (oracle *adviceOracle) creditAnswer(item map[string]any) map[string]any {
	source := domain.CreditSource{Path: text(item["path"]), Title: text(item["title"]), Artist: text(item["artist"]), AlbumArtist: text(item["album_artist"])}
	credit, err := domain.CreditFromRules(source)
	if err != nil {
		return map[string]any{"key": item["key"], "title": source.Title, "artists": []string{}}
	}
	artists := credit.Artists().Artists()
	if oracle.currentMode() == oracleInventsArtist {
		artists = append(artists, "Invented Star")
	}
	return map[string]any{"key": item["key"], "title": credit.Title().String(), "artists": artists}
}

func (oracle *adviceOracle) genreAnswer(item map[string]any) map[string]any {
	oracle.mutex.Lock()
	known, isKnown := oracle.genres[strings.ToLower(text(item["title"]))]
	oracle.mutex.Unlock()
	if isKnown {
		return withKey(known, item["key"])
	}
	tags := []string{}
	for _, tag := range item["tags"].([]any) {
		tags = append(tags, text(tag))
	}
	classification := domain.ClassifyByTags(domain.NewTagEvidence(tags))
	return withKey(classificationAnswer(classification), item["key"])
}

func classificationAnswer(classification domain.Classification) map[string]any {
	parts := strings.Split(classification.Genre().String(), "; ")
	subgenre := ""
	if len(parts) > 1 {
		subgenre = strings.TrimSuffix(parts[1], " Nacional")
	}
	return map[string]any{"genre": strings.TrimSuffix(parts[0], " Nacional"), "subgenre": subgenre, "is_brazilian": classification.IsBrazilian(), "is_known": classification.IsKnown()}
}

func withKey(answer map[string]any, key any) map[string]any {
	copied := map[string]any{"key": key}
	for name, value := range answer {
		copied[name] = value
	}
	return copied
}

func text(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

type fakeLlama struct {
	server *httptest.Server
	oracle *adviceOracle
	calls  atomic.Int64
}

func newFakeLlama(t *testing.T, oracle *adviceOracle) *fakeLlama {
	t.Helper()
	fake := &fakeLlama{oracle: oracle}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	t.Cleanup(fake.server.Close)
	return fake
}

func (fake *fakeLlama) handle(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path == "/health" {
		writer.WriteHeader(http.StatusOK)
		return
	}
	fake.calls.Add(1)
	var body struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	raw, _ := io.ReadAll(request.Body)
	_ = json.Unmarshal(raw, &body)
	content, finish := fake.content(body.Messages[0].Content, body.Messages[1].Content)
	_ = json.NewEncoder(writer).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]any{"content": content}, "finish_reason": finish}}})
}

func (fake *fakeLlama) content(system, user string) (string, string) {
	switch fake.oracle.currentMode() {
	case oracleMalformed:
		return "Sure! Here are the answers.", "stop"
	case oracleTruncated:
		return `{"items":[`, "length"
	}
	return fake.oracle.answer(strings.Contains(system, "genre curator"), user), "stop"
}
