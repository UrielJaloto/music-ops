//go:build windows && integration

package filesystem

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFileScanner_IsInside_WindowsNames(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "Music")
	createFiles(t, root, "Music/a.flac")
	cases := []struct {
		name     string
		child    string
		expected bool
	}{
		{name: "OtherCaseAndSlashes_IsInside", child: strings.ToLower(filepath.ToSlash(input)) + "/out/", expected: true},
		{name: "UpperCase_IsInside", child: strings.ToUpper(input), expected: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			isInside, err := NewFileScanner().IsInside(input, testCase.child)

			if err != nil || isInside != testCase.expected {
				t.Fatalf("inside = %v err = %v", isInside, err)
			}
		})
	}
}

func TestFileScanner_IsInside_InvalidNames_ReturnErrors(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name           string
		parent         string
		child          string
		expectedPrefix string
	}{
		{name: "InvalidParent", parent: filepath.Join(root, "bad<name"), child: root, expectedPrefix: "reading"},
		{name: "ChildWithNul", parent: root, child: "bad\x00name", expectedPrefix: "resolving"},
		{name: "InvalidChild", parent: root, child: filepath.Join(root, "bad<name"), expectedPrefix: "reading"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := NewFileScanner().IsInside(testCase.parent, testCase.child)

			if err == nil || !strings.HasPrefix(err.Error(), testCase.expectedPrefix) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
