package domain

import (
	"errors"
	"slices"
	"testing"
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

func TestNewArtistCredit(t *testing.T) {
	cases := []struct {
		name          string
		artists       []string
		expected      []string
		expectedError error
	}{
		{name: "EmptyList_ReturnsErrArtistMissing", artists: []string{}, expectedError: ErrArtistMissing},
		{name: "OnlyBlanks_ReturnsErrArtistMissing", artists: []string{" ", ""}, expectedError: ErrArtistMissing},
		{name: "BlanksDropped", artists: []string{" A ", "", "B"}, expected: []string{"A", "B"}},
		{name: "RepeatsWithDifferentCase_KeepFirst", artists: []string{"Queen", "QUEEN", "queen "}, expected: []string{"Queen"}},
		{name: "OrderPreserved", artists: []string{"C", "A", "B"}, expected: []string{"C", "A", "B"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			credit, err := NewArtistCredit(testCase.artists)

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("error = %v, want %v", err, testCase.expectedError)
			}
			if !slices.Equal(credit.Artists(), testCase.expected) {
				t.Fatalf("artists = %#v, want %#v", credit.Artists(), testCase.expected)
			}
		})
	}
}

func TestParseArtistCredit(t *testing.T) {
	cases := []struct {
		name          string
		tag           string
		expected      []string
		expectedError error
	}{
		{name: "CR6_SlashNotSplit", tag: "AC/DC", expected: []string{"AC/DC"}},
		{name: "CR6_AmpersandNotSplit", tag: "Simon & Garfunkel", expected: []string{"Simon & Garfunkel"}},
		{name: "CR6_CommaNotSplit", tag: "Tyler, The Creator", expected: []string{"Tyler, The Creator"}},
		{name: "Semicolon_Splits", tag: "A; B", expected: []string{"A", "B"}},
		{name: "DoubleSemicolon_DropsEmpty", tag: "A;;B", expected: []string{"A", "B"}},
		{name: "Empty_ReturnsErrArtistMissing", tag: "", expectedError: ErrArtistMissing},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			credit, err := ParseArtistCredit(testCase.tag)

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("error = %v, want %v", err, testCase.expectedError)
			}
			if !slices.Equal(credit.Artists(), testCase.expected) {
				t.Fatalf("artists = %#v, want %#v", credit.Artists(), testCase.expected)
			}
		})
	}
}

func TestArtistCreditQueries(t *testing.T) {
	credit := mustArtists(t, "Main", "Guest One", "Guest Two")

	if credit.String() != "Main; Guest One; Guest Two" {
		t.Fatalf("String = %q", credit.String())
	}
	if credit.MainArtist() != "Main" || !credit.HasGuests() {
		t.Fatalf("main = %q, hasGuests = %v", credit.MainArtist(), credit.HasGuests())
	}
	if !slices.Equal(credit.GuestArtists(), []string{"Guest One", "Guest Two"}) {
		t.Fatalf("guests = %#v", credit.GuestArtists())
	}
	if mustArtists(t, "Solo").HasGuests() || len(mustArtists(t, "Solo").GuestArtists()) != 0 {
		t.Fatal("solo artist reports guests")
	}
}

func TestArtistCredit_ReadsAreCopies(t *testing.T) {
	input := []string{"A", "B"}
	credit := mustArtists(t, input...)
	input[0] = "changed"
	read := credit.Artists()
	read[1] = "changed"
	guests := credit.GuestArtists()
	guests[0] = "changed"

	if !slices.Equal(credit.Artists(), []string{"A", "B"}) {
		t.Fatalf("artists = %#v", credit.Artists())
	}
}

func TestWithGuests(t *testing.T) {
	cases := []struct {
		name     string
		base     []string
		guests   []string
		expected []string
	}{
		{name: "CR3_GuestsAppendedInOrder", base: []string{"Main"}, guests: []string{"B", "A"}, expected: []string{"Main", "B", "A"}},
		{name: "CR3_RepeatedGuestIgnored", base: []string{"Main", "A"}, guests: []string{"a", "MAIN", "C"}, expected: []string{"Main", "A", "C"}},
		{name: "NoGuests_Unchanged", base: []string{"Main"}, guests: []string{}, expected: []string{"Main"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			base := mustArtists(t, testCase.base...)

			result := base.WithGuests(testCase.guests)

			if !slices.Equal(result.Artists(), testCase.expected) || !slices.Equal(base.Artists(), testCase.base) {
				t.Fatalf("result = %#v base = %#v", result.Artists(), base.Artists())
			}
		})
	}
}

func TestCreditFromRules(t *testing.T) {
	cases := []struct {
		name            string
		source          CreditSource
		expectedTitle   string
		expectedArtists string
		expectedError   error
	}{
		{"Spec_Heatseeker", CreditSource{Title: "HEATSEEKER (feat. Ethan Ross)", Artist: "$werve"}, "HEATSEEKER", "$werve; Ethan Ross", nil},
		{"Spec_Paperwork", CreditSource{Title: "PAPERWORK (feat. Ty Dolla $ign)", Artist: "¥$"}, "PAPERWORK", "¥$; Ty Dolla $ign", nil},
		{"Spec_DestinationCalabria", CreditSource{Title: "Destination Calabria (feat. Crystal Waters) [Radio Edit]", Artist: "Alex Gaudino"}, "Destination Calabria [Radio Edit]", "Alex Gaudino; Crystal Waters", nil},
		{"Spec_Cigarettes", CreditSource{Title: "Cigarettes On Patios (Remix) (feat. 24kgoldn)", Artist: "BabyJake"}, "Cigarettes On Patios (Remix)", "BabyJake; 24kgoldn", nil},
		{"Spec_BackInBlack", CreditSource{Title: "Back In Black", Artist: "AC/DC"}, "Back In Black", "AC/DC", nil},
		{"Spec_SongWith", CreditSource{Title: "Song (with A & B)", Artist: "X"}, "Song", "X; A; B", nil},
		{"Spec_IronMan", CreditSource{Title: "Iron Man (2009 Remaster)", Artist: "Black Sabbath"}, "Iron Man (2009 Remaster)", "Black Sabbath", nil},
		{"CR3_GuestAlreadyCredited", CreditSource{Title: "Song (feat. B)", Artist: "A; B"}, "Song", "A; B", nil},
		{"CR5_AlbumArtistWhenArtistEmpty", CreditSource{Title: "Song", AlbumArtist: "Band"}, "Song", "Band", nil},
		{"CR5_ArtistFromFolder", CreditSource{Path: "C:\\Music\\Queen - Greatest Hits\\01 Song.flac", Title: "Song"}, "Song", "Queen", nil},
		{"CR5_ArtistFromFileName", CreditSource{Path: "C:/Music/Downloads/Queen - Song.flac", Title: "Song"}, "Song", "Queen", nil},
		{"CR5_NoArtistAnywhere_ReturnsErrArtistMissing", CreditSource{Path: "C:/Music/song.flac", Title: "Song"}, "", "", ErrArtistMissing},
		{"CR5_EmptyPath_ReturnsErrArtistMissing", CreditSource{Title: "Song"}, "", "", ErrArtistMissing},
		{"TitleMissing_ReturnsErrTitleMissing", CreditSource{Artist: "A"}, "", "", ErrTitleMissing},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			credit, err := CreditFromRules(testCase.source)

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("error = %v, want %v", err, testCase.expectedError)
			}
			if err != nil {
				return
			}
			if credit.Title().String() != testCase.expectedTitle || credit.Artists().String() != testCase.expectedArtists {
				t.Fatalf("credit = %q / %q", credit.Title().String(), credit.Artists().String())
			}
		})
	}
}

func TestIsConsistentWith(t *testing.T) {
	source := CreditSource{Path: "C:/Music/Alex Gaudino/track.flac", Title: "Destination Calabria (feat. Crystal Waters) [Radio Edit]", Artist: "Alex Gaudino"}
	cases := []struct {
		name     string
		source   CreditSource
		title    string
		artists  []string
		expected bool
	}{
		{"CR7_InventedArtist_IsRejected", source, "Destination Calabria [Radio Edit]", []string{"Alex Gaudino", "Madonna"}, false},
		{"CR7_ArtistFromPath_IsAccepted", CreditSource{Path: "C:/Music/Crystal Waters/x.flac", Title: "Song", Artist: "Alex Gaudino"}, "Song", []string{"Alex Gaudino", "Crystal Waters"}, true},
		{"CR8_ChangedWord_IsRejected", source, "Destination Calabria [Club Edit]", []string{"Alex Gaudino"}, false},
		{"CR8_FeatRemoved_IsAccepted", source, "Destination Calabria [Radio Edit]", []string{"Alex Gaudino", "Crystal Waters"}, true},
		{"CR8_CaseOnlyDifference_IsAccepted", source, "destination calabria [radio edit]", []string{"alex gaudino"}, true},
		{"InvalidSource_IsRejected", CreditSource{Title: "Song"}, "Song", []string{"A"}, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			credit := NewCredit(mustTitle(t, testCase.title), mustArtists(t, testCase.artists...))

			if got := credit.IsConsistentWith(testCase.source); got != testCase.expected {
				t.Fatalf("IsConsistentWith = %v, want %v", got, testCase.expected)
			}
		})
	}
}

func mustArtists(t *testing.T, artists ...string) ArtistCredit {
	t.Helper()
	credit, err := NewArtistCredit(artists)
	if err != nil {
		t.Fatalf("building artists %v: %v", artists, err)
	}
	return credit
}
