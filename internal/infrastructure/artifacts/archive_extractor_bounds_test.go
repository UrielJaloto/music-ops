package artifacts

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestClampInt64(t *testing.T) {
	cases := []struct {
		name     string
		value    uint64
		expected int64
	}{
		{name: "Zero_IsKept", value: 0, expected: 0},
		{name: "AtMaximum_IsKept", value: math.MaxInt64, expected: math.MaxInt64},
		{name: "AboveMaximum_ReturnsMaximum", value: math.MaxInt64 + 1, expected: math.MaxInt64},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := clampInt64(testCase.value); got != testCase.expected {
				t.Fatalf("clampInt64(%d) = %d, want %d", testCase.value, got, testCase.expected)
			}
		})
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
