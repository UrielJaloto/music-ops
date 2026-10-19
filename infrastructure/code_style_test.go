package infrastructure

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

var allowedDirectivePrefixes = []string{"//go:build", "//go:embed", "//go:generate"}

func TestSourceFiles_HaveNoComments(t *testing.T) {
	root := ".."
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && strings.HasPrefix(entry.Name(), ".") && path != root {
			return filepath.SkipDir
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		assertFileHasNoComments(t, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walking sources: %v", err)
	}
}

func TestCommentDetector(t *testing.T) {
	cases := []struct {
		name             string
		source           string
		expectedComments []string
	}{
		{name: "Directive_IsAllowed", source: "//go:build integration\n\npackage sample\n", expectedComments: nil},
		{name: "EmbedDirective_IsAllowed", source: "package sample\n\nimport _ \"embed\"\n\n//go:embed file.txt\nvar content string\n", expectedComments: nil},
		{name: "LineComment_IsRejected", source: "package sample\n\n// explanation\nvar value = 1\n", expectedComments: []string{"// explanation"}},
		{name: "BlockComment_IsRejected", source: "package sample\n\n/* explanation */\nvar value = 1\n", expectedComments: []string{"/* explanation */"}},
		{name: "TrailingComment_IsRejected", source: "package sample\n\nvar value = 1 // note\n", expectedComments: []string{"// note"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), "sample.go", testCase.source, parser.ParseComments)
			if err != nil {
				t.Fatalf("parsing: %v", err)
			}

			comments := forbiddenComments(file)

			if strings.Join(comments, "|") != strings.Join(testCase.expectedComments, "|") {
				t.Fatalf("comments = %q, want %q", comments, testCase.expectedComments)
			}
		})
	}
}

func assertFileHasNoComments(t *testing.T, path string) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	if ast.IsGenerated(file) {
		return
	}
	for _, comment := range forbiddenComments(file) {
		t.Errorf("%s: comment found: %s", path, comment)
	}
}

func forbiddenComments(file *ast.File) []string {
	var found []string
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if isAllowedDirective(comment.Text) {
				continue
			}
			found = append(found, comment.Text)
		}
	}
	return found
}

func isAllowedDirective(text string) bool {
	for _, prefix := range allowedDirectivePrefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}
