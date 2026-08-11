package infrastructure

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestClampInt32(t *testing.T) {
	cases := []struct {
		name     string
		value    int
		expected int32
	}{
		{name: "BelowMinimum_ReturnsMinimum", value: math.MinInt32 - 1, expected: math.MinInt32},
		{name: "AtMinimum_IsKept", value: math.MinInt32, expected: math.MinInt32},
		{name: "Zero_IsKept", value: 0, expected: 0},
		{name: "AtMaximum_IsKept", value: math.MaxInt32, expected: math.MaxInt32},
		{name: "AboveMaximum_ReturnsMaximum", value: math.MaxInt32 + 1, expected: math.MaxInt32},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := clampInt32(testCase.value); got != testCase.expected {
				t.Fatalf("clampInt32(%d) = %d, want %d", testCase.value, got, testCase.expected)
			}
		})
	}
}

func TestClampUint32(t *testing.T) {
	cases := []struct {
		name     string
		value    int64
		expected uint32
	}{
		{name: "Negative_ReturnsZero", value: -1, expected: 0},
		{name: "Zero_IsKept", value: 0, expected: 0},
		{name: "AtMaximum_IsKept", value: math.MaxUint32, expected: math.MaxUint32},
		{name: "AboveMaximum_ReturnsMaximum", value: math.MaxUint32 + 1, expected: math.MaxUint32},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := clampUint32(testCase.value); got != testCase.expected {
				t.Fatalf("clampUint32(%d) = %d, want %d", testCase.value, got, testCase.expected)
			}
		})
	}
}

func TestClampUint16(t *testing.T) {
	cases := []struct {
		name     string
		value    int
		expected uint16
	}{
		{name: "Negative_ReturnsZero", value: -1, expected: 0},
		{name: "Zero_IsKept", value: 0, expected: 0},
		{name: "AtMaximum_IsKept", value: math.MaxUint16, expected: math.MaxUint16},
		{name: "AboveMaximum_ReturnsMaximum", value: math.MaxUint16 + 1, expected: math.MaxUint16},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := clampUint16(testCase.value); got != testCase.expected {
				t.Fatalf("clampUint16(%d) = %d, want %d", testCase.value, got, testCase.expected)
			}
		})
	}
}

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

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestReadFolderLine_Errors(t *testing.T) {
	cases := []struct {
		name   string
		input  func() (string, error)
		errors string
	}{
		{name: "PromptWriteFails_ReturnsError", errors: "write failed", input: func() (string, error) {
			return readFolderLine(strings.NewReader("D:/A\n"), failingWriter{}, "Folder")
		}},
		{name: "InputReadFails_ReturnsError", errors: "read failed", input: func() (string, error) {
			return readFolderLine(failingReader{}, &bytes.Buffer{}, "Folder")
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			folder, err := testCase.input()

			if err == nil || err.Error() != testCase.errors || folder != "" {
				t.Fatalf("folder = %q err = %v", folder, err)
			}
		})
	}
}

func TestReadFolderLine_WithoutNewline_ReturnsTrimmedText(t *testing.T) {
	folder, err := readFolderLine(strings.NewReader(" D:/A "), &bytes.Buffer{}, "Folder")

	if err != nil || folder != "D:/A" {
		t.Fatalf("folder = %q err = %v", folder, err)
	}
}

func TestNewLastFMLimiter_AllowsOneBurst(t *testing.T) {
	limiter := NewLastFMLimiter()

	if limiter.Burst() != 1 || float64(limiter.Limit()) != lastFMRequestsPerSecond {
		t.Fatalf("burst = %d limit = %v", limiter.Burst(), limiter.Limit())
	}
}
