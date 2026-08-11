package transcoding

import (
	"math"
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
