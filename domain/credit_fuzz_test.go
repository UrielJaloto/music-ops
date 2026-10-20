package domain

import (
	"strings"
	"testing"
	"unicode"
)

var creditSeeds = []string{
	"HEATSEEKER (feat. Ethan Ross)",
	"PAPERWORK (feat. Ty Dolla $ign)",
	"Destination Calabria (feat. Crystal Waters) [Radio Edit]",
	"Cigarettes On Patios (Remix) (feat. 24kgoldn)",
	"Back In Black",
	"Song (with A & B)",
	"Iron Man (2009 Remaster)",
	"Track - feat. Someone",
	"(feat. Someone)",
	"Dance With Me",
	"Defeat",
}

func FuzzTitleWithoutFeaturing(f *testing.F) {
	for _, seed := range creditSeeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		title, err := NewTitle(raw)
		if err != nil {
			return
		}

		cleaned, guests := title.WithoutFeaturing()

		if cleaned.String() == "" {
			t.Fatalf("empty title for %q", raw)
		}
		if !isRuneSubsequence(withoutSpaces(cleaned.String()), withoutSpaces(raw)) {
			t.Fatalf("title %q is not drawn from %q", cleaned.String(), raw)
		}
		for _, guest := range guests {
			if guest == "" || startsWithFeaturingKeyword(guest) {
				t.Fatalf("invalid guest %q from %q", guest, raw)
			}
		}
	})
}

func FuzzParseArtistCredit(f *testing.F) {
	for _, seed := range []string{"AC/DC", "Simon & Garfunkel", "A; B", "A;;B", "¥$; Ty Dolla $ign", " a ;A; b "} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, tag string) {
		credit, err := ParseArtistCredit(tag)
		if err != nil {
			return
		}

		again, err := ParseArtistCredit(credit.String())

		if err != nil || again.String() != credit.String() {
			t.Fatalf("round trip of %q: %q -> %q (%v)", tag, credit.String(), again.String(), err)
		}
	})
}

func startsWithFeaturingKeyword(guest string) bool {
	lowered := strings.ToLower(guest)
	for _, keyword := range []string{"feat. ", "ft. ", "featuring "} {
		if strings.HasPrefix(lowered, keyword) {
			return true
		}
	}
	return false
}

func withoutSpaces(value string) []rune {
	return []rune(strings.Map(func(character rune) rune {
		if unicode.IsSpace(character) {
			return -1
		}
		return character
	}, value))
}

func isRuneSubsequence(candidate, source []rune) bool {
	position := 0
	for _, character := range source {
		if position < len(candidate) && candidate[position] == character {
			position++
		}
	}
	return position == len(candidate)
}
