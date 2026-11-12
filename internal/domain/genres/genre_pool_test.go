package genres

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func customPool(t *testing.T) Pool {
	t.Helper()
	pool, err := NewPool([]GenreDefinition{
		{Name: " Rock ", Subgenres: []string{"Metal", "Grunge"}},
		{Name: "Sertanejo", Subgenres: []string{"Universitário"}, IsNationalOnly: true},
		{Name: "Lo-Fi", Hint: "chill beats"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestNewPool(t *testing.T) {
	cases := []struct {
		name          string
		definitions   []GenreDefinition
		expectedError error
	}{
		{name: "Empty_ReturnsErrGenrePoolEmpty", definitions: nil, expectedError: ErrGenrePoolEmpty},
		{name: "BlankName_IsInvalid", definitions: []GenreDefinition{{Name: "  "}}, expectedError: ErrGenreDefinitionInvalid},
		{name: "NameWithSeparator_IsInvalid", definitions: []GenreDefinition{{Name: "Rock; Metal"}}, expectedError: ErrGenreDefinitionInvalid},
		{name: "UnknownValueAsName_IsInvalid", definitions: []GenreDefinition{{Name: "Desconhecido"}}, expectedError: ErrGenreDefinitionInvalid},
		{name: "DuplicateGenreIgnoringCase_IsInvalid", definitions: []GenreDefinition{{Name: "Rock"}, {Name: "rock"}}, expectedError: ErrGenreDefinitionInvalid},
		{name: "DuplicateSubgenre_IsInvalid", definitions: []GenreDefinition{{Name: "Rock", Subgenres: []string{"Metal", "metal"}}}, expectedError: ErrGenreDefinitionInvalid},
		{name: "BlankSubgenre_IsInvalid", definitions: []GenreDefinition{{Name: "Rock", Subgenres: []string{""}}}, expectedError: ErrGenreDefinitionInvalid},
		{name: "SameSubgenreInTwoGenres_IsValid", definitions: []GenreDefinition{{Name: "Rock", Subgenres: []string{"Live"}}, {Name: "Pop", Subgenres: []string{"Live"}}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := NewPool(testCase.definitions)

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("error = %v, want %v", err, testCase.expectedError)
			}
		})
	}
}

func TestNewPool_TrimsNamesAndExposesLists(t *testing.T) {
	pool := customPool(t)

	if !slices.Equal(pool.ListNames(), []string{"Rock", "Sertanejo", "Lo-Fi"}) || !slices.Equal(pool.ListSubgenreNames(), []string{"Metal", "Grunge", "Universitário"}) {
		t.Fatalf("names = %v subgenres = %v", pool.ListNames(), pool.ListSubgenreNames())
	}
	if !slices.Equal(pool.ListNationalOnlyNames(), []string{"Sertanejo"}) || pool.ListHints()["Lo-Fi"] != "chill beats" || len(pool.ListSubgenres()) != 2 {
		t.Fatalf("national = %v hints = %v subgenres = %v", pool.ListNationalOnlyNames(), pool.ListHints(), pool.ListSubgenres())
	}
}

func TestPool_ListSubgenreNames_ListsSharedNameOnce(t *testing.T) {
	pool, err := NewPool([]GenreDefinition{{Name: "Rock", Subgenres: []string{"Live"}}, {Name: "Pop", Subgenres: []string{"Live"}}})
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(pool.ListSubgenreNames(), []string{"Live"}) {
		t.Fatalf("subgenres = %v", pool.ListSubgenreNames())
	}
}

func TestPool_Definitions_AreCopied(t *testing.T) {
	pool := customPool(t)

	pool.Definitions()[0].Subgenres[0] = "changed"

	if pool.Definitions()[0].Subgenres[0] != "Metal" || len(NewDefaultPool().Definitions()) != 20 {
		t.Fatalf("definitions = %+v", pool.Definitions())
	}
}

func TestPool_CustomGenres(t *testing.T) {
	cases := []struct {
		name          string
		value         string
		expected      string
		expectedError error
	}{
		{name: "CustomSubgenre", value: "rock; grunge", expected: "Rock; Grunge"},
		{name: "CustomNationalOnly_NeverSuffixed", value: "Sertanejo Nacional; universitário", expected: "Sertanejo; Universitário"},
		{name: "BrazilianCustomGenre_IsSuffixed", value: "lo-fi nacional", expected: "Lo-Fi Nacional"},
		{name: "DefaultGenreOutsideCustomPool", value: "Pop", expectedError: ErrGenreNotInPool},
		{name: "SubgenreOfGenreWithoutSubgenres", value: "Lo-Fi; Jazz", expectedError: ErrSubgenreNotInGenre},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			classification, err := customPool(t).ParseGenre(testCase.value)

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("error = %v, want %v", err, testCase.expectedError)
			}
			if err == nil && classification.Genre().String() != testCase.expected {
				t.Fatalf("genre = %q, want %q", classification.Genre().String(), testCase.expected)
			}
		})
	}
}

func TestPool_ParseGenre_ErrorListsOptions(t *testing.T) {
	pool := customPool(t)

	_, genreErr := pool.ParseGenre("Pop")
	_, subgenreErr := pool.ParseGenre("Rock; Punk")
	_, emptyErr := pool.ParseGenre("Lo-Fi; Jazz")

	if !strings.HasSuffix(genreErr.Error(), "use one of: Rock, Sertanejo, Lo-Fi") {
		t.Fatalf("genre error = %v", genreErr)
	}
	if !strings.HasSuffix(subgenreErr.Error(), "sub-genres of Rock: Metal, Grunge") || !strings.HasSuffix(emptyErr.Error(), "sub-genres of Lo-Fi: none") {
		t.Fatalf("subgenre errors = %v / %v", subgenreErr, emptyErr)
	}
}

func TestPool_ClassifyByTags_CustomPool(t *testing.T) {
	cases := []struct {
		name     string
		tags     []string
		expected string
	}{
		{name: "CustomGenreMatchedByName", tags: []string{"seen live", "lo-fi"}, expected: "Lo-Fi"},
		{name: "MappedSubgenreInPool", tags: []string{"heavy metal"}, expected: "Rock; Metal"},
		{name: "MappedSubgenreMissing_KeepsGenre", tags: []string{"punk rock"}, expected: "Rock"},
		{name: "MappedGenreMissing_TriesNextTag", tags: []string{"pop", "sertanejo", "brazil"}, expected: "Sertanejo"},
		{name: "NothingInPool_IsUnknown", tags: []string{"pop", "house"}, expected: "desconhecido"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			classification := customPool(t).ClassifyByTags(NewTagEvidence(testCase.tags))

			if got := classification.Genre().String(); got != testCase.expected {
				t.Fatalf("genre = %q, want %q", got, testCase.expected)
			}
		})
	}
}
