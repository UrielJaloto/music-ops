package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/internal/domain/runs"
	"github.com/UrielJaloto/music-ops/internal/domain/tracks"
	"github.com/UrielJaloto/music-ops/internal/services/conversion"
	"github.com/UrielJaloto/music-ops/internal/services/importing"
	"github.com/UrielJaloto/music-ops/internal/services/publishing"
)

type steppingClock struct {
	now  time.Time
	step time.Duration
}

func (clock *steppingClock) Now() time.Time {
	current := clock.now
	clock.now = clock.now.Add(clock.step)
	return current
}

func newTestClock() *steppingClock {
	return &steppingClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), step: 1500 * time.Millisecond}
}

func decodeLogRecords(t *testing.T, buffer *bytes.Buffer) []map[string]any {
	t.Helper()
	records := []map[string]any{}
	for line := range strings.SplitSeq(strings.TrimSpace(buffer.String()), "\n") {
		record := map[string]any{}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

func assertFinishedRecord(t *testing.T, records []map[string]any, operation string, expectedError error) {
	t.Helper()
	if len(records) != 2 {
		t.Fatalf("records = %v", records)
	}
	last := records[1]
	if last["operation"] != operation || last["duration"] != float64(1500*time.Millisecond) {
		t.Fatalf("record = %v", last)
	}
	if expectedError == nil && (last["msg"] != "finished" || last["error"] != nil) {
		t.Fatalf("record = %v", last)
	}
	if expectedError != nil && (last["msg"] != "failed" || last["error"] != expectedError.Error()) {
		t.Fatalf("record = %v", last)
	}
}

func TestNewLogger_FanOut(t *testing.T) {
	var file, console bytes.Buffer

	NewLogger(&file, &console).With("run", "r1").WithGroup("g").Info("hello", "key", "value")

	if !strings.Contains(file.String(), `"msg":"hello"`) || !strings.Contains(file.String(), `"g":{"key":"value"}`) {
		t.Fatalf("file = %s", file.String())
	}
	if !strings.Contains(console.String(), "msg=hello") || !strings.Contains(console.String(), "run=r1") {
		t.Fatalf("console = %s", console.String())
	}
}

func TestNewLogger_FileOnly(t *testing.T) {
	var file bytes.Buffer
	logger := NewLogger(&file, nil)

	logger.Debug("hidden")
	logger.Info("shown")

	records := decodeLogRecords(t, &file)
	if len(records) != 1 || records[0]["msg"] != "shown" {
		t.Fatalf("records = %v", records)
	}
}

type importerStub struct {
	importOutput  importing.ImportOutput
	previewOutput importing.ImportPreviewOutput
	err           error
}

func (stub importerStub) Import(context.Context, importing.ImportInput) (importing.ImportOutput, error) {
	return stub.importOutput, stub.err
}

func (stub importerStub) Preview(context.Context, importing.ImportInput) (importing.ImportPreviewOutput, error) {
	return stub.previewOutput, stub.err
}

func TestImportServiceLogger_IsTransparent(t *testing.T) {
	errImport := errors.New("output not writable")
	cases := []struct {
		name string
		stub importerStub
	}{
		{name: "Success", stub: importerStub{
			importOutput:  importing.ImportOutput{RunID: "r1", ExitStatus: 2, Counts: runs.RunCounts{}, Rows: []importing.ImportRow{{}}},
			previewOutput: importing.ImportPreviewOutput{Warnings: []string{"no advisor"}},
		}},
		{name: "Failure", stub: importerStub{err: errImport}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var importLog, previewLog bytes.Buffer
			importDecorator := NewImportServiceLogger(testCase.stub, NewLogger(&importLog, nil), newTestClock())
			previewDecorator := NewImportServiceLogger(testCase.stub, NewLogger(&previewLog, nil), newTestClock())

			importOutput, importErr := importDecorator.Import(context.Background(), importing.ImportInput{})
			previewOutput, previewErr := previewDecorator.Preview(context.Background(), importing.ImportInput{})

			if diff := differences(testCase.stub.importOutput, importOutput); diff != "" || !errors.Is(importErr, testCase.stub.err) {
				t.Fatalf("import diff %s err = %v", diff, importErr)
			}
			if diff := differences(testCase.stub.previewOutput, previewOutput); diff != "" || !errors.Is(previewErr, testCase.stub.err) {
				t.Fatalf("preview diff %s err = %v", diff, previewErr)
			}
			assertFinishedRecord(t, decodeLogRecords(t, &importLog), "import", testCase.stub.err)
			assertFinishedRecord(t, decodeLogRecords(t, &previewLog), "preview", testCase.stub.err)
		})
	}
}

type converterStub struct {
	output conversion.ConvertOutput
	err    error
}

func (stub converterStub) Convert(context.Context, conversion.ConvertInput) (conversion.ConvertOutput, error) {
	return stub.output, stub.err
}

func TestConversionServiceLogger_IsTransparent(t *testing.T) {
	cases := []struct {
		name string
		stub converterStub
	}{
		{name: "Success", stub: converterStub{output: conversion.ConvertOutput{Converted: []conversion.ConvertedTrack{{ID: "t1"}}}}},
		{name: "Failure", stub: converterStub{err: errors.New("encoder missing")}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var logs bytes.Buffer

			output, err := NewConversionServiceLogger(testCase.stub, NewLogger(&logs, nil), newTestClock()).Convert(context.Background(), conversion.ConvertInput{})

			if diff := differences(testCase.stub.output, output); diff != "" || !errors.Is(err, testCase.stub.err) {
				t.Fatalf("diff %s err = %v", diff, err)
			}
			records := decodeLogRecords(t, &logs)
			assertFinishedRecord(t, records, "convert", testCase.stub.err)
			if testCase.stub.err == nil && records[1]["converted"] != float64(1) {
				t.Fatalf("record = %v", records[1])
			}
		})
	}
}

type publisherStub struct {
	published publishing.PublishedTracks
	admitted  publishing.AdmittedTracks
	output    publishing.PublishOutput
	err       error
}

func (stub publisherStub) FindPublished(context.Context, publishing.FindPublishedInput) (publishing.PublishedTracks, error) {
	return stub.published, stub.err
}

func (stub publisherStub) Admit(context.Context, publishing.AdmitInput) (publishing.AdmittedTracks, error) {
	return stub.admitted, stub.err
}

func (stub publisherStub) Publish(context.Context, publishing.PublishInput) (publishing.PublishOutput, error) {
	return stub.output, stub.err
}

func TestPublishingServiceLogger_IsTransparent(t *testing.T) {
	cases := []struct {
		name string
		stub publisherStub
	}{
		{name: "Success", stub: publisherStub{
			published: publishing.PublishedTracks{Targets: map[tracks.TrackID]string{"t1": "a.m4a"}},
			admitted:  publishing.AdmittedTracks{Publishable: []tracks.TrackID{"t2"}},
			output:    publishing.PublishOutput{Status: publishing.PublishStatusPublished, Destination: "b.m4a"},
		}},
		{name: "Failure", stub: publisherStub{err: errors.New("library unreadable")}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var findLog, admitLog, publishLog bytes.Buffer
			ctx := context.Background()

			published, findErr := NewPublishingServiceLogger(testCase.stub, NewLogger(&findLog, nil), newTestClock()).FindPublished(ctx, publishing.FindPublishedInput{})
			admitted, admitErr := NewPublishingServiceLogger(testCase.stub, NewLogger(&admitLog, nil), newTestClock()).Admit(ctx, publishing.AdmitInput{})
			output, publishErr := NewPublishingServiceLogger(testCase.stub, NewLogger(&publishLog, nil), newTestClock()).Publish(ctx, publishing.PublishInput{})

			if diff := differences(testCase.stub.published, published); diff != "" || !errors.Is(findErr, testCase.stub.err) {
				t.Fatalf("find diff %s err = %v", diff, findErr)
			}
			if diff := differences(testCase.stub.admitted, admitted); diff != "" || !errors.Is(admitErr, testCase.stub.err) {
				t.Fatalf("admit diff %s err = %v", diff, admitErr)
			}
			if diff := differences(testCase.stub.output, output); diff != "" || !errors.Is(publishErr, testCase.stub.err) {
				t.Fatalf("publish diff %s err = %v", diff, publishErr)
			}
			assertFinishedRecord(t, decodeLogRecords(t, &findLog), "find_published", testCase.stub.err)
			assertFinishedRecord(t, decodeLogRecords(t, &admitLog), "admit", testCase.stub.err)
			assertFinishedRecord(t, decodeLogRecords(t, &publishLog), "publish", testCase.stub.err)
		})
	}
}

func differences(want, got any) string {
	if reflect.DeepEqual(want, got) {
		return ""
	}
	return fmt.Sprintf("want: %+v\ngot:  %+v", want, got)
}
