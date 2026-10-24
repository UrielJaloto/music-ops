package infrastructure

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/UrielJaloto/music-ops/domain"
	"github.com/UrielJaloto/music-ops/services"
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
	importOutput  services.ImportOutput
	previewOutput services.ImportPreviewOutput
	err           error
}

func (stub importerStub) Import(context.Context, services.ImportInput) (services.ImportOutput, error) {
	return stub.importOutput, stub.err
}

func (stub importerStub) Preview(context.Context, services.ImportInput) (services.ImportPreviewOutput, error) {
	return stub.previewOutput, stub.err
}

func TestImportServiceLogger_IsTransparent(t *testing.T) {
	errImport := errors.New("output not writable")
	cases := []struct {
		name string
		stub importerStub
	}{
		{name: "Success", stub: importerStub{
			importOutput:  services.ImportOutput{RunID: "r1", ExitStatus: 2, Counts: domain.RunCounts{}, Rows: []services.ImportRow{{}}},
			previewOutput: services.ImportPreviewOutput{Warnings: []string{"no advisor"}},
		}},
		{name: "Failure", stub: importerStub{err: errImport}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var importLog, previewLog bytes.Buffer
			importDecorator := NewImportServiceLogger(testCase.stub, NewLogger(&importLog, nil), newTestClock())
			previewDecorator := NewImportServiceLogger(testCase.stub, NewLogger(&previewLog, nil), newTestClock())

			importOutput, importErr := importDecorator.Import(context.Background(), services.ImportInput{})
			previewOutput, previewErr := previewDecorator.Preview(context.Background(), services.ImportInput{})

			if diff := cmp.Diff(testCase.stub.importOutput, importOutput); diff != "" || !errors.Is(importErr, testCase.stub.err) {
				t.Fatalf("import diff %s err = %v", diff, importErr)
			}
			if diff := cmp.Diff(testCase.stub.previewOutput, previewOutput); diff != "" || !errors.Is(previewErr, testCase.stub.err) {
				t.Fatalf("preview diff %s err = %v", diff, previewErr)
			}
			assertFinishedRecord(t, decodeLogRecords(t, &importLog), "import", testCase.stub.err)
			assertFinishedRecord(t, decodeLogRecords(t, &previewLog), "preview", testCase.stub.err)
		})
	}
}

type converterStub struct {
	output services.ConvertOutput
	err    error
}

func (stub converterStub) Convert(context.Context, services.ConvertInput) (services.ConvertOutput, error) {
	return stub.output, stub.err
}

func TestConversionServiceLogger_IsTransparent(t *testing.T) {
	cases := []struct {
		name string
		stub converterStub
	}{
		{name: "Success", stub: converterStub{output: services.ConvertOutput{Converted: []services.ConvertedTrack{{ID: "t1"}}}}},
		{name: "Failure", stub: converterStub{err: errors.New("encoder missing")}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var logs bytes.Buffer

			output, err := NewConversionServiceLogger(testCase.stub, NewLogger(&logs, nil), newTestClock()).Convert(context.Background(), services.ConvertInput{})

			if diff := cmp.Diff(testCase.stub.output, output); diff != "" || !errors.Is(err, testCase.stub.err) {
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
	published services.PublishedTracks
	admitted  services.AdmittedTracks
	output    services.PublishOutput
	err       error
}

func (stub publisherStub) FindPublished(context.Context, services.FindPublishedInput) (services.PublishedTracks, error) {
	return stub.published, stub.err
}

func (stub publisherStub) Admit(context.Context, services.AdmitInput) (services.AdmittedTracks, error) {
	return stub.admitted, stub.err
}

func (stub publisherStub) Publish(context.Context, services.PublishInput) (services.PublishOutput, error) {
	return stub.output, stub.err
}

func TestPublishingServiceLogger_IsTransparent(t *testing.T) {
	cases := []struct {
		name string
		stub publisherStub
	}{
		{name: "Success", stub: publisherStub{
			published: services.PublishedTracks{Targets: map[domain.TrackID]string{"t1": "a.m4a"}},
			admitted:  services.AdmittedTracks{Publishable: []domain.TrackID{"t2"}},
			output:    services.PublishOutput{Status: services.PublishStatusPublished, Destination: "b.m4a"},
		}},
		{name: "Failure", stub: publisherStub{err: errors.New("library unreadable")}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var findLog, admitLog, publishLog bytes.Buffer
			ctx := context.Background()

			published, findErr := NewPublishingServiceLogger(testCase.stub, NewLogger(&findLog, nil), newTestClock()).FindPublished(ctx, services.FindPublishedInput{})
			admitted, admitErr := NewPublishingServiceLogger(testCase.stub, NewLogger(&admitLog, nil), newTestClock()).Admit(ctx, services.AdmitInput{})
			output, publishErr := NewPublishingServiceLogger(testCase.stub, NewLogger(&publishLog, nil), newTestClock()).Publish(ctx, services.PublishInput{})

			if diff := cmp.Diff(testCase.stub.published, published); diff != "" || !errors.Is(findErr, testCase.stub.err) {
				t.Fatalf("find diff %s err = %v", diff, findErr)
			}
			if diff := cmp.Diff(testCase.stub.admitted, admitted); diff != "" || !errors.Is(admitErr, testCase.stub.err) {
				t.Fatalf("admit diff %s err = %v", diff, admitErr)
			}
			if diff := cmp.Diff(testCase.stub.output, output); diff != "" || !errors.Is(publishErr, testCase.stub.err) {
				t.Fatalf("publish diff %s err = %v", diff, publishErr)
			}
			assertFinishedRecord(t, decodeLogRecords(t, &findLog), "find_published", testCase.stub.err)
			assertFinishedRecord(t, decodeLogRecords(t, &admitLog), "admit", testCase.stub.err)
			assertFinishedRecord(t, decodeLogRecords(t, &publishLog), "publish", testCase.stub.err)
		})
	}
}
