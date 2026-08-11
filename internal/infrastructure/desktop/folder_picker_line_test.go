package desktop

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

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
