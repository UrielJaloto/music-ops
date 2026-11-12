package tracks

import (
	"errors"
	"testing"

	"github.com/UrielJaloto/music-ops/internal/domain/genres"
)

func TestNewTagOverrides(t *testing.T) {
	cases := []struct {
		name          string
		values        map[string]string
		expected      TagOverrides
		expectedError error
	}{
		{name: "Empty", values: map[string]string{}},
		{name: "EveryTag", values: map[string]string{"album": "Hybrid  Theory", "genre": " rock;metal ", "year": "2000"}, expected: TagOverrides{Album: "Hybrid Theory", Genre: "rock;metal", Year: "2000"}},
		{name: "KeyCaseAndSpacesIgnored", values: map[string]string{" Album ": "X"}, expected: TagOverrides{Album: "X"}},
		{name: "UnknownTag", values: map[string]string{"artist": "X"}, expectedError: ErrTagUnknown},
		{name: "EmptyValue", values: map[string]string{"album": "  "}, expectedError: ErrTagValueMissing},
		{name: "YearTooShort", values: map[string]string{"year": "99"}, expectedError: ErrYearInvalid},
		{name: "YearNotDigits", values: map[string]string{"year": "20O0"}, expectedError: ErrYearInvalid},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			overrides, err := NewTagOverrides(testCase.values)

			if !errors.Is(err, testCase.expectedError) || overrides != testCase.expected {
				t.Fatalf("overrides = %+v err = %v", overrides, err)
			}
		})
	}
}

func TestTagOverrides_CanonicalizeGenre(t *testing.T) {
	cases := []struct {
		name          string
		overrides     TagOverrides
		expected      TagOverrides
		expectedError error
	}{
		{name: "NoGenre_Unchanged", overrides: TagOverrides{Album: "X"}, expected: TagOverrides{Album: "X"}},
		{name: "GenreCanonicalized", overrides: TagOverrides{Genre: "rock;metal", Year: "2000"}, expected: TagOverrides{Genre: "Rock; Metal", Year: "2000"}},
		{name: "GenreNotInPool", overrides: TagOverrides{Genre: "Nu Metal"}, expectedError: genres.ErrGenreNotInPool},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			overrides, err := testCase.overrides.CanonicalizeGenre(genres.NewDefaultPool())

			if !errors.Is(err, testCase.expectedError) || overrides != testCase.expected {
				t.Fatalf("overrides = %+v err = %v", overrides, err)
			}
		})
	}
}

func TestTagOverrides_Apply(t *testing.T) {
	source := SourceTags{Title: "Song", Album: "Old", Date: "1999-01-01"}
	cases := []struct {
		name      string
		overrides TagOverrides
		expected  SourceTags
	}{
		{name: "NoOverrides_KeepsSource", expected: source},
		{name: "AlbumAndYearReplaced", overrides: TagOverrides{Album: "New", Year: "2001", Genre: "Rock"}, expected: SourceTags{Title: "Song", Album: "New", Date: "2001"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.overrides.Apply(source); got != testCase.expected {
				t.Fatalf("got %+v", got)
			}
		})
	}
}
