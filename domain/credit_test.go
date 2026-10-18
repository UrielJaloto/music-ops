package domain

import (
	"errors"
	"testing"

	"slices"
)

func TestNewTitle(t *testing.T) {
	cases := []struct {
		name          string
		raw           string
		expected      string
		expectedError error
	}{
		{name: "Empty_ReturnsErrTitleMissing", raw: "", expectedError: ErrTitleMissing},
		{name: "WhitespaceOnly_ReturnsErrTitleMissing", raw: " \t\n ", expectedError: ErrTitleMissing},
		{name: "InnerWhitespace_IsCollapsed", raw: "  Back   In\tBlack ", expected: "Back In Black"},
		{name: "Plain_IsKept", raw: "Iron Man", expected: "Iron Man"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			title, err := NewTitle(testCase.raw)

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("error = %v, want %v", err, testCase.expectedError)
			}
			if title.String() != testCase.expected {
				t.Fatalf("title = %q, want %q", title.String(), testCase.expected)
			}
		})
	}
}

func TestTitleWithoutFeaturing(t *testing.T) {
	cases := []struct {
		name           string
		raw            string
		expectedTitle  string
		expectedGuests []string
	}{
		{"Spec_Heatseeker", "HEATSEEKER (feat. Ethan Ross)", "HEATSEEKER", []string{"Ethan Ross"}},
		{"Spec_Paperwork", "PAPERWORK (feat. Ty Dolla $ign)", "PAPERWORK", []string{"Ty Dolla $ign"}},
		{"Spec_DestinationCalabria", "Destination Calabria (feat. Crystal Waters) [Radio Edit]", "Destination Calabria [Radio Edit]", []string{"Crystal Waters"}},
		{"Spec_CigarettesOnPatios", "Cigarettes On Patios (Remix) (feat. 24kgoldn)", "Cigarettes On Patios (Remix)", []string{"24kgoldn"}},
		{"Spec_BackInBlack", "Back In Black", "Back In Black", []string{}},
		{"Spec_SongWith", "Song (with A & B)", "Song", []string{"A", "B"}},
		{"Spec_IronMan", "Iron Man (2009 Remaster)", "Iron Man (2009 Remaster)", []string{}},
		{"CR1_UppercaseFT", "Track (FT. Someone)", "Track", []string{"Someone"}},
		{"CR1_TrailingDashFeat", "Track - feat. Someone", "Track", []string{"Someone"}},
		{"CR1_TrailingFeaturing", "Track featuring Someone and Other", "Track", []string{"Someone", "Other"}},
		{"CR1_MultipleClauses", "Track (feat. A) [ft B]", "Track", []string{"A", "B"}},
		{"CR1_WithInBrackets", "Track [with Someone]", "Track", []string{"Someone"}},
		{"CR2_KeepsRemaster", "Track (2011 Remaster) (feat. A)", "Track (2011 Remaster)", []string{"A"}},
		{"CR2_KeepsRadioEditBrackets", "Track [Radio Edit] (ft. A)", "Track [Radio Edit]", []string{"A"}},
		{"CR1_WordWithoutBracketsIsKept", "Dance With Me", "Dance With Me", []string{}},
		{"CR1_FeatInsideWordIsKept", "Defeat", "Defeat", []string{}},
		{"CR1_OnlyFeatKeepsOriginal", "(feat. Someone)", "(feat. Someone)", []string{}},
		{"CR1_GuestListWithAmpersandAndComma", "Track (feat. A, B & C)", "Track", []string{"A", "B", "C"}},
		{"CR1_GuestListWithPortugueseAndX", "Faixa (part. ignored) (feat. Ana e Bia x Caio)", "Faixa (part. ignored)", []string{"Ana", "Bia", "Caio"}},
		{"CR1_EmptyGuestPartsDropped", "Track (feat. A, , B)", "Track", []string{"A", "B"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			title := mustTitle(t, testCase.raw)

			cleaned, guests := title.WithoutFeaturing()

			if cleaned.String() != testCase.expectedTitle {
				t.Fatalf("title = %q, want %q", cleaned.String(), testCase.expectedTitle)
			}
			if !slices.Equal(guests, testCase.expectedGuests) || guests == nil {
				t.Fatalf("guests = %#v, want %#v", guests, testCase.expectedGuests)
			}
		})
	}
}

func mustTitle(t *testing.T, raw string) Title {
	t.Helper()
	title, err := NewTitle(raw)
	if err != nil {
		t.Fatalf("building title %q: %v", raw, err)
	}
	return title
}
