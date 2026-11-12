package config

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"testing"

	"github.com/UrielJaloto/music-ops/internal/domain/genres"
)

const customGenres = `- name: Rock
  subgenres: [Metal, Grunge]
- name: Sertanejo
  national_only: true
- name: Lo-Fi
  hint: chill beats
`

func TestConfigStore_Load_GenresFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "genres.yaml")
	writeTestFile(t, path, customGenres)

	config, err := configStoreWithFile(t, "", "", nil).Load(context.Background(), map[string]string{"genres.file": path})

	if err != nil || !slices.Equal(config.Genres.ListNames(), []string{"Rock", "Sertanejo", "Lo-Fi"}) {
		t.Fatalf("names = %v err = %v", config.Genres.ListNames(), err)
	}
	if !slices.Equal(config.Genres.ListNationalOnlyNames(), []string{"Sertanejo"}) || config.Genres.ListHints()["Lo-Fi"] != "chill beats" || len(config.Genres.ListSubgenres()["Rock"]) != 2 {
		t.Fatalf("pool = %+v", config.Genres.Definitions())
	}
}

func TestConfigStore_Load_GenresFileInvalid(t *testing.T) {
	cases := []struct {
		name          string
		content       string
		isMissing     bool
		expectedError error
	}{
		{name: "MissingFile", isMissing: true, expectedError: fs.ErrNotExist},
		{name: "EmptyFile", content: "", expectedError: genres.ErrGenrePoolEmpty},
		{name: "DuplicateGenre", content: "- name: Rock\n- name: rock\n", expectedError: genres.ErrGenreDefinitionInvalid},
		{name: "UnknownField", content: "- name: Rock\n  color: red\n"},
		{name: "NotAList", content: "name: Rock\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "genres.yaml")
			if !testCase.isMissing {
				writeTestFile(t, path, testCase.content)
			}

			_, err := configStoreWithFile(t, "", "", nil).Load(context.Background(), map[string]string{"genres.file": path})

			if err == nil || (testCase.expectedError != nil && !errors.Is(err, testCase.expectedError)) {
				t.Fatalf("error = %v, want %v", err, testCase.expectedError)
			}
		})
	}
}
