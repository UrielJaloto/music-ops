package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/internal/domain/albums"
	"github.com/UrielJaloto/music-ops/internal/domain/audio"
	"github.com/UrielJaloto/music-ops/internal/domain/filename"
	"github.com/UrielJaloto/music-ops/internal/domain/runs"
	"github.com/UrielJaloto/music-ops/internal/domain/tracks"
	"github.com/UrielJaloto/music-ops/internal/services/discovery"
	"github.com/UrielJaloto/music-ops/internal/services/importing"
)

type importServiceSpy struct {
	output        importing.ImportOutput
	preview       importing.ImportPreviewOutput
	err           error
	importInputs  []importing.ImportInput
	previewInputs []importing.ImportInput
}

func (spy *importServiceSpy) Import(_ context.Context, input importing.ImportInput) (importing.ImportOutput, error) {
	spy.importInputs = append(spy.importInputs, input)
	return spy.output, spy.err
}

func (spy *importServiceSpy) Preview(_ context.Context, input importing.ImportInput) (importing.ImportPreviewOutput, error) {
	spy.previewInputs = append(spy.previewInputs, input)
	return spy.preview, spy.err
}

type fixedProfile audio.EncoderEngine

func (engine fixedProfile) SelectProfile() audio.EncodingProfile {
	return audio.NewEncodingProfile(audio.EncoderEngine(engine))
}

type finisherSpy struct {
	err   error
	calls int
}

func (spy *finisherSpy) Finish(context.Context, importing.ImportOutput) error {
	spy.calls++
	return spy.err
}

func testDefaults() ImportDefaults {
	return ImportDefaults{Output: "E:/Music", Profile: fixedProfile(audio.EngineFDK), Numbering: albums.NumberingPerDirectory, Layout: filename.LayoutFlat, AlbumTemplate: "{album}", Tags: tracks.TagOverrides{Album: "Fixed", Genre: "Rock"}}
}

type reporterStub struct {
	path  string
	err   error
	calls int
}

func (stub *reporterStub) Present(context.Context, importing.ImportOutput) (string, error) {
	stub.calls++
	return stub.path, stub.err
}

type controllerFixture struct {
	reporter *reporterStub
	service  *importServiceSpy
	finisher *finisherSpy
	output   *bytes.Buffer
}

func newImportFixture(opensFiles bool) (ImportController, controllerFixture) {
	fixture := controllerFixture{
		reporter: &reporterStub{path: "C:/Local/music-ops/runs/r1/report.md"},
		service:  &importServiceSpy{output: importing.ImportOutput{RunID: "r1", Output: "E:/Music", ExitStatus: 2}},
		finisher: &finisherSpy{},
		output:   &bytes.Buffer{},
	}
	presenter := NewImportPresenter(fixture.output, NewErrorPresenter(fixture.output))
	controller := NewImportController(ImportServices{Importer: fixture.service, Previewer: fixture.service, Reporter: fixture.reporter}, fixture.finisher, presenter, ImportSettings{Defaults: testDefaults(), OpensFiles: opensFiles})
	return controller, fixture
}

func TestImportController_Run_FolderFromArgument(t *testing.T) {
	controller, fixture := newImportFixture(true)

	code := NewRunCommand(controller).Execute(context.Background(), []string{"D:/In"})

	expected := importing.ImportInput{Folder: "D:/In", Output: "E:/Music", Profile: audio.NewEncodingProfile(audio.EngineFDK), Numbering: albums.NumberingPerDirectory, Layout: filename.LayoutFlat, AlbumTemplate: "{album}", Tags: tracks.TagOverrides{Album: "Fixed", Genre: "Rock"}}
	if diff := differences([]importing.ImportInput{expected}, fixture.service.importInputs); diff != "" {
		t.Fatalf("input (-want +got):\n%s", diff)
	}
	if code != 2 || fixture.finisher.calls != 1 || fixture.reporter.calls != 1 || !strings.Contains(fixture.output.String(), "Report: C:/Local/music-ops/runs/r1/report.md") {
		t.Fatalf("code = %d finisher = %d output = %q", code, fixture.finisher.calls, fixture.output.String())
	}
}

func TestImportController_Run_NoArgument_LeavesTheFolderToTheSession(t *testing.T) {
	controller, fixture := newImportFixture(true)

	code := controller.Run(context.Background(), nil)

	if code != 2 || fixture.service.importInputs[0].Folder != "" {
		t.Fatalf("code = %d inputs = %v", code, fixture.service.importInputs)
	}
}

func TestImportController_Run_ChoiceCancelled(t *testing.T) {
	controller, fixture := newImportFixture(true)
	fixture.service.err = importing.ErrFolderChoiceCancelled

	code := controller.Run(context.Background(), nil)

	if code != 0 || fixture.finisher.calls != 0 || fixture.output.String() != "no folder selected\n" {
		t.Fatalf("code = %d output = %q", code, fixture.output.String())
	}
}

func TestImportController_Run_ImportError(t *testing.T) {
	controller, fixture := newImportFixture(true)
	fixture.service.err = importing.ErrRunInProgress

	code := controller.Run(context.Background(), []string{"D:/In"})

	if code != 1 || fixture.finisher.calls != 0 || fixture.output.String() != "error: another music-ops run is in progress\n" {
		t.Fatalf("code = %d output = %q", code, fixture.output.String())
	}
}

func TestImportController_Run_NoOpenSkipsFinisher(t *testing.T) {
	controller, fixture := newImportFixture(false)

	code := controller.Run(context.Background(), []string{"D:/In"})

	if code != 2 || fixture.finisher.calls != 0 {
		t.Fatalf("code = %d finisher = %d", code, fixture.finisher.calls)
	}
}

func TestImportController_Run_FinisherFailureIsAWarning(t *testing.T) {
	controller, fixture := newImportFixture(true)
	fixture.finisher.err = errors.New("no shell")

	code := controller.Run(context.Background(), []string{"D:/In"})

	if code != 2 || !strings.HasSuffix(fixture.output.String(), "warning: could not open the report: no shell\n") {
		t.Fatalf("code = %d output = %q", code, fixture.output.String())
	}
}

func TestImportController_Run_ReportFailure(t *testing.T) {
	controller, fixture := newImportFixture(false)
	fixture.reporter = &reporterStub{err: errors.New("disk full")}
	controller.services.Reporter = fixture.reporter

	code := controller.Run(context.Background(), []string{"D:/In"})

	output := fixture.output.String()
	if code != 2 || !strings.HasPrefix(output, "warning: could not write the report: disk full\n") || !strings.Contains(output, "Report: not written") {
		t.Fatalf("code = %d output = %q", code, output)
	}
}

func TestImportController_Preview(t *testing.T) {
	controller, fixture := newImportFixture(true)
	fixture.service.preview = importing.ImportPreviewOutput{Rows: []importing.ImportRow{
		{Source: "a/01.flac", Target: "Artist - Song.m4a", Title: "Song", Artist: "Artist", Genre: "Rock", Status: "planned"},
		{Source: "a/02 long name.flac", Target: "Artist - Other Song.m4a", Title: "Other Song", Artist: "Artist; Guest", Genre: "Pop", Status: "planned"},
	}, Warnings: []string{"no advisor"}}

	code := NewPreviewCommand(controller).Execute(context.Background(), []string{"D:/In"})

	if code != 0 || len(fixture.service.importInputs) != 0 || fixture.finisher.calls != 0 || fixture.output.String() != readGolden(t, "preview.golden") {
		t.Fatalf("code = %d output:\n%s", code, fixture.output.String())
	}
}

func TestImportController_PreviewErrors(t *testing.T) {
	controller, fixture := newImportFixture(true)
	fixture.service.err = discovery.ErrOutputInsideInput
	cancelled, cancelledFixture := newImportFixture(true)
	cancelledFixture.service.err = importing.ErrFolderChoiceCancelled

	code := controller.Preview(context.Background(), []string{"D:/In"})
	cancelledCode := cancelled.Preview(context.Background(), nil)

	if code != 1 || cancelledCode != 0 || cancelledFixture.service.previewInputs[0].Folder != "" {
		t.Fatalf("codes = %d %d", code, cancelledCode)
	}
}

func TestImportPresenter_SummaryGolden(t *testing.T) {
	var output bytes.Buffer
	result := importing.ImportOutput{
		RunID: "20261016-083005-abcd", Output: "E:/Music", Duration: 72*time.Second + 400*time.Millisecond,
		Counts:   runs.RunCounts{Published: 10, Duplicate: 2, Skipped: 1, Failed: 3, Total: 16},
		Warnings: []string{"a", "b", "c", "d"},
	}

	NewImportPresenter(&output, NewErrorPresenter(&output)).Imported(result, "C:/Local/music-ops/runs/20261016-083005-abcd/report.md")

	if output.String() != readGolden(t, "summary.golden") {
		t.Fatalf("summary:\n%s", output.String())
	}
}

type steppedClock struct {
	mutex      sync.Mutex
	now        time.Time
	step       time.Duration
	ticks      chan time.Time
	afterCalls chan struct{}
}

func (clock *steppedClock) After(time.Duration) <-chan time.Time {
	if clock.afterCalls != nil {
		clock.afterCalls <- struct{}{}
	}
	return clock.ticks
}

func (clock *steppedClock) Now() time.Time {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	clock.now = clock.now.Add(clock.step)
	return clock.now
}

func TestProgressPresenter_NonTerminalGolden(t *testing.T) {
	var output bytes.Buffer
	progress := NewProgressPresenter(&output, &steppedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), step: 30 * time.Millisecond})

	progress.StageStarted(importing.StageDiscover, 0)
	progress.StageFinished(importing.StageDiscover)
	progress.StageStarted(importing.StageConvert, 10)
	for index := range 10 {
		status := "converted"
		if index%4 == 0 {
			status = "failed"
		}
		progress.TrackFinished(importing.StageConvert, "track", status)
	}
	progress.TrackFinished(importing.StagePublish, "ignored", "published")
	progress.StageFinished(importing.StageConvert)
	progress.StageFinished(importing.StagePublish)

	if output.String() != readGolden(t, "progress.golden") {
		t.Fatalf("progress:\n%s", output.String())
	}
}

func TestProgressPresenter_ConcurrentEvents(t *testing.T) {
	var output bytes.Buffer
	progress := NewProgressPresenter(&output, &steppedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), step: time.Millisecond})
	progress.StageStarted(importing.StagePublish, 1000)
	var group sync.WaitGroup

	for worker := range 8 {
		group.Go(func() {
			for index := worker; index < 1000; index += 8 {
				progress.TrackFinished(importing.StagePublish, "track", "published")
			}
		})
	}
	group.Wait()
	progress.StageFinished(importing.StagePublish)

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if !strings.Contains(lines[len(lines)-1], "1000/1000") {
		t.Fatalf("last line = %q", lines[len(lines)-1])
	}
}

func TestRenderProgressBar(t *testing.T) {
	cases := []struct {
		done, total int
		expected    string
	}{
		{done: 0, total: 10, expected: "░░░░░░░░░░"},
		{done: 5, total: 10, expected: "█████░░░░░"},
		{done: 10, total: 10, expected: "██████████"},
		{done: 11, total: 10, expected: "██████████"},
		{done: 0, total: 0, expected: "██████████"},
	}
	for _, testCase := range cases {
		if bar := renderProgressBar(testCase.done, testCase.total); bar != testCase.expected {
			t.Fatalf("renderProgressBar(%d, %d) = %s", testCase.done, testCase.total, bar)
		}
	}
}
