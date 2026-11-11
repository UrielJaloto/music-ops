package importing

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/internal/domain/albums"
	"github.com/UrielJaloto/music-ops/internal/domain/audio"
	"github.com/UrielJaloto/music-ops/internal/domain/credits"
	"github.com/UrielJaloto/music-ops/internal/domain/filename"
	"github.com/UrielJaloto/music-ops/internal/domain/genres"
	"github.com/UrielJaloto/music-ops/internal/domain/runs"
	"github.com/UrielJaloto/music-ops/internal/domain/tracks"
	"github.com/UrielJaloto/music-ops/internal/services/conversion"
	"github.com/UrielJaloto/music-ops/internal/services/discovery"
	"github.com/UrielJaloto/music-ops/internal/services/metadata"
	"github.com/UrielJaloto/music-ops/internal/services/publishing"
)

type sourcesStub struct {
	discovered discovery.DiscoveredSources
	err        error
}

func (stub sourcesStub) Discover(_ context.Context, input discovery.DiscoverInput) (discovery.DiscoveredSources, error) {
	if stub.err != nil {
		return discovery.DiscoveredSources{}, stub.err
	}
	input.Progress.Counted(len(stub.discovered.Tracks) + len(stub.discovered.Failed))
	for _, track := range stub.discovered.Tracks {
		input.Progress.Finished(track.Source.Path, false)
	}
	for _, failure := range stub.discovered.Failed {
		input.Progress.Finished(failure.Path, true)
	}
	return stub.discovered, nil
}

type converterFake struct {
	failing  map[tracks.TrackID]bool
	omitted  map[tracks.TrackID]bool
	repeated bool
	err      error
	calls    int
}

func (fake *converterFake) Convert(_ context.Context, input conversion.ConvertInput) (conversion.ConvertOutput, error) {
	fake.calls++
	if fake.err != nil {
		return conversion.ConvertOutput{}, fake.err
	}
	output := conversion.ConvertOutput{}
	for _, track := range input.Tracks {
		if fake.omitted[track.ID] {
			continue
		}
		input.Progress.Finished(track.Source, fake.failing[track.ID])
		if fake.failing[track.ID] {
			output.Failed = append(output.Failed, conversion.ConversionFailure{ID: track.ID, Source: track.Source, Reason: "encoder crashed"})
			continue
		}
		converted := conversion.ConvertedTrack{ID: track.ID, StagedAudio: "stage/" + string(track.ID), FinalStaged: "stage/" + string(track.ID) + ".final"}
		output.Converted = append(output.Converted, converted)
		if fake.repeated {
			output.Converted = append(output.Converted, converted)
		}
	}
	return output, nil
}

type resolverFake struct {
	err   error
	calls int
	input metadata.ResolveMetadataInput
}

func (fake *resolverFake) Resolve(_ context.Context, input metadata.ResolveMetadataInput) (metadata.ResolvedMetadata, error) {
	fake.calls++
	fake.input = input
	if fake.err != nil {
		return metadata.ResolvedMetadata{}, fake.err
	}
	resolved := metadata.ResolvedMetadata{Warnings: []string{"rules used"}}
	for _, track := range input.Tracks {
		tags := track.Source.Tags
		credit, err := credits.CreditSource{Path: track.Source.Path, Title: tags.Title, Artist: tags.Artist}.BuildCreditFromRules()
		if err != nil {
			resolved.Failed = append(resolved.Failed, metadata.MetadataFailure{ID: track.ID, Path: track.Source.Path, Reason: err.Error()})
			continue
		}
		resolved.Tracks = append(resolved.Tracks, metadata.ResolvedTrack{ID: track.ID, Credit: credit, Classification: genres.NewUnknownClassification()})
	}
	return resolved, nil
}

type publisherFake struct {
	findErr        error
	cancelAfter    int
	cancel         context.CancelFunc
	libraryTargets map[string]bool
	failing        map[tracks.TrackID]bool
	fatalAfter     int
	journal        map[tracks.TrackID]string
	admitErr       error
	strangers      []tracks.TrackID
	published      []publishing.PublishTrack
	writes         int
}

func newPublisherFake() *publisherFake {
	return newPublisherFakeWithJournal(map[tracks.TrackID]string{})
}

func newPublisherFakeWithJournal(journal map[tracks.TrackID]string) *publisherFake {
	return &publisherFake{libraryTargets: map[string]bool{}, failing: map[tracks.TrackID]bool{}, fatalAfter: -1, cancelAfter: -1, journal: journal}
}

func (fake *publisherFake) FindPublished(_ context.Context, input publishing.FindPublishedInput) (publishing.PublishedTracks, error) {
	if fake.findErr != nil {
		return publishing.PublishedTracks{}, fake.findErr
	}
	published := publishing.PublishedTracks{Targets: map[tracks.TrackID]string{}}
	for _, id := range input.IDs {
		if target, found := fake.journal[id]; found {
			published.Targets[id] = target
		}
	}
	return published, nil
}

func (fake *publisherFake) Admit(_ context.Context, input publishing.AdmitInput) (publishing.AdmittedTracks, error) {
	if fake.admitErr != nil {
		return publishing.AdmittedTracks{}, fake.admitErr
	}
	admitted := publishing.AdmittedTracks{Publishable: slices.Clone(fake.strangers)}
	for _, stranger := range fake.strangers {
		admitted.Duplicates = append(admitted.Duplicates, publishing.DuplicateTrack{ID: stranger, KeeperPath: "elsewhere"})
	}
	for _, track := range input.Tracks {
		if fake.libraryTargets[track.Target.Relative()] {
			admitted.Duplicates = append(admitted.Duplicates, publishing.DuplicateTrack{ID: track.ID, KeeperPath: input.Output + "/" + track.Target.Relative()})
			continue
		}
		admitted.Publishable = append(admitted.Publishable, track.ID)
	}
	return admitted, nil
}

func (fake *publisherFake) Publish(ctx context.Context, input publishing.PublishInput) (publishing.PublishOutput, error) {
	if err := ctx.Err(); err != nil {
		return publishing.PublishOutput{}, err
	}
	if fake.fatalAfter >= 0 && fake.writes >= fake.fatalAfter {
		return publishing.PublishOutput{}, errTechnical
	}
	if _, isPublished := fake.journal[input.Track.ID]; isPublished {
		return publishing.PublishOutput{Status: publishing.PublishStatusAlreadyPublished}, nil
	}
	if fake.failing[input.Track.ID] {
		return publishing.PublishOutput{Status: publishing.PublishStatusFailed, Stage: publishing.PublishStageVerify, Reason: "bitrate mismatch"}, nil
	}
	fake.writes++
	fake.published = append(fake.published, input.Track)
	fake.journal[input.Track.ID] = input.Track.Target
	if fake.cancelAfter >= 0 && fake.writes >= fake.cancelAfter {
		fake.cancel()
	}
	return publishing.PublishOutput{Status: publishing.PublishStatusPublished}, nil
}

type progressSpy struct {
	events []string
}

func (spy *progressSpy) StageStarted(stage string, total int) {
	spy.events = append(spy.events, fmt.Sprintf("start %s %d", stage, total))
}

func (spy *progressSpy) TrackFinished(stage, path, status string) {
	spy.events = append(spy.events, fmt.Sprintf("track %s %s %s", stage, path, status))
}

func (spy *progressSpy) StageFinished(stage string) {
	spy.events = append(spy.events, "finish "+stage)
}

type runtimeFake struct {
	now      time.Time
	step     time.Duration
	progress *progressSpy
	idErr    error
}

func newRuntimeFake() *runtimeFake {
	return &runtimeFake{now: time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC), step: time.Second, progress: &progressSpy{}}
}

func (fake *runtimeFake) RunParallel(ctx context.Context, size int, task func(ctx context.Context, index int) error) error {
	return sequentialPool{}.Run(ctx, size, task)
}

func (fake *runtimeFake) Now() time.Time {
	fake.now = fake.now.Add(fake.step)
	return fake.now
}

func (fake *runtimeFake) NewRunID(startedAt time.Time) (runs.RunID, error) {
	if fake.idErr != nil {
		return "", fake.idErr
	}
	return runs.NewRunID(startedAt, "abcd")
}

func (fake *runtimeFake) Progress() ImportProgress {
	return fake.progress
}

type importDoubles struct {
	sources   sourcesStub
	converter *converterFake
	resolver  *resolverFake
	publisher *publisherFake
	runtime   *runtimeFake
}

func newImportDoubles(t *testing.T) importDoubles {
	t.Helper()
	return importDoubles{
		sources:   sourcesStub{discovered: discoveredFixture(t)},
		converter: &converterFake{failing: map[tracks.TrackID]bool{}, omitted: map[tracks.TrackID]bool{}},
		resolver:  &resolverFake{},
		publisher: newPublisherFake(),
		runtime:   newRuntimeFake(),
	}
}

func (doubles importDoubles) service() ImportService {
	return NewImportService(doubles.sources, doubles.converter, doubles.resolver, doubles.publisher, doubles.runtime)
}

type trackSpec struct {
	id        string
	directory string
	file      string
	title     string
	number    int
}

func discoveredTrack(t *testing.T, spec trackSpec) discovery.DiscoveredTrack {
	t.Helper()
	return discovery.DiscoveredTrack{ID: tracks.TrackID(spec.id), Source: tracks.SourceDetails{
		Path: "in/" + spec.directory + "/" + spec.file, RelativePath: spec.directory + "/" + spec.file, Directory: "in/" + spec.directory,
		Codec: "flac", Quality: testQuality(t),
		Tags: tracks.SourceTags{Title: spec.title, Artist: "Band", Album: "Album", Position: tracks.SourcePosition{TrackNumber: spec.number}},
	}}
}

func discoveredFixture(t *testing.T) discovery.DiscoveredSources {
	t.Helper()
	return discovery.DiscoveredSources{
		Tracks: []discovery.DiscoveredTrack{
			discoveredTrack(t, trackSpec{"a1", "album", "01.flac", "First (feat. Guest)", 1}),
			discoveredTrack(t, trackSpec{"a2", "album", "02.flac", "Second", 2}),
			discoveredTrack(t, trackSpec{"b1", "single", "x.flac", "Single", 0}),
		},
		Ignored: []string{"in/cover.jpg"},
		Failed:  []discovery.SourceFailure{{Path: "in/broken.flac", Reason: "invalid data"}},
	}
}

func importInput() ImportInput {
	return ImportInput{Folder: "in", Output: "out", Profile: audio.NewEncodingProfile(audio.EngineFDK), Numbering: albums.NumberingPerDirectory, Layout: filename.LayoutFlat}
}

func rowsByStatus(rows []ImportRow) map[string]int {
	counts := map[string]int{}
	for _, row := range rows {
		counts[row.Status]++
	}
	return counts
}

func findRow(t *testing.T, rows []ImportRow, source string) ImportRow {
	t.Helper()
	index := slices.IndexFunc(rows, func(row ImportRow) bool { return row.Source == source })
	if index < 0 {
		t.Fatalf("row %s not found in %+v", source, rows)
	}
	return rows[index]
}

func TestImportService_Import_HappyPath_PublishesAllTracks(t *testing.T) {
	doubles := newImportDoubles(t)

	output, err := doubles.service().Import(context.Background(), importInput())
	if err != nil {
		t.Fatal(err)
	}
	expectedCounts := runs.RunCounts{Published: 3, Skipped: 1, Failed: 1, Total: 5}
	if output.Counts != expectedCounts || len(doubles.publisher.published) != 3 || output.RunID != "20261016-080001-abcd" {
		t.Fatalf("output = %+v", output)
	}
	first := findRow(t, output.Rows, "in/album/01.flac")
	expectedFirst := ImportRow{Source: "in/album/01.flac", Target: "in/Band - First (feat. Guest).m4a", Title: "First", Artist: "Band; Guest", Genre: "desconhecido", Status: "published"}
	if first != expectedFirst {
		t.Fatalf("row = %+v, want %+v", first, expectedFirst)
	}
	if findRow(t, output.Rows, "in/cover.jpg").Status != "skipped" || findRow(t, output.Rows, "in/broken.flac").Reason != "discover: invalid data" {
		t.Fatalf("rows = %+v", output.Rows)
	}
	if !slices.Equal(output.Warnings, []string{"rules used"}) || output.Input != "in" || output.Output != "out" || output.Duration <= 0 {
		t.Fatalf("output = %+v", output)
	}
}

func TestImportService_Import_PublishRequest(t *testing.T) {
	doubles := newImportDoubles(t)

	_, err := doubles.service().Import(context.Background(), importInput())

	request := doubles.publisher.published[0]
	if err != nil || request.StagedAudio != "stage/a1" || request.FinalStaged != "stage/a1.final" || request.CoverSource != "in/album/01.flac" {
		t.Fatalf("request = %+v err = %v", request, err)
	}
	if request.Metadata.Track != "1/2" || request.Bitrate != 256000 || !request.IsReencoded || request.Duration != time.Minute {
		t.Fatalf("request = %+v", request)
	}
}

func TestImportService_Import_Validation(t *testing.T) {
	cases := []struct {
		name          string
		folder        string
		output        string
		expectedError error
	}{
		{name: "MissingFolder_ReturnsErrFolderMissing", folder: "", output: "out", expectedError: discovery.ErrFolderMissing},
		{name: "MissingOutput_ReturnsErrFolderMissing", folder: "in", output: " ", expectedError: discovery.ErrFolderMissing},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			doubles := newImportDoubles(t)
			input := importInput()
			input.Folder, input.Output = testCase.folder, testCase.output

			_, err := doubles.service().Import(context.Background(), input)

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("error = %v, want %v", err, testCase.expectedError)
			}
		})
	}
}

func TestImportService_Import_ConversionFailure_TrackFailedOthersPublished(t *testing.T) {
	doubles := newImportDoubles(t)
	doubles.converter.failing["a2"] = true

	output, err := doubles.service().Import(context.Background(), importInput())

	if err != nil || output.Counts.Published != 2 || output.Counts.Failed != 2 {
		t.Fatalf("counts = %+v err = %v", output.Counts, err)
	}
	if row := findRow(t, output.Rows, "in/album/02.flac"); row.Status != "failed" || row.Reason != "convert: encoder crashed" {
		t.Fatalf("row = %+v", row)
	}
}

func TestImportService_Import_PublishFailure_IsRecorded(t *testing.T) {
	doubles := newImportDoubles(t)
	doubles.publisher.failing["b1"] = true

	output, err := doubles.service().Import(context.Background(), importInput())

	if row := findRow(t, output.Rows, "in/single/x.flac"); err != nil || row.Status != "failed" || row.Reason != "publish:verify: bitrate mismatch" {
		t.Fatalf("row = %+v err = %v", row, err)
	}
}

func TestImportService_Import_InfrastructureErrors(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(*importDoubles)
	}{
		{name: "DiscoverError_ReturnsWrappedError", arrange: func(doubles *importDoubles) { doubles.sources.err = errTechnical }},
		{name: "ConverterError_ReturnsWrappedError", arrange: func(doubles *importDoubles) { doubles.converter.err = errTechnical }},
		{name: "MetadataError_ReturnsWrappedError", arrange: func(doubles *importDoubles) { doubles.resolver.err = errTechnical }},
		{name: "AdmitError_ReturnsWrappedError", arrange: func(doubles *importDoubles) { doubles.publisher.admitErr = errTechnical }},
		{name: "RunIDError_ReturnsWrappedError", arrange: func(doubles *importDoubles) { doubles.runtime.idErr = errTechnical }},
		{name: "FatalPublishError_ReturnsWrappedError", arrange: func(doubles *importDoubles) { doubles.publisher.fatalAfter = 0 }},
		{name: "FindPublishedError_ReturnsWrappedError", arrange: func(doubles *importDoubles) { doubles.publisher.findErr = errTechnical }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			doubles := newImportDoubles(t)
			testCase.arrange(&doubles)

			_, err := doubles.service().Import(context.Background(), importInput())

			if !errors.Is(err, errTechnical) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestImportService_Import_DuplicateInLibrary_NotPublished(t *testing.T) {
	doubles := newImportDoubles(t)
	doubles.publisher.libraryTargets["in/Band - Second.m4a"] = true

	output, err := doubles.service().Import(context.Background(), importInput())

	row := findRow(t, output.Rows, "in/album/02.flac")
	if err != nil || row.Status != "duplicate" || row.Reason != "out/in/Band - Second.m4a" || len(doubles.publisher.published) != 2 {
		t.Fatalf("row = %+v published = %d err = %v", row, len(doubles.publisher.published), err)
	}
}

func TestImportService_Import_ProgressEventsInOrder(t *testing.T) {
	doubles := newImportDoubles(t)
	doubles.sources.discovered = discovery.DiscoveredSources{Tracks: []discovery.DiscoveredTrack{discoveredTrack(t, trackSpec{"a1", "album", "01.flac", "Song", 1})}}

	_, err := doubles.service().Import(context.Background(), importInput())

	expected := []string{
		"start discover 0", "start discover 1", "track discover in/album/01.flac discovered", "finish discover",
		"start convert 1", "track convert in/album/01.flac converted", "finish convert",
		"start metadata 1", "track metadata in/album/01.flac ready", "finish metadata",
		"start publish 1", "track publish in/album/01.flac published", "finish publish",
	}
	if err != nil || !slices.Equal(doubles.runtime.progress.events, expected) {
		t.Fatalf("events = %v err = %v", doubles.runtime.progress.events, err)
	}
}

func TestImportService_Import_ExitStatus(t *testing.T) {
	cases := []struct {
		name     string
		failing  bool
		expected int
	}{{name: "ExitStatusZeroWhenNoneFailed", expected: 0}, {name: "ExitStatusTwoWhenAnyFailed", failing: true, expected: 2}}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			doubles := newImportDoubles(t)
			doubles.sources.discovered.Failed = nil
			doubles.converter.failing["a1"] = testCase.failing

			output, err := doubles.service().Import(context.Background(), importInput())

			if err != nil || output.ExitStatus != testCase.expected {
				t.Fatalf("exit = %d err = %v", output.ExitStatus, err)
			}
		})
	}
}

func TestImportService_Import_NumberingAppliedPerDirectory(t *testing.T) {
	doubles := newImportDoubles(t)

	_, err := doubles.service().Import(context.Background(), importInput())

	tracks := map[string]string{}
	for _, published := range doubles.publisher.published {
		tracks[published.Target] = published.Metadata.Track
	}
	expected := map[string]string{"in/Band - First (feat. Guest).m4a": "1/2", "in/Band - Second.m4a": "2/2", "in/Band - Single.m4a": "1/1"}
	if err != nil || fmt.Sprint(tracks) != fmt.Sprint(expected) {
		t.Fatalf("tracks = %v err = %v", tracks, err)
	}
}

func TestImportService_Import_KeepSourceWithoutNumber_FailsGroup(t *testing.T) {
	doubles := newImportDoubles(t)
	input := importInput()
	input.Numbering = albums.NumberingKeepSource

	output, err := doubles.service().Import(context.Background(), input)

	if row := findRow(t, output.Rows, "in/single/x.flac"); err != nil || row.Status != "failed" || row.Reason != "numbering: "+tracks.ErrTrackPositionOutOfRange.Error() {
		t.Fatalf("row = %+v err = %v", row, err)
	}
}

func TestImportService_Import_UnknownTemplate_FailsTargets(t *testing.T) {
	doubles := newImportDoubles(t)
	input := importInput()
	input.Layout, input.AlbumTemplate = filename.LayoutAlbumFolders, "{genre}"

	output, err := doubles.service().Import(context.Background(), input)

	if err != nil || output.Counts.Published != 0 || findRow(t, output.Rows, "in/single/x.flac").Reason != "target: "+filename.ErrTemplatePlaceholderUnknown.Error() {
		t.Fatalf("output = %+v err = %v", output, err)
	}
}

func TestImportService_Preview_ReturnsPlannedTargets_PublisherAndConverterNotCalled(t *testing.T) {
	doubles := newImportDoubles(t)

	preview, err := doubles.service().Preview(context.Background(), importInput())

	if err != nil || doubles.converter.calls != 0 || len(doubles.publisher.published) != 0 || doubles.resolver.calls != 1 {
		t.Fatalf("converter = %d published = %d err = %v", doubles.converter.calls, len(doubles.publisher.published), err)
	}
	if row := findRow(t, preview.Rows, "in/album/02.flac"); row.Status != RowStatusPlanned || row.Target != "in/Band - Second.m4a" {
		t.Fatalf("row = %+v", row)
	}
	if counts := rowsByStatus(preview.Rows); counts[RowStatusPlanned] != 3 || counts["failed"] != 1 || counts["skipped"] != 1 {
		t.Fatalf("counts = %v", counts)
	}
}

func TestImportService_Preview_Errors(t *testing.T) {
	cases := []struct {
		name          string
		arrange       func(*importDoubles, *ImportInput)
		expectedError error
	}{
		{name: "MissingFolder", arrange: func(_ *importDoubles, input *ImportInput) { input.Folder = "" }, expectedError: discovery.ErrFolderMissing},
		{name: "ResolverError", arrange: func(doubles *importDoubles, _ *ImportInput) { doubles.resolver.err = errTechnical }, expectedError: errTechnical},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			doubles := newImportDoubles(t)
			input := importInput()
			testCase.arrange(&doubles, &input)

			_, err := doubles.service().Preview(context.Background(), input)

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestImportService_SecondRun_WritesNothing(t *testing.T) {
	doubles := newImportDoubles(t)
	_, err := doubles.service().Import(context.Background(), importInput())
	if err != nil {
		t.Fatal(err)
	}
	second := newImportDoubles(t)
	second.publisher = newPublisherFakeWithJournal(doubles.publisher.journal)
	for _, published := range doubles.publisher.published {
		second.publisher.libraryTargets[published.Target] = true
	}

	output, err := second.service().Import(context.Background(), importInput())

	if err != nil || second.converter.calls != 1 || second.publisher.writes != 0 {
		t.Fatalf("converter calls = %d writes = %d err = %v", second.converter.calls, second.publisher.writes, err)
	}
	if output.Counts != (runs.RunCounts{Skipped: 4, Failed: 1, Total: 5}) {
		t.Fatalf("counts = %+v", output.Counts)
	}
	if row := findRow(t, output.Rows, "in/album/01.flac"); row.Status != "skipped" || row.Reason != "already published" {
		t.Fatalf("row = %+v", row)
	}
}

func TestImportService_SecondRun_ConvertsNothing(t *testing.T) {
	doubles := newImportDoubles(t)
	doubles.publisher.journal = map[tracks.TrackID]string{"a1": "x", "a2": "y", "b1": "z"}

	_, err := doubles.service().Import(context.Background(), importInput())

	if err != nil || doubles.converter.calls != 1 || doubles.publisher.writes != 0 {
		t.Fatalf("calls = %d writes = %d err = %v", doubles.converter.calls, doubles.publisher.writes, err)
	}
}

func TestImportService_ResumeAfterCrash_PublishesRemainingOnce(t *testing.T) {
	journal := map[tracks.TrackID]string{}
	first := newImportDoubles(t)
	first.publisher = newPublisherFakeWithJournal(journal)
	first.publisher.fatalAfter = 1

	_, err := first.service().Import(context.Background(), importInput())
	if !errors.Is(err, errTechnical) || first.publisher.writes != 1 {
		t.Fatalf("first run err = %v writes = %d", err, first.publisher.writes)
	}
	second := newImportDoubles(t)
	second.publisher = newPublisherFakeWithJournal(journal)

	output, err := second.service().Import(context.Background(), importInput())

	if err != nil || second.publisher.writes != 2 || output.Counts.Published != 2 || output.Counts.Skipped != 2 {
		t.Fatalf("second run writes = %d counts = %+v err = %v", second.publisher.writes, output.Counts, err)
	}
	if len(journal) != 3 {
		t.Fatalf("journal = %v", journal)
	}
}

func TestImportService_CancelMidPublish_ReturnsContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	doubles := newImportDoubles(t)
	doubles.publisher.cancelAfter = 1
	doubles.publisher.cancel = cancel

	_, err := doubles.service().Import(ctx, importInput())

	if !errors.Is(err, context.Canceled) || len(doubles.publisher.journal) != 1 || len(doubles.publisher.published) != 1 {
		t.Fatalf("err = %v journal = %v published = %d", err, doubles.publisher.journal, len(doubles.publisher.published))
	}
}

func TestImportService_Import_NativeEngineSkipsBitrateCheck(t *testing.T) {
	doubles := newImportDoubles(t)
	input := importInput()
	input.Profile = audio.NewEncodingProfile(audio.EngineNative)

	_, err := doubles.service().Import(context.Background(), input)

	request := doubles.publisher.published[0]
	if err != nil || request.Bitrate != 512000 || request.IsReencoded {
		t.Fatalf("request = %+v err = %v", request, err)
	}
}

func TestImportService_Import_ClockGoesBack_ReturnsFinishError(t *testing.T) {
	doubles := newImportDoubles(t)
	doubles.runtime.step = -time.Second

	_, err := doubles.service().Import(context.Background(), importInput())

	if !errors.Is(err, runs.ErrRunFinishBeforeStart) || err.Error() != "finishing run: "+runs.ErrRunFinishBeforeStart.Error() {
		t.Fatalf("err = %v", err)
	}
}

func TestImportService_Import_SourceWithoutPath_IsIgnoredWithWarning(t *testing.T) {
	doubles := newImportDoubles(t)
	doubles.sources.discovered.Tracks = append(doubles.sources.discovered.Tracks, discovery.DiscoveredTrack{ID: "empty"})

	output, err := doubles.service().Import(context.Background(), importInput())

	expected := fmt.Sprintf("ignored source %q: %v", "", tracks.ErrTrackPathMissing)
	if err != nil || !slices.Contains(output.Warnings, expected) || output.Counts.Total != 5 {
		t.Fatalf("warnings = %v counts = %+v err = %v", output.Warnings, output.Counts, err)
	}
}

func TestImportService_Import_MetadataFailure_FailsTrack(t *testing.T) {
	doubles := newImportDoubles(t)
	doubles.sources.discovered.Tracks[2].Source.Tags.Title = ""

	output, err := doubles.service().Import(context.Background(), importInput())

	row := findRow(t, output.Rows, "in/single/x.flac")
	if err != nil || row.Status != "failed" || !strings.HasPrefix(row.Reason, StageMetadata+": ") || output.Counts.Failed != 2 {
		t.Fatalf("row = %+v counts = %+v err = %v", row, output.Counts, err)
	}
}

func TestImportService_Import_RepeatedConversion_IsWarned(t *testing.T) {
	doubles := newImportDoubles(t)
	doubles.converter.repeated = true

	output, err := doubles.service().Import(context.Background(), importInput())

	expected := fmt.Sprintf("%s: %s: %v", StageConvert, "in/album/01.flac", tracks.ErrTrackTransitionForbidden)
	if err != nil || !slices.Contains(output.Warnings, expected) || output.Counts.Published != 3 {
		t.Fatalf("warnings = %v counts = %+v err = %v", output.Warnings, output.Counts, err)
	}
}

func TestImportService_Import_TrackMissingFromConversion_FailsAtTarget(t *testing.T) {
	doubles := newImportDoubles(t)
	doubles.converter.omitted["b1"] = true

	output, err := doubles.service().Import(context.Background(), importInput())

	row := findRow(t, output.Rows, "in/single/x.flac")
	expectedReason := stageTarget + ": " + tracks.ErrTrackTransitionForbidden.Error()
	if err != nil || row.Status != "failed" || row.Reason != expectedReason || output.Counts.Published != 2 {
		t.Fatalf("row = %+v counts = %+v err = %v", row, output.Counts, err)
	}
}

func TestImportService_Import_UnknownAdmittedTracks_AreIgnored(t *testing.T) {
	doubles := newImportDoubles(t)
	doubles.publisher.strangers = []tracks.TrackID{"ghost"}

	output, err := doubles.service().Import(context.Background(), importInput())

	if err != nil || output.Counts.Published != 3 || len(doubles.publisher.published) != 3 {
		t.Fatalf("counts = %+v err = %v", output.Counts, err)
	}
}

var errTechnical = errors.New("technical failure")

type sequentialPool struct{}

func (sequentialPool) Run(ctx context.Context, size int, task func(ctx context.Context, index int) error) error {
	for index := range size {
		if err := task(ctx, index); err != nil {
			return err
		}
	}
	return nil
}

func testQuality(t *testing.T) audio.AudioQuality {
	t.Helper()
	quality, err := audio.NewAudioQuality(audio.AudioQualityDetails{SampleRate: 96000, BitDepth: 24, Channels: 2, Duration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	return quality
}
