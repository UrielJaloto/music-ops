package services

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/domain"
)

type sourcesStub struct {
	discovered DiscoveredSources
	err        error
}

func (stub sourcesStub) Discover(context.Context, DiscoverInput) (DiscoveredSources, error) {
	return stub.discovered, stub.err
}

type converterFake struct {
	failing map[domain.TrackID]bool
	err     error
	calls   int
}

func (fake *converterFake) Convert(_ context.Context, input ConvertInput) (ConvertOutput, error) {
	fake.calls++
	if fake.err != nil {
		return ConvertOutput{}, fake.err
	}
	output := ConvertOutput{}
	for _, track := range input.Tracks {
		if fake.failing[track.ID] {
			output.Failed = append(output.Failed, ConversionFailure{ID: track.ID, Source: track.Source, Reason: "encoder crashed"})
			continue
		}
		output.Converted = append(output.Converted, ConvertedTrack{ID: track.ID, StagedAudio: "stage/" + string(track.ID), FinalStaged: "stage/" + string(track.ID) + ".final"})
	}
	return output, nil
}

type resolverFake struct {
	err   error
	calls int
}

func (fake *resolverFake) Resolve(_ context.Context, input ResolveMetadataInput) (ResolvedMetadata, error) {
	fake.calls++
	if fake.err != nil {
		return ResolvedMetadata{}, fake.err
	}
	resolved := ResolvedMetadata{Warnings: []string{"rules used"}}
	for _, track := range input.Tracks {
		tags := track.Source.Tags
		credit, err := domain.CreditFromRules(domain.CreditSource{Path: track.Source.Path, Title: tags.Title, Artist: tags.Artist})
		if err != nil {
			resolved.Failed = append(resolved.Failed, MetadataFailure{ID: track.ID, Path: track.Source.Path, Reason: err.Error()})
			continue
		}
		resolved.Tracks = append(resolved.Tracks, ResolvedTrack{ID: track.ID, Credit: credit, Classification: domain.UnknownClassification()})
	}
	return resolved, nil
}

type publisherFake struct {
	findErr        error
	cancelAfter    int
	cancel         context.CancelFunc
	libraryTargets map[string]bool
	failing        map[domain.TrackID]bool
	fatalAfter     int
	journal        map[domain.TrackID]string
	admitErr       error
	published      []PublishTrack
	writes         int
}

func newPublisherFake() *publisherFake {
	return newPublisherFakeWithJournal(map[domain.TrackID]string{})
}

func newPublisherFakeWithJournal(journal map[domain.TrackID]string) *publisherFake {
	return &publisherFake{libraryTargets: map[string]bool{}, failing: map[domain.TrackID]bool{}, fatalAfter: -1, cancelAfter: -1, journal: journal}
}

func (fake *publisherFake) FindPublished(_ context.Context, input FindPublishedInput) (PublishedTracks, error) {
	if fake.findErr != nil {
		return PublishedTracks{}, fake.findErr
	}
	published := PublishedTracks{Targets: map[domain.TrackID]string{}}
	for _, id := range input.IDs {
		if target, found := fake.journal[id]; found {
			published.Targets[id] = target
		}
	}
	return published, nil
}

func (fake *publisherFake) Admit(_ context.Context, input AdmitInput) (AdmittedTracks, error) {
	if fake.admitErr != nil {
		return AdmittedTracks{}, fake.admitErr
	}
	admitted := AdmittedTracks{}
	for _, track := range input.Tracks {
		if fake.libraryTargets[track.Target.Relative()] {
			admitted.Duplicates = append(admitted.Duplicates, DuplicateTrack{ID: track.ID, KeeperPath: input.Output + "/" + track.Target.Relative()})
			continue
		}
		admitted.Publishable = append(admitted.Publishable, track.ID)
	}
	return admitted, nil
}

func (fake *publisherFake) Publish(ctx context.Context, input PublishInput) (PublishOutput, error) {
	if err := ctx.Err(); err != nil {
		return PublishOutput{}, err
	}
	if fake.fatalAfter >= 0 && fake.writes >= fake.fatalAfter {
		return PublishOutput{}, errTechnical
	}
	if _, isPublished := fake.journal[input.Track.ID]; isPublished {
		return PublishOutput{Status: PublishStatusAlreadyPublished}, nil
	}
	if fake.failing[input.Track.ID] {
		return PublishOutput{Status: PublishStatusFailed, Stage: PublishStageVerify, Reason: "bitrate mismatch"}, nil
	}
	fake.writes++
	fake.published = append(fake.published, input.Track)
	fake.journal[input.Track.ID] = input.Track.Target
	if fake.cancelAfter >= 0 && fake.writes >= fake.cancelAfter {
		fake.cancel()
	}
	return PublishOutput{Status: PublishStatusPublished}, nil
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
	progress *progressSpy
	idErr    error
}

func newRuntimeFake() *runtimeFake {
	return &runtimeFake{now: time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC), progress: &progressSpy{}}
}

func (fake *runtimeFake) RunParallel(ctx context.Context, size int, task func(ctx context.Context, index int) error) error {
	return sequentialPool{}.Run(ctx, size, task)
}

func (fake *runtimeFake) Now() time.Time {
	fake.now = fake.now.Add(time.Second)
	return fake.now
}

func (fake *runtimeFake) NewRunID(startedAt time.Time) (domain.RunID, error) {
	if fake.idErr != nil {
		return "", fake.idErr
	}
	return domain.NewRunID(startedAt, "abcd")
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
		converter: &converterFake{failing: map[domain.TrackID]bool{}},
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

func discoveredTrack(t *testing.T, spec trackSpec) DiscoveredTrack {
	t.Helper()
	return DiscoveredTrack{ID: domain.TrackID(spec.id), Source: domain.SourceDetails{
		Path: "in/" + spec.directory + "/" + spec.file, RelativePath: spec.directory + "/" + spec.file, Directory: "in/" + spec.directory,
		Codec: "flac", Quality: testQuality(t),
		Tags: domain.SourceTags{Title: spec.title, Artist: "Band", Album: "Album", Position: domain.SourcePosition{TrackNumber: spec.number}},
	}}
}

func discoveredFixture(t *testing.T) DiscoveredSources {
	t.Helper()
	return DiscoveredSources{
		Tracks: []DiscoveredTrack{
			discoveredTrack(t, trackSpec{"a1", "album", "01.flac", "First (feat. Guest)", 1}),
			discoveredTrack(t, trackSpec{"a2", "album", "02.flac", "Second", 2}),
			discoveredTrack(t, trackSpec{"b1", "single", "x.flac", "Single", 0}),
		},
		Ignored: []string{"in/cover.jpg"},
		Failed:  []SourceFailure{{Path: "in/broken.flac", Reason: "invalid data"}},
	}
}

func importInput() ImportInput {
	return ImportInput{Folder: "in", Output: "out", Profile: domain.NewEncodingProfile(domain.EngineFDK), Numbering: domain.NumberingPerDirectory, Layout: domain.LayoutFlat}
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
	expectedCounts := domain.RunCounts{Published: 3, Skipped: 1, Failed: 1, Total: 5}
	if output.Counts != expectedCounts || len(doubles.publisher.published) != 3 || output.RunID != "20261016-080001-abcd" {
		t.Fatalf("output = %+v", output)
	}
	first := findRow(t, output.Rows, "in/album/01.flac")
	expectedFirst := ImportRow{Source: "in/album/01.flac", Target: "Band - First (feat. Guest).m4a", Title: "First", Artist: "Band; Guest", Genre: "desconhecido", Status: "published"}
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
		{name: "MissingFolder_ReturnsErrFolderMissing", folder: "", output: "out", expectedError: ErrFolderMissing},
		{name: "MissingOutput_ReturnsErrFolderMissing", folder: "in", output: " ", expectedError: ErrFolderMissing},
		{name: "OutputInsideInput_ReturnsErrOutputInsideInput", folder: `C:\Music\In`, output: "c:/music/in/out", expectedError: ErrOutputInsideInput},
		{name: "OutputEqualsInput_ReturnsErrOutputInsideInput", folder: "in/", output: "IN", expectedError: ErrOutputInsideInput},
		{name: "SiblingWithSamePrefix_IsAccepted", folder: "in", output: "inbox", expectedError: nil},
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
	doubles.publisher.libraryTargets["Band - Second.m4a"] = true

	output, err := doubles.service().Import(context.Background(), importInput())

	row := findRow(t, output.Rows, "in/album/02.flac")
	if err != nil || row.Status != "duplicate" || row.Reason != "out/Band - Second.m4a" || len(doubles.publisher.published) != 2 {
		t.Fatalf("row = %+v published = %d err = %v", row, len(doubles.publisher.published), err)
	}
}

func TestImportService_Import_ProgressEventsInOrder(t *testing.T) {
	doubles := newImportDoubles(t)
	doubles.sources.discovered = DiscoveredSources{Tracks: []DiscoveredTrack{discoveredTrack(t, trackSpec{"a1", "album", "01.flac", "Song", 1})}}

	_, err := doubles.service().Import(context.Background(), importInput())

	expected := []string{
		"start discover 0", "finish discover",
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
	expected := map[string]string{"Band - First (feat. Guest).m4a": "1/2", "Band - Second.m4a": "2/2", "Band - Single.m4a": "1/1"}
	if err != nil || fmt.Sprint(tracks) != fmt.Sprint(expected) {
		t.Fatalf("tracks = %v err = %v", tracks, err)
	}
}

func TestImportService_Import_KeepSourceWithoutNumber_FailsGroup(t *testing.T) {
	doubles := newImportDoubles(t)
	input := importInput()
	input.Numbering = domain.NumberingKeepSource

	output, err := doubles.service().Import(context.Background(), input)

	if row := findRow(t, output.Rows, "in/single/x.flac"); err != nil || row.Status != "failed" || row.Reason != "numbering: "+domain.ErrTrackPositionOutOfRange.Error() {
		t.Fatalf("row = %+v err = %v", row, err)
	}
}

func TestImportService_Import_UnknownTemplate_FailsTargets(t *testing.T) {
	doubles := newImportDoubles(t)
	input := importInput()
	input.Layout, input.AlbumTemplate = domain.LayoutAlbumFolders, "{genre}"

	output, err := doubles.service().Import(context.Background(), input)

	if err != nil || output.Counts.Published != 0 || findRow(t, output.Rows, "in/single/x.flac").Reason != "target: "+domain.ErrTemplatePlaceholderUnknown.Error() {
		t.Fatalf("output = %+v err = %v", output, err)
	}
}

func TestImportService_Preview_ReturnsPlannedTargets_PublisherAndConverterNotCalled(t *testing.T) {
	doubles := newImportDoubles(t)

	preview, err := doubles.service().Preview(context.Background(), importInput())

	if err != nil || doubles.converter.calls != 0 || len(doubles.publisher.published) != 0 || doubles.resolver.calls != 1 {
		t.Fatalf("converter = %d published = %d err = %v", doubles.converter.calls, len(doubles.publisher.published), err)
	}
	if row := findRow(t, preview.Rows, "in/album/02.flac"); row.Status != RowStatusPlanned || row.Target != "Band - Second.m4a" {
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
		{name: "MissingFolder", arrange: func(_ *importDoubles, input *ImportInput) { input.Folder = "" }, expectedError: ErrFolderMissing},
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
	if output.Counts != (domain.RunCounts{Skipped: 4, Failed: 1, Total: 5}) {
		t.Fatalf("counts = %+v", output.Counts)
	}
	if row := findRow(t, output.Rows, "in/album/01.flac"); row.Status != "skipped" || row.Reason != "already published" {
		t.Fatalf("row = %+v", row)
	}
}

func TestImportService_SecondRun_ConvertsNothing(t *testing.T) {
	doubles := newImportDoubles(t)
	doubles.publisher.journal = map[domain.TrackID]string{"a1": "x", "a2": "y", "b1": "z"}

	_, err := doubles.service().Import(context.Background(), importInput())

	if err != nil || doubles.converter.calls != 1 || doubles.publisher.writes != 0 {
		t.Fatalf("calls = %d writes = %d err = %v", doubles.converter.calls, doubles.publisher.writes, err)
	}
}

func TestImportService_ResumeAfterCrash_PublishesRemainingOnce(t *testing.T) {
	journal := map[domain.TrackID]string{}
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
