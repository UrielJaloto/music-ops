//go:build windows

package transcoding

import (
	"context"
	"strings"
	"testing"
)

func TestToProcessPath_InvalidLongPath_IsUnchanged(t *testing.T) {
	path := strings.Repeat("a", longPathLimit) + "\x00"

	if toProcessPath(path) != path {
		t.Fatal("path changed")
	}
}

func TestFFprobeInspector_LongPathsUsePrefix(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		expected func(path string) string
	}{
		{name: "AtLimit_Unchanged", path: `C:\` + strings.Repeat("a", 237), expected: func(path string) string { return path }},
		{name: "AboveLimit_Prefixed", path: `C:\` + strings.Repeat("a", 238), expected: func(path string) string { return longPathPrefix + path }},
		{name: "AlreadyPrefixed_Unchanged", path: longPathPrefix + `C:\` + strings.Repeat("a", 300), expected: func(path string) string { return path }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			runner := &ffprobeRunnerStub{output: ffprobeFixture(t, "wav_no_tags.json")}

			_, err := NewFFprobeInspector(runner, "ffprobe").Inspect(context.Background(), testCase.path)

			if err != nil || runner.arguments[len(runner.arguments)-1] != testCase.expected(testCase.path) {
				t.Fatalf("arguments = %v err = %v", runner.arguments, err)
			}
		})
	}
}
