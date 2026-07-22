package services

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/domain"
)

type libraryReaderStub struct {
	paths []string
	err   error
}

func (stub libraryReaderStub) ListTrackPaths(context.Context, string) ([]string, error) {
	return stub.paths, stub.err
}

type publishRecorder struct {
	calls []string
}

type finalizerSpy struct {
	recorder *publishRecorder
	err      error
	request  FinalizeRequest
}

func (spy *finalizerSpy) Finalize(_ context.Context, request FinalizeRequest) error {
	spy.recorder.calls = append(spy.recorder.calls, "finalize")
	spy.request = request
	return spy.err
}

type moverSpy struct {
	recorder  *publishRecorder
	moveErr   error
	removeErr error
	existing  map[string]bool
	moved     [][2]string
	removed   []string
}

func (spy *moverSpy) Move(_ context.Context, source, destination string) error {
	spy.recorder.calls = append(spy.recorder.calls, "move")
	spy.moved = append(spy.moved, [2]string{source, destination})
	return spy.moveErr
}

func (spy *moverSpy) Remove(_ context.Context, path string) error {
	spy.recorder.calls = append(spy.recorder.calls, "remove")
	spy.removed = append(spy.removed, path)
	return spy.removeErr
}

func (spy *moverSpy) Exists(path string) bool {
	return spy.existing[path]
}

type verifierSpy struct {
	recorder *publishRecorder
	err      error
	expected PublishedAudio
}

func (spy *verifierSpy) Verify(_ context.Context, _ string, expected PublishedAudio) error {
	spy.recorder.calls = append(spy.recorder.calls, "verify")
	spy.expected = expected
	return spy.err
}

type publishingJournalFake struct {
	recorder  *publishRecorder
	published map[domain.TrackID]string
	readErr   error
	writeErr  error
}

func (fake *publishingJournalFake) IsPublished(_ context.Context, id domain.TrackID) (string, bool, error) {
	target, found := fake.published[id]
	return target, found, fake.readErr
}

func (fake *publishingJournalFake) RecordPublished(_ context.Context, id domain.TrackID, target string) error {
	fake.recorder.calls = append(fake.recorder.calls, "record")
	if fake.writeErr != nil {
		return fake.writeErr
	}
	if fake.published == nil {
		fake.published = map[domain.TrackID]string{}
	}
	fake.published[id] = target
	return nil
}

type publishingDoubles struct {
	recorder  *publishRecorder
	finalizer *finalizerSpy
	mover     *moverSpy
	verifier  *verifierSpy
	journal   *publishingJournalFake
}

func newPublishingDoubles() publishingDoubles {
	recorder := &publishRecorder{}
	return publishingDoubles{
		recorder:  recorder,
		finalizer: &finalizerSpy{recorder: recorder},
		mover:     &moverSpy{recorder: recorder, existing: map[string]bool{}},
		verifier:  &verifierSpy{recorder: recorder},
		journal:   &publishingJournalFake{recorder: recorder},
	}
}

func (doubles publishingDoubles) service(library LibraryReader) PublishingService {
	return NewPublishingService(library, doubles.finalizer, doubles.mover, doubles.verifier, doubles.journal)
}

func publishTrack() PublishTrack {
	return PublishTrack{
		ID: "a", StagedAudio: "stage/a.audio.m4a", FinalStaged: "stage/a.final.m4a", CoverSource: "in/a.flac",
		Cover: domain.CoverArt{Stream: 1, Codec: "png"}, Target: "Band - Song.m4a",
		Metadata: domain.TrackMetadata{Title: "Song", Artist: "Band"}, Duration: time.Minute, Bitrate: 256000, IsReencoded: true,
	}
}

func TestPublishingService_Publish(t *testing.T) {
	cases := []struct {
		name          string
		arrange       func(publishingDoubles)
		expected      PublishOutput
		expectedCalls []string
	}{
		{
			name: "HappyPath_CallsFinalizeMoveVerifyRecordInOrder", arrange: func(publishingDoubles) {},
			expected:      PublishOutput{Status: PublishStatusPublished, Destination: "out/Band - Song.m4a"},
			expectedCalls: []string{"finalize", "move", "verify", "record"},
		},
		{
			name: "AlreadyPublished_SkipsAllWrites",
			arrange: func(doubles publishingDoubles) {
				doubles.journal.published = map[domain.TrackID]string{"a": "Band - Song.m4a"}
				doubles.mover.existing["out/Band - Song.m4a"] = true
			},
			expected: PublishOutput{Status: PublishStatusAlreadyPublished, Destination: "out/Band - Song.m4a"},
		},
		{
			name: "JournalSaysPublishedButFileMissing_PublishesAgain",
			arrange: func(doubles publishingDoubles) {
				doubles.journal.published = map[domain.TrackID]string{"a": "Band - Song.m4a"}
			},
			expected:      PublishOutput{Status: PublishStatusPublished, Destination: "out/Band - Song.m4a"},
			expectedCalls: []string{"finalize", "move", "verify", "record"},
		},
		{
			name:          "FinalizeFails_MoveNotCalled",
			arrange:       func(doubles publishingDoubles) { doubles.finalizer.err = errTechnical },
			expected:      PublishOutput{Status: PublishStatusFailed, Stage: PublishStageFinalize, Reason: "technical failure", Destination: "out/Band - Song.m4a"},
			expectedCalls: []string{"finalize"},
		},
		{
			name:          "MoveFails_VerifyNotCalled",
			arrange:       func(doubles publishingDoubles) { doubles.mover.moveErr = errTechnical },
			expected:      PublishOutput{Status: PublishStatusFailed, Stage: PublishStageMove, Reason: "technical failure", Destination: "out/Band - Song.m4a"},
			expectedCalls: []string{"finalize", "move"},
		},
		{
			name:          "VerifyFails_RemovesFileAndFails",
			arrange:       func(doubles publishingDoubles) { doubles.verifier.err = errTechnical },
			expected:      PublishOutput{Status: PublishStatusFailed, Stage: PublishStageVerify, Reason: "technical failure", Destination: "out/Band - Song.m4a"},
			expectedCalls: []string{"finalize", "move", "verify", "remove"},
		},
		{
			name: "VerifyAndRemoveFail_ReportsBoth",
			arrange: func(doubles publishingDoubles) {
				doubles.verifier.err = errTechnical
				doubles.mover.removeErr = errors.New("locked")
			},
			expected:      PublishOutput{Status: PublishStatusFailed, Stage: PublishStageVerify, Reason: "technical failure (removing invalid file: locked)", Destination: "out/Band - Song.m4a"},
			expectedCalls: []string{"finalize", "move", "verify", "remove"},
		},
		{
			name:          "RecordFails_ReturnsFailure",
			arrange:       func(doubles publishingDoubles) { doubles.journal.writeErr = errTechnical },
			expected:      PublishOutput{Status: PublishStatusFailed, Stage: PublishStageJournal, Reason: "technical failure", Destination: "out/Band - Song.m4a"},
			expectedCalls: []string{"finalize", "move", "verify", "record"},
		},
		{
			name:     "JournalReadFails_ReturnsFailure",
			arrange:  func(doubles publishingDoubles) { doubles.journal.readErr = errTechnical },
			expected: PublishOutput{Status: PublishStatusFailed, Stage: PublishStageJournal, Reason: "technical failure", Destination: "out/Band - Song.m4a"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			doubles := newPublishingDoubles()
			testCase.arrange(doubles)

			output, err := doubles.service(libraryReaderStub{}).Publish(context.Background(), PublishInput{Track: publishTrack(), Output: "out/"})

			if err != nil || output != testCase.expected {
				t.Fatalf("output = %+v err = %v, want %+v", output, err, testCase.expected)
			}
			if !slices.Equal(doubles.recorder.calls, testCase.expectedCalls) {
				t.Fatalf("calls = %v, want %v", doubles.recorder.calls, testCase.expectedCalls)
			}
		})
	}
}

func TestPublishingService_Publish_PassesRequests(t *testing.T) {
	doubles := newPublishingDoubles()
	track := publishTrack()

	_, err := doubles.service(libraryReaderStub{}).Publish(context.Background(), PublishInput{Track: track, Output: "out"})

	expectedRequest := FinalizeRequest{Staged: "stage/a.audio.m4a", CoverSource: "in/a.flac", Cover: track.Cover, Destination: "stage/a.final.m4a", Metadata: track.Metadata}
	if err != nil || doubles.finalizer.request != expectedRequest {
		t.Fatalf("finalize request = %+v", doubles.finalizer.request)
	}
	if doubles.mover.moved[0] != [2]string{"stage/a.final.m4a", "out/Band - Song.m4a"} {
		t.Fatalf("moved = %v", doubles.mover.moved)
	}
	if doubles.verifier.expected != (PublishedAudio{Metadata: track.Metadata, Duration: time.Minute, Bitrate: 256000, IsReencoded: true}) {
		t.Fatalf("verify expected = %+v", doubles.verifier.expected)
	}
	if doubles.journal.published["a"] != "Band - Song.m4a" {
		t.Fatalf("journal = %v", doubles.journal.published)
	}
}

func admitTrack(t *testing.T, id, relative, target string) AdmitTrack {
	t.Helper()
	quality := testQuality(t)
	fileName := domain.NewFileName(genreTrack(t, id, "Band", target).Credit)
	path, err := domain.NewTargetPath(domain.TargetPathDetails{Layout: domain.LayoutFlat, FileName: fileName})
	if err != nil {
		t.Fatal(err)
	}
	return AdmitTrack{ID: domain.TrackID(id), Source: domain.SourceDetails{Path: "in/" + relative, RelativePath: relative, Codec: "flac", Quality: quality}, Target: path}
}

func TestPublishingService_Admit(t *testing.T) {
	cases := []struct {
		name                string
		library             libraryReaderStub
		tracks              func(*testing.T) []AdmitTrack
		expectedPublishable []domain.TrackID
		expectedDuplicates  []DuplicateTrack
		expectedError       error
	}{
		{
			name: "LibraryHit_IsDuplicate", library: libraryReaderStub{paths: []string{"Band - Song.m4a"}},
			tracks:              func(t *testing.T) []AdmitTrack { return []AdmitTrack{admitTrack(t, "a", "a.flac", "Song")} },
			expectedPublishable: []domain.TrackID{}, expectedDuplicates: []DuplicateTrack{{ID: "a", KeeperPath: "out/Band - Song.m4a"}},
		},
		{
			name: "InRunDuplicates_KeepFirstByPath", library: libraryReaderStub{},
			tracks: func(t *testing.T) []AdmitTrack {
				return []AdmitTrack{admitTrack(t, "b", "b.flac", "Song"), admitTrack(t, "a", "a.flac", "Song"), admitTrack(t, "c", "c.flac", "Other")}
			},
			expectedPublishable: []domain.TrackID{"a", "c"}, expectedDuplicates: []DuplicateTrack{{ID: "b", KeeperPath: "in/a.flac"}},
		},
		{
			name: "ListError_ReturnsWrappedError", library: libraryReaderStub{err: errTechnical},
			tracks: func(*testing.T) []AdmitTrack { return nil }, expectedError: errTechnical,
		},
		{
			name: "InvalidTrack_ReturnsError", library: libraryReaderStub{},
			tracks: func(*testing.T) []AdmitTrack { return []AdmitTrack{{ID: "x"}} }, expectedError: domain.ErrTrackPathMissing,
		},
		{
			name: "MissingTarget_IsNotPublishable", library: libraryReaderStub{},
			tracks: func(*testing.T) []AdmitTrack {
				return []AdmitTrack{{ID: "x", Source: domain.SourceDetails{Path: "in/x.flac"}}}
			},
			expectedPublishable: []domain.TrackID{}, expectedDuplicates: []DuplicateTrack{},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service := newPublishingDoubles().service(testCase.library)

			admitted, err := service.Admit(context.Background(), AdmitInput{Output: "out", Tracks: testCase.tracks(t)})

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("error = %v, want %v", err, testCase.expectedError)
			}
			if err != nil {
				return
			}
			if !slices.Equal(admitted.Publishable, testCase.expectedPublishable) || !slices.Equal(admitted.Duplicates, testCase.expectedDuplicates) {
				t.Fatalf("admitted = %+v", admitted)
			}
		})
	}
}
