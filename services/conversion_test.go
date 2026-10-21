package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/domain"
)

type encoderSpy struct {
	requests []EncodeRequest
	err      error
}

func (spy *encoderSpy) Encode(_ context.Context, request EncodeRequest) error {
	spy.requests = append(spy.requests, request)
	return spy.err
}

type copierSpy struct {
	copies [][2]string
	err    error
}

func (spy *copierSpy) CopyAudio(_ context.Context, source, destination string) error {
	spy.copies = append(spy.copies, [2]string{source, destination})
	return spy.err
}

type stagingFake struct {
	existing     map[string]bool
	shortPathErr error
}

func (fake stagingFake) StagedPath(id domain.TrackID, suffix string) string {
	return "stage/" + string(id) + suffix
}

func (fake stagingFake) ShortSourcePath(_ context.Context, source string) (string, error) {
	return "short/" + source, fake.shortPathErr
}

func (fake stagingFake) Exists(path string) bool {
	return fake.existing[path]
}

type conversionJournalFake struct {
	converted map[domain.TrackID]bool
	readErr   error
	writeErr  error
	recorded  []domain.TrackID
}

func (fake *conversionJournalFake) IsConverted(_ context.Context, id domain.TrackID) (bool, error) {
	return fake.converted[id], fake.readErr
}

func (fake *conversionJournalFake) RecordConverted(_ context.Context, id domain.TrackID, _ string) error {
	fake.recorded = append(fake.recorded, id)
	return fake.writeErr
}

func testQuality(t *testing.T) domain.AudioQuality {
	t.Helper()
	quality, err := domain.NewAudioQuality(domain.AudioQualityDetails{SampleRate: 96000, BitDepth: 24, Channels: 2, Duration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	return quality
}

type conversionCase struct {
	name             string
	tracks           []ConvertTrack
	staging          stagingFake
	journal          *conversionJournalFake
	encoderErr       error
	ctx              func() context.Context
	expectedEncodes  int
	expectedCopies   int
	expectedFailed   int
	expectedRecorded int
	expectedError    error
}

func TestConversionService_Convert(t *testing.T) {
	quality := testQuality(t)
	flac := ConvertTrack{ID: "a", Source: "in/a.flac", Codec: "flac", Quality: quality}
	aac := ConvertTrack{ID: "b", Source: "in/b.m4a", Codec: "aac", Quality: quality}
	mp3 := ConvertTrack{ID: "c", Source: "in/c.mp3", Codec: "mp3", Quality: quality}
	cases := []conversionCase{
		{name: "LosslessSource_IsEncoded", tracks: []ConvertTrack{flac}, journal: &conversionJournalFake{}, expectedEncodes: 1, expectedRecorded: 1},
		{name: "AACSource_IsCopied", tracks: []ConvertTrack{aac}, journal: &conversionJournalFake{}, expectedCopies: 1, expectedRecorded: 1},
		{name: "Mp3Source_IsEncoded", tracks: []ConvertTrack{mp3}, journal: &conversionJournalFake{}, expectedEncodes: 1, expectedRecorded: 1},
		{
			name: "AlreadyConverted_IsReused_EncoderNotCalled", tracks: []ConvertTrack{flac},
			staging: stagingFake{existing: map[string]bool{"stage/a.audio.m4a": true}}, journal: &conversionJournalFake{converted: map[domain.TrackID]bool{"a": true}},
		},
		{name: "ConvertedButStagedMissing_IsEncodedAgain", tracks: []ConvertTrack{flac}, journal: &conversionJournalFake{converted: map[domain.TrackID]bool{"a": true}}, expectedEncodes: 1, expectedRecorded: 1},
		{name: "EncoderFails_RecordsFailureAndContinues", tracks: []ConvertTrack{flac, aac}, journal: &conversionJournalFake{}, encoderErr: errTechnical, expectedEncodes: 1, expectedCopies: 1, expectedFailed: 1, expectedRecorded: 1},
		{name: "JournalReadFails_RecordsFailure", tracks: []ConvertTrack{flac}, journal: &conversionJournalFake{readErr: errTechnical}, expectedFailed: 1},
		{name: "JournalWriteFails_RecordsFailure", tracks: []ConvertTrack{flac}, journal: &conversionJournalFake{writeErr: errTechnical}, expectedEncodes: 1, expectedFailed: 1, expectedRecorded: 1},
		{name: "ShortPathFails_RecordsFailure", tracks: []ConvertTrack{flac}, staging: stagingFake{shortPathErr: errTechnical}, journal: &conversionJournalFake{}, expectedFailed: 1},
		{name: "EmptyInput_ReturnsEmptyOutput", tracks: nil, journal: &conversionJournalFake{}},
		{name: "Cancelled_ReturnsContextError", tracks: []ConvertTrack{flac}, journal: &conversionJournalFake{}, ctx: cancelledContext, expectedError: context.Canceled},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			runConversionCase(t, testCase)
		})
	}
}

func runConversionCase(t *testing.T, testCase conversionCase) {
	t.Helper()
	encoder := &encoderSpy{err: testCase.encoderErr}
	copier := &copierSpy{}
	service := NewConversionService(encoder, copier, testCase.staging, testCase.journal, sequentialPool{})
	ctx := context.Background()
	if testCase.ctx != nil {
		ctx = testCase.ctx()
	}
	profile := domain.NewEncodingProfile(domain.EngineFDK)

	output, err := service.Convert(ctx, ConvertInput{Tracks: testCase.tracks, Profile: profile})

	if !errors.Is(err, testCase.expectedError) {
		t.Fatalf("error = %v, want %v", err, testCase.expectedError)
	}
	if len(encoder.requests) != testCase.expectedEncodes || len(copier.copies) != testCase.expectedCopies || len(testCase.journal.recorded) != testCase.expectedRecorded {
		t.Fatalf("encodes = %d copies = %d recorded = %d", len(encoder.requests), len(copier.copies), len(testCase.journal.recorded))
	}
	if err == nil && (len(output.Failed) != testCase.expectedFailed || len(output.Converted) != len(testCase.tracks)-testCase.expectedFailed) {
		t.Fatalf("output = %+v", output)
	}
}

func TestConversionService_Convert_PassesProfileAndPaths(t *testing.T) {
	encoder := &encoderSpy{}
	quality := testQuality(t)
	service := NewConversionService(encoder, &copierSpy{}, stagingFake{}, &conversionJournalFake{}, sequentialPool{})
	profile := domain.NewEncodingProfile(domain.EngineNative)

	output, err := service.Convert(context.Background(), ConvertInput{Tracks: []ConvertTrack{{ID: "a", Source: "in/a.flac", Codec: "flac", Quality: quality}}, Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	expected := EncodeRequest{Source: "short/in/a.flac", Destination: "stage/a.audio.m4a", Profile: profile, Quality: quality}
	if encoder.requests[0] != expected {
		t.Fatalf("request = %+v, want %+v", encoder.requests[0], expected)
	}
	if output.Converted[0] != (ConvertedTrack{ID: "a", StagedAudio: "stage/a.audio.m4a", FinalStaged: "stage/a.final.m4a"}) {
		t.Fatalf("converted = %+v", output.Converted[0])
	}
}

func TestConversionService_Convert_CopierFailure(t *testing.T) {
	copier := &copierSpy{err: errTechnical}
	service := NewConversionService(&encoderSpy{}, copier, stagingFake{}, &conversionJournalFake{}, sequentialPool{})

	output, err := service.Convert(context.Background(), ConvertInput{Tracks: []ConvertTrack{{ID: "b", Source: "in/b.m4a", Codec: "aac"}}})

	if err != nil || len(output.Failed) != 1 || output.Failed[0].ID != "b" || output.Failed[0].Source != "in/b.m4a" {
		t.Fatalf("output = %+v err = %v", output, err)
	}
	if copier.copies[0] != [2]string{"short/in/b.m4a", "stage/b.audio.m4a"} {
		t.Fatalf("copy = %v", copier.copies[0])
	}
}
