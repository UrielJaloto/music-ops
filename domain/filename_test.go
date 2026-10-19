package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestSanitizePathComponent(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		maxUnits int
		expected string
	}{
		{"FN3_Backslash", `a\b`, 180, "ab"},
		{"FN3_Slash", "AC/DC", 180, "ACDC"},
		{"FN3_Colon", "a:b", 180, "ab"},
		{"FN3_Asterisk", "a*b", 180, "ab"},
		{"FN3_Question", "a?b", 180, "ab"},
		{"FN3_Quote", `a"b`, 180, "ab"},
		{"FN3_Less", "a<b", 180, "ab"},
		{"FN3_Greater", "a>b", 180, "ab"},
		{"FN3_Pipe", "a|b", 180, "ab"},
		{"FN3_Control", "a\x00b\x1fc", 180, "abc"},
		{"FN3_WhitespaceCollapsed", "  a \t  b  ", 180, "a b"},
		{"FN3_TrailingDotsTrimmed", "name...", 180, "name"},
		{"FN3_TrailingDotSpaceMix", "name. . ", 180, "name"},
		{"FN4_ReservedCon", "CON", 180, "CON_"},
		{"FN4_ReservedLowercase", "nul", 180, "nul_"},
		{"FN4_ReservedWithExtension", "com1.txt", 180, "com1_.txt"},
		{"FN4_ReservedLpt9", "LPT9", 180, "LPT9_"},
		{"FN4_NotReservedLonger", "CONSOLE", 180, "CONSOLE"},
		{"FN4_NotReservedCom0", "COM0", 180, "COM0"},
		{"FN5_Limit179", strings.Repeat("a", 179), 180, strings.Repeat("a", 179)},
		{"FN5_Limit180", strings.Repeat("a", 180), 180, strings.Repeat("a", 180)},
		{"FN5_Limit181", strings.Repeat("a", 181), 180, strings.Repeat("a", 180)},
		{"FN5_EmojiAtCutIsNotSplit", "ab😀", 3, "ab"},
		{"FN5_EmojiFits", "ab😀", 4, "ab😀"},
		{"FN5_CombiningMarkAtCut", "abé", 3, "ab"},
		{"FN5_CombiningMarkFits", "abé", 4, "abé"},
		{"FN5_CutBeforeSpaceTrims", "abc def", 4, "abc"},
		{"Empty", "", 180, ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			sanitized := SanitizePathComponent(testCase.raw, testCase.maxUnits)

			if sanitized != testCase.expected {
				t.Fatalf("sanitized = %q, want %q", sanitized, testCase.expected)
			}
			if again := SanitizePathComponent(sanitized, testCase.maxUnits); again != sanitized {
				t.Fatalf("not idempotent: %q -> %q", sanitized, again)
			}
		})
	}
}

func TestNewFileName(t *testing.T) {
	longTitle := strings.Repeat("t", 300)
	cases := []struct {
		name     string
		title    string
		artists  []string
		expected string
	}{
		{"FN1_SingleArtist", "Back In Black", []string{"AC/DC"}, "ACDC - Back In Black.m4a"},
		{"FN2_WithGuests", "HEATSEEKER", []string{"$werve", "Ethan Ross"}, "$werve - HEATSEEKER (feat. Ethan Ross).m4a"},
		{"FN2_TwoGuests", "Song", []string{"X", "A", "B"}, "X - Song (feat. A; B).m4a"},
		{"FN2_RadioEdit", "Destination Calabria [Radio Edit]", []string{"Alex Gaudino", "Crystal Waters"}, "Alex Gaudino - Destination Calabria [Radio Edit] (feat. Crystal Waters).m4a"},
		{"FN3_ForbiddenInTitle", "What?: Yes", []string{"A"}, "A - What Yes.m4a"},
		{"FN5_DropsFeatFirst", strings.Repeat("t", 160), []string{"Main", "Guest Artist Name"}, "Main - " + strings.Repeat("t", 160) + ".m4a"},
		{"FN5_TruncatesTitleKeepsArtist", longTitle, []string{"Main", "Guest"}, "Main - " + strings.Repeat("t", 169) + ".m4a"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			credit := NewCredit(mustTitle(t, testCase.title), mustArtists(t, testCase.artists...))

			name := NewFileName(credit).String()

			if name != testCase.expected {
				t.Fatalf("name = %q, want %q", name, testCase.expected)
			}
			if utf16Units(name) > MaxFileNameUnits {
				t.Fatalf("name has %d units", utf16Units(name))
			}
		})
	}
}

func TestNewTargetPath(t *testing.T) {
	fileName := NewFileName(NewCredit(mustTitle(t, "Song"), mustArtists(t, "Band")))
	cases := []struct {
		name          string
		details       TargetPathDetails
		expected      string
		expectedError error
	}{
		{"FN6_Flat", TargetPathDetails{Layout: LayoutFlat, Template: "{album}", Album: "X", FileName: fileName}, "Band - Song.m4a", nil},
		{"FN6_AlbumFolders", TargetPathDetails{Layout: LayoutAlbumFolders, Template: "{albumartist} - {album}", AlbumArtist: "Band", Album: "Best", FileName: fileName}, "Band - Best/Band - Song.m4a", nil},
		{"FN6_MissingAlbum", TargetPathDetails{Layout: LayoutAlbumFolders, Template: "{albumartist} - {album}", AlbumArtist: "Band", FileName: fileName}, "Band - Unknown/Band - Song.m4a", nil},
		{"FN6_YearFromDate", TargetPathDetails{Layout: LayoutAlbumFolders, Template: "{year} {album}", Album: "Best", Date: "1999-05-01", FileName: fileName}, "1999 Best/Band - Song.m4a", nil},
		{"FN6_ShortDateKept", TargetPathDetails{Layout: LayoutAlbumFolders, Template: "{year}", Date: "99", FileName: fileName}, "99/Band - Song.m4a", nil},
		{"FN6_FolderSanitized", TargetPathDetails{Layout: LayoutAlbumFolders, Template: "{album}", Album: "A/B: C.", FileName: fileName}, "AB C/Band - Song.m4a", nil},
		{"FN6_EmptyTemplate", TargetPathDetails{Layout: LayoutAlbumFolders, Template: "", FileName: fileName}, "Unknown/Band - Song.m4a", nil},
		{"UnknownPlaceholder_ReturnsError", TargetPathDetails{Layout: LayoutAlbumFolders, Template: "{genre}", FileName: fileName}, "", ErrTemplatePlaceholderUnknown},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path, err := NewTargetPath(testCase.details)

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("error = %v, want %v", err, testCase.expectedError)
			}
			if path.Relative() != testCase.expected {
				t.Fatalf("relative = %q, want %q", path.Relative(), testCase.expected)
			}
		})
	}
}

func TestDuplicateKey(t *testing.T) {
	first := TargetPath{relative: "Band - Song.m4a"}
	second := TargetPath{relative: "band  -  SONG.m4a"}
	other := TargetPath{relative: "Band - Other.m4a"}

	if first.DuplicateKey() != second.DuplicateKey() {
		t.Fatalf("keys differ: %q %q", first.DuplicateKey(), second.DuplicateKey())
	}
	if first.DuplicateKey() == other.DuplicateKey() {
		t.Fatal("different paths share a key")
	}
}
