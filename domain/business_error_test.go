package domain

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsBusinessError(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		expected bool
	}{
		{name: "BusinessError_IsRecognized", err: newBusinessError("rule broken"), expected: true},
		{name: "WrappedBusinessError_IsRecognized", err: fmt.Errorf("context: %w", newBusinessError("rule broken")), expected: true},
		{name: "PlainError_IsNotBusiness", err: errors.New("disk failure"), expected: false},
		{name: "NilError_IsNotBusiness", err: nil, expected: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := IsBusinessError(testCase.err); got != testCase.expected {
				t.Fatalf("IsBusinessError = %v, want %v", got, testCase.expected)
			}
		})
	}
}

func TestBusinessErrorMessage(t *testing.T) {
	if got := newBusinessError("rule broken").Error(); got != "rule broken" {
		t.Fatalf("Error() = %q, want %q", got, "rule broken")
	}
}
