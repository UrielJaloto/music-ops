package session

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/UrielJaloto/music-ops/internal/services/diagnostics"
	"github.com/UrielJaloto/music-ops/internal/services/importing"
	"github.com/UrielJaloto/music-ops/internal/services/watching"
)

var (
	errOpen   = errors.New("run in progress")
	errClose  = errors.New("close failed")
	errChoose = errors.New("dialog cancelled")
	errWork   = errors.New("work failed")
)

type sessionDoubles struct {
	calls     []string
	openErr   error
	closeErr  error
	chooseErr error
	workErr   error
	folder    string
	closeCtx  context.Context
}

func (doubles *sessionDoubles) Open(context.Context) error {
	doubles.calls = append(doubles.calls, "open")
	return doubles.openErr
}

func (doubles *sessionDoubles) Close(ctx context.Context) error {
	doubles.calls = append(doubles.calls, "close")
	doubles.closeCtx = ctx
	return doubles.closeErr
}

func (doubles *sessionDoubles) Import(_ context.Context, input importing.ImportInput) (importing.ImportOutput, error) {
	doubles.calls = append(doubles.calls, "import "+input.Folder)
	return importing.ImportOutput{}, doubles.workErr
}

func (doubles *sessionDoubles) Preview(_ context.Context, input importing.ImportInput) (importing.ImportPreviewOutput, error) {
	doubles.calls = append(doubles.calls, "preview "+input.Folder)
	return importing.ImportPreviewOutput{}, doubles.workErr
}

func (doubles *sessionDoubles) Watch(context.Context, watching.WatchInput) error {
	doubles.calls = append(doubles.calls, "watch")
	return doubles.workErr
}

func (doubles *sessionDoubles) Check(context.Context) diagnostics.DiagnosticsOutput {
	doubles.calls = append(doubles.calls, "check")
	return diagnostics.DiagnosticsOutput{IsHealthy: true}
}

func (doubles *sessionDoubles) ChooseFolder(_ context.Context, title, _ string) (string, error) {
	doubles.calls = append(doubles.calls, "choose "+title)
	return doubles.folder, doubles.chooseErr
}

func (doubles *sessionDoubles) service() SessionService {
	return NewSessionService(doubles, doubles, doubles, doubles, doubles)
}

func TestSessionService_Import(t *testing.T) {
	cases := []struct {
		name          string
		folder        string
		arrange       func(doubles *sessionDoubles)
		expectedCalls []string
		expectedError error
	}{
		{name: "GivenFolder", folder: "in", arrange: func(*sessionDoubles) {}, expectedCalls: []string{"open", "import in", "close"}},
		{name: "ChosenFolder", arrange: func(doubles *sessionDoubles) { doubles.folder = "picked" }, expectedCalls: []string{"open", "choose " + chooseFolderTitle, "import picked", "close"}},
		{name: "OpenFailsBeforeTheDialog", arrange: func(doubles *sessionDoubles) { doubles.openErr = errOpen }, expectedCalls: []string{"open"}, expectedError: errOpen},
		{name: "ChooseFails", arrange: func(doubles *sessionDoubles) { doubles.chooseErr = errChoose }, expectedCalls: []string{"open", "choose " + chooseFolderTitle, "close"}, expectedError: errChoose},
		{name: "WorkAndCloseFail", folder: "in", arrange: func(doubles *sessionDoubles) { doubles.workErr, doubles.closeErr = errWork, errClose }, expectedCalls: []string{"open", "import in", "close"}, expectedError: errClose},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			doubles := &sessionDoubles{}
			testCase.arrange(doubles)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			_, err := doubles.service().Import(ctx, importing.ImportInput{Folder: testCase.folder})

			if !reflect.DeepEqual(doubles.calls, testCase.expectedCalls) {
				t.Fatalf("calls = %v", doubles.calls)
			}
			if testCase.expectedError == nil && err != nil || testCase.expectedError != nil && !errors.Is(err, testCase.expectedError) {
				t.Fatalf("err = %v", err)
			}
			if doubles.closeCtx != nil && doubles.closeCtx.Err() != nil {
				t.Fatal("close ran with a cancelled context")
			}
		})
	}
}

func TestSessionService_Import_KeepsWorkError(t *testing.T) {
	doubles := &sessionDoubles{workErr: errWork, closeErr: errClose}

	_, err := doubles.service().Import(context.Background(), importing.ImportInput{Folder: "in"})

	if !errors.Is(err, errWork) || !errors.Is(err, errClose) {
		t.Fatalf("err = %v", err)
	}
}

func TestSessionService_Preview(t *testing.T) {
	doubles := &sessionDoubles{folder: "picked"}

	_, err := doubles.service().Preview(context.Background(), importing.ImportInput{Folder: " "})

	expected := []string{"open", "choose " + chooseFolderTitle, "preview picked", "close"}
	if err != nil || !reflect.DeepEqual(doubles.calls, expected) {
		t.Fatalf("calls = %v err = %v", doubles.calls, err)
	}
}

func TestSessionService_Preview_ChooseFails(t *testing.T) {
	doubles := &sessionDoubles{chooseErr: errChoose}

	_, err := doubles.service().Preview(context.Background(), importing.ImportInput{})

	if !errors.Is(err, errChoose) {
		t.Fatalf("err = %v", err)
	}
}

func TestSessionService_Watch(t *testing.T) {
	cases := []struct {
		name          string
		doubles       *sessionDoubles
		expectedCalls []string
		expectedError error
	}{
		{name: "Runs", doubles: &sessionDoubles{}, expectedCalls: []string{"open", "watch", "close"}},
		{name: "OpenFails", doubles: &sessionDoubles{openErr: errOpen}, expectedCalls: []string{"open"}, expectedError: errOpen},
		{name: "WatchFails", doubles: &sessionDoubles{workErr: errWork}, expectedCalls: []string{"open", "watch", "close"}, expectedError: errWork},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.doubles.service().Watch(context.Background(), watching.WatchInput{})

			if !reflect.DeepEqual(testCase.doubles.calls, testCase.expectedCalls) {
				t.Fatalf("calls = %v", testCase.doubles.calls)
			}
			if testCase.expectedError == nil && err != nil || testCase.expectedError != nil && !errors.Is(err, testCase.expectedError) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestSessionService_Diagnose_ClosesWithoutOpening(t *testing.T) {
	doubles := &sessionDoubles{closeErr: errClose}

	output, err := doubles.service().Diagnose(context.Background())

	if !output.IsHealthy || !errors.Is(err, errClose) || !reflect.DeepEqual(doubles.calls, []string{"check", "close"}) {
		t.Fatalf("output = %+v err = %v calls = %v", output, err, doubles.calls)
	}
}
