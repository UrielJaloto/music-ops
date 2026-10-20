package domain

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func FuzzSanitizePathComponent(f *testing.F) {
	for _, seed := range []string{"AC/DC", "CON", "name...", "a\x00b", "ab😀", "abé", strings.Repeat("a", 200)} {
		f.Add(seed, uint8(180))
	}
	f.Add("abc def", uint8(4))
	f.Fuzz(func(t *testing.T, raw string, limit uint8) {
		sanitized := SanitizePathComponent(raw, int(limit))

		assertSanitizedComponent(t, sanitized, int(limit))
		if again := SanitizePathComponent(sanitized, int(limit)); again != sanitized {
			t.Fatalf("not idempotent: %q -> %q", sanitized, again)
		}
	})
}

func FuzzNewFileName(f *testing.F) {
	f.Add("Back In Black", "AC/DC", "")
	f.Add("HEATSEEKER", "$werve", "Ethan Ross")
	f.Add(strings.Repeat("t", 300), "Main", "Guest")
	f.Fuzz(func(t *testing.T, rawTitle, mainArtist, guest string) {
		title, titleError := NewTitle(rawTitle)
		artists, artistError := NewArtistCredit([]string{mainArtist, guest})
		if titleError != nil || artistError != nil {
			return
		}

		name := NewFileName(NewCredit(title, artists)).String()

		if !strings.HasSuffix(name, FileExtension) {
			t.Fatalf("name %q lacks extension", name)
		}
		assertSanitizedComponent(t, strings.TrimSuffix(name, FileExtension), MaxFileNameUnits-utf16Units(FileExtension))
	})
}

func assertSanitizedComponent(t *testing.T, value string, limit int) {
	t.Helper()
	if !utf8.ValidString(value) {
		t.Fatalf("invalid UTF-8: %q", value)
	}
	if strings.ContainsAny(value, forbiddenPathRunes) || strings.ContainsFunc(value, unicode.IsControl) {
		t.Fatalf("forbidden rune in %q", value)
	}
	if strings.HasSuffix(value, ".") || strings.HasSuffix(value, " ") {
		t.Fatalf("trailing dot or space in %q", value)
	}
	if utf16Units(value) > limit {
		t.Fatalf("%q has %d units, limit %d", value, utf16Units(value), limit)
	}
}
