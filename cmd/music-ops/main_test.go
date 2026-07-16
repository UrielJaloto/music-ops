package main

import (
	"bytes"
	"testing"
)

func TestRun(t *testing.T) {
	cases := []struct {
		name           string
		arguments      []string
		expectedCode   int
		expectedStdout string
	}{
		{name: "Version_PrintsVersionAndReturnsZero", arguments: []string{"version"}, expectedCode: 0, expectedStdout: "music-ops dev\n"},
		{name: "UnknownCommand_ReturnsOne", arguments: []string{"unknown"}, expectedCode: 1, expectedStdout: ""},
		{name: "NoArguments_ReturnsOne", arguments: []string{}, expectedCode: 1, expectedStdout: ""},
		{name: "VersionWithExtraArgument_ReturnsOne", arguments: []string{"version", "extra"}, expectedCode: 1, expectedStdout: ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := run(testCase.arguments, &stdout, &stderr)

			if code != testCase.expectedCode {
				t.Fatalf("code = %d, want %d", code, testCase.expectedCode)
			}
			if stdout.String() != testCase.expectedStdout {
				t.Fatalf("stdout = %q, want %q", stdout.String(), testCase.expectedStdout)
			}
		})
	}
}
