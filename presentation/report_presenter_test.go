package presentation

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/domain"
	"github.com/UrielJaloto/music-ops/services"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

type memoryReportFile struct {
	bytes.Buffer
	closeErr error
}

func (file *memoryReportFile) Close() error { return file.closeErr }

type memoryReportDestination struct {
	files     map[string]*memoryReportFile
	createErr error
	closeErr  error
}

func newMemoryReportDestination() *memoryReportDestination {
	return &memoryReportDestination{files: map[string]*memoryReportFile{}}
}

func (destination *memoryReportDestination) Create(_ context.Context, runID, name string) (io.WriteCloser, error) {
	if destination.createErr != nil {
		return nil, destination.createErr
	}
	file := &memoryReportFile{closeErr: destination.closeErr}
	destination.files[runID+"/"+name] = file
	return file, nil
}

func (destination *memoryReportDestination) Location(runID, name string) string {
	return "E:/Music/.music-ops/runs/" + runID + "/" + name
}

func sampleImportOutput() services.ImportOutput {
	return services.ImportOutput{
		RunID: "20261016-083005-abcd", Input: "D:/Downloads/Album", Output: "E:/Music", ExitStatus: 2,
		Duration: 95*time.Second + 300*time.Millisecond,
		Counts:   domain.RunCounts{Published: 2, Duplicate: 1, Skipped: 1, Failed: 1, Total: 5},
		Timings:  []services.StageTiming{{Stage: "discover", Duration: 1200 * time.Millisecond}, {Stage: "convert", Duration: 61 * time.Second}},
		Warnings: []string{"no advisor available | rules only"},
		Rows: []services.ImportRow{
			{Source: "b.flac", Target: "Zeta - Song.m4a", Title: "Song", Artist: "Zeta", Genre: "Rock", Status: "published"},
			{Source: "a.flac", Target: "Alpha - Hit.m4a", Title: "Hit", Artist: "Alpha; Guest", Genre: "Pop", Status: "published"},
			{Source: "c.flac", Target: "Alpha - Hit.m4a", Title: "Hit", Artist: "Alpha", Genre: "Pop", Status: "duplicate", Reason: "kept a.flac"},
			{Source: "notes.txt", Status: "skipped", Reason: "not an audio file"},
			{Source: "bad.flac", Target: "Bad - Track.m4a", Title: "A | B", Artist: "Bad", Status: "failed", Reason: "decode error\nffmpeg exited"},
		},
	}
}

func assertGolden(t *testing.T, name, actual string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateGolden {
		if err := os.WriteFile(path, []byte(actual), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if expected := readGolden(t, name); actual != expected {
		t.Fatalf("%s mismatch:\n%s", name, actual)
	}
}

func TestReportPresenter_Golden(t *testing.T) {
	destination := newMemoryReportDestination()

	path, err := NewReportPresenter(destination).Present(context.Background(), sampleImportOutput())

	if err != nil || path != "E:/Music/.music-ops/runs/20261016-083005-abcd/report.md" {
		t.Fatalf("path = %s err = %v", path, err)
	}
	assertGolden(t, "report.md.golden", destination.files["20261016-083005-abcd/report.md"].String())
	assertGolden(t, "report.json.golden", destination.files["20261016-083005-abcd/report.json"].String())
}

func TestReportPresenter_EscapesPipesAndNewlines(t *testing.T) {
	destination := newMemoryReportDestination()

	_, err := NewReportPresenter(destination).Present(context.Background(), sampleImportOutput())

	markdown := destination.files["20261016-083005-abcd/report.md"].String()
	if err != nil || !strings.Contains(markdown, `A \| B`) || !strings.Contains(markdown, "decode error ffmpeg exited") || !strings.Contains(markdown, `no advisor available \| rules only`) {
		t.Fatalf("markdown:\n%s", markdown)
	}
}

func TestReportPresenter_FailedFirstThenByTarget(t *testing.T) {
	tracks := toReportTracks(sampleImportOutput().Rows)

	order := make([]string, 0, len(tracks))
	for _, track := range tracks {
		order = append(order, track.Status+":"+track.Target)
	}
	expected := "failed:Bad - Track.m4a,skipped:,published:Alpha - Hit.m4a,duplicate:Alpha - Hit.m4a,published:Zeta - Song.m4a"
	if strings.Join(order, ",") != expected {
		t.Fatalf("order = %v", order)
	}
}

func TestReportPresenter_EmptyRun(t *testing.T) {
	destination := newMemoryReportDestination()

	_, err := NewReportPresenter(destination).Present(context.Background(), services.ImportOutput{RunID: "empty"})

	assertGolden(t, "report_empty.md.golden", destination.files["empty/report.md"].String())
	if err != nil || !strings.Contains(destination.files["empty/report.json"].String(), `"tracks": []`) {
		t.Fatalf("json = %s err = %v", destination.files["empty/report.json"].String(), err)
	}
}

func TestReportPresenter_DestinationErrors(t *testing.T) {
	errDisk := errors.New("disk full")
	cases := []struct {
		name        string
		destination *memoryReportDestination
	}{
		{name: "CreateFails", destination: &memoryReportDestination{files: map[string]*memoryReportFile{}, createErr: errDisk}},
		{name: "CloseFails", destination: &memoryReportDestination{files: map[string]*memoryReportFile{}, closeErr: errDisk}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path, err := NewReportPresenter(testCase.destination).Present(context.Background(), sampleImportOutput())

			if !errors.Is(err, errDisk) || path != "" {
				t.Fatalf("path = %s err = %v", path, err)
			}
		})
	}
}
