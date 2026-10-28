package presentation

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/UrielJaloto/music-ops/domain"
	"github.com/UrielJaloto/music-ops/services"
)

type importServiceSpy struct {
	output        services.ImportOutput
	preview       services.ImportPreviewOutput
	err           error
	importInputs  []services.ImportInput
	previewInputs []services.ImportInput
}

func (spy *importServiceSpy) Import(_ context.Context, input services.ImportInput) (services.ImportOutput, error) {
	spy.importInputs = append(spy.importInputs, input)
	return spy.output, spy.err
}

func (spy *importServiceSpy) Preview(_ context.Context, input services.ImportInput) (services.ImportPreviewOutput, error) {
	spy.previewInputs = append(spy.previewInputs, input)
	return spy.preview, spy.err
}

type chooserStub struct {
	folder string
	err    error
	calls  int
}

func (stub *chooserStub) ChooseFolder(context.Context, string, string) (string, error) {
	stub.calls++
	return stub.folder, stub.err
}

type finisherSpy struct {
	err   error
	calls int
}

func (spy *finisherSpy) Finish(context.Context, services.ImportOutput) error {
	spy.calls++
	return spy.err
}

func testDefaults() ImportDefaults {
	return ImportDefaults{Output: "E:/Music", Profile: domain.NewEncodingProfile(domain.EngineFDK), Numbering: domain.NumberingPerDirectory, Layout: domain.LayoutFlat, AlbumTemplate: "{album}"}
}

type controllerFixture struct {
	service  *importServiceSpy
	chooser  *chooserStub
	finisher *finisherSpy
	output   *bytes.Buffer
}

func newImportFixture(opensFiles bool) (ImportController, controllerFixture) {
	fixture := controllerFixture{
		service:  &importServiceSpy{output: services.ImportOutput{RunID: "r1", Output: "E:/Music", ExitStatus: 2}},
		chooser:  &chooserStub{folder: "D:/Chosen"},
		finisher: &finisherSpy{},
		output:   &bytes.Buffer{},
	}
	presenter := NewImportPresenter(fixture.output, NewErrorPresenter(fixture.output))
	controller := NewImportController(ImportServices{Importer: fixture.service, Previewer: fixture.service}, ImportDesktop{Chooser: fixture.chooser, Finisher: fixture.finisher}, presenter, ImportSettings{Defaults: testDefaults(), OpensFiles: opensFiles})
	return controller, fixture
}

func TestImportController_Run_FolderFromArgument(t *testing.T) {
	controller, fixture := newImportFixture(true)

	code := NewRunCommand(controller).Execute(context.Background(), []string{"D:/In"})

	expected := services.ImportInput{Folder: "D:/In", Output: "E:/Music", Profile: domain.NewEncodingProfile(domain.EngineFDK), Numbering: domain.NumberingPerDirectory, Layout: domain.LayoutFlat, AlbumTemplate: "{album}"}
	if diff := cmp.Diff([]services.ImportInput{expected}, fixture.service.importInputs, cmp.AllowUnexported(domain.EncodingProfile{})); diff != "" {
		t.Fatalf("input (-want +got):\n%s", diff)
	}
	if code != 2 || fixture.chooser.calls != 0 || fixture.finisher.calls != 1 || !strings.Contains(fixture.output.String(), "Run r1 finished") {
		t.Fatalf("code = %d chooser = %d finisher = %d output = %q", code, fixture.chooser.calls, fixture.finisher.calls, fixture.output.String())
	}
}

func TestImportController_Run_FolderFromChooser(t *testing.T) {
	controller, fixture := newImportFixture(true)

	code := controller.Run(context.Background(), nil)

	if code != 2 || fixture.chooser.calls != 1 || fixture.service.importInputs[0].Folder != "D:/Chosen" {
		t.Fatalf("code = %d inputs = %v", code, fixture.service.importInputs)
	}
}

func TestImportController_Run_ChooserCancelled(t *testing.T) {
	controller, fixture := newImportFixture(true)
	fixture.chooser.err = services.ErrFolderChoiceCancelled

	code := controller.Run(context.Background(), nil)

	if code != 0 || len(fixture.service.importInputs) != 0 || fixture.output.String() != "no folder selected\n" {
		t.Fatalf("code = %d output = %q", code, fixture.output.String())
	}
}

func TestImportController_Run_ImportError(t *testing.T) {
	controller, fixture := newImportFixture(true)
	fixture.service.err = services.ErrLibraryBusy

	code := controller.Run(context.Background(), []string{"D:/In"})

	if code != 1 || fixture.finisher.calls != 0 || fixture.output.String() != "error: another music-ops run is using this library\n" {
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

func TestImportController_Preview(t *testing.T) {
	controller, fixture := newImportFixture(true)
	fixture.service.preview = services.ImportPreviewOutput{Rows: []services.ImportRow{
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
	fixture.service.err = services.ErrOutputInsideInput
	failingChooser, chooserFixture := newImportFixture(true)
	chooserFixture.chooser.err = services.ErrFolderChoiceCancelled

	code := controller.Preview(context.Background(), []string{"D:/In"})
	chooserCode := failingChooser.Preview(context.Background(), nil)

	if code != 1 || chooserCode != 0 || len(chooserFixture.service.previewInputs) != 0 {
		t.Fatalf("codes = %d %d", code, chooserCode)
	}
}

func TestImportPresenter_SummaryGolden(t *testing.T) {
	var output bytes.Buffer
	result := services.ImportOutput{
		RunID: "20261016-083005-abcd", Output: "E:/Music", Duration: 72*time.Second + 400*time.Millisecond,
		Counts:   domain.RunCounts{Published: 10, Duplicate: 2, Skipped: 1, Failed: 3, Total: 16},
		Warnings: []string{"a", "b", "c", "d"},
	}

	NewImportPresenter(&output, NewErrorPresenter(&output)).Imported(result)

	if output.String() != strings.ReplaceAll(readGolden(t, "summary.golden"), "/", string(filepath.Separator)) {
		t.Fatalf("summary:\n%s", output.String())
	}
}

type steppedClock struct {
	mutex sync.Mutex
	now   time.Time
	step  time.Duration
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

	progress.StageStarted(services.StageDiscover, 0)
	progress.StageFinished(services.StageDiscover)
	progress.StageStarted(services.StageConvert, 10)
	for index := range 10 {
		status := "converted"
		if index%4 == 0 {
			status = "failed"
		}
		progress.TrackFinished(services.StageConvert, "track", status)
	}
	progress.TrackFinished(services.StagePublish, "ignored", "published")
	progress.StageFinished(services.StageConvert)
	progress.StageFinished(services.StagePublish)

	if output.String() != readGolden(t, "progress.golden") {
		t.Fatalf("progress:\n%s", output.String())
	}
}

func TestProgressPresenter_ConcurrentEvents(t *testing.T) {
	var output bytes.Buffer
	progress := NewProgressPresenter(&output, &steppedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), step: time.Millisecond})
	progress.StageStarted(services.StagePublish, 1000)
	var group sync.WaitGroup

	for worker := range 8 {
		group.Go(func() {
			for index := worker; index < 1000; index += 8 {
				progress.TrackFinished(services.StagePublish, "track", "published")
			}
		})
	}
	group.Wait()
	progress.StageFinished(services.StagePublish)

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if !strings.Contains(lines[len(lines)-1], "1000/1000") {
		t.Fatalf("last line = %q", lines[len(lines)-1])
	}
}

func TestProgressBar(t *testing.T) {
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
		if bar := progressBar(testCase.done, testCase.total); bar != testCase.expected {
			t.Fatalf("progressBar(%d, %d) = %s", testCase.done, testCase.total, bar)
		}
	}
}
