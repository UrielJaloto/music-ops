package infrastructure

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/UrielJaloto/music-ops/domain"
	"github.com/UrielJaloto/music-ops/services"
)

func readGolden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(filepath.Join("testdata", "golden", name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func indentedJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestAdvisorPrompts_Golden(t *testing.T) {
	cases := []struct {
		golden string
		actual func(t *testing.T) string
	}{
		{golden: "advisor_credit_system.golden", actual: func(*testing.T) string { return CreditSystemPrompt() }},
		{golden: "advisor_genre_system.golden", actual: func(*testing.T) string { return GenreSystemPrompt() }},
		{golden: "advisor_credit_schema.json", actual: func(t *testing.T) string { return indentedJSON(t, CreditSchema()) }},
		{golden: "advisor_genre_schema.json", actual: func(t *testing.T) string { return indentedJSON(t, GenreSchema()) }},
	}
	for _, testCase := range cases {
		t.Run(testCase.golden, func(t *testing.T) {
			if diff := cmp.Diff(readGolden(t, testCase.golden), testCase.actual(t)); diff != "" {
				t.Fatalf("golden (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGenreSchema_EnumsEqualDomainLists(t *testing.T) {
	item := GenreSchema()["properties"].(map[string]any)["items"].(map[string]any)["items"].(map[string]any)
	properties := item["properties"].(map[string]any)

	genres := properties["genre"].(map[string]any)["enum"]
	subgenres := properties["subgenre"].(map[string]any)["enum"]

	if diff := cmp.Diff(domain.GenreNames(), genres); diff != "" {
		t.Fatalf("genres (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(append(domain.SubgenreNames(), ""), subgenres); diff != "" {
		t.Fatalf("subgenres (-want +got):\n%s", diff)
	}
	if item["additionalProperties"] != false || len(item["required"].([]string)) != 5 {
		t.Fatalf("item = %v", item)
	}
}

func TestGenreSystemPrompt_ListsWholePool(t *testing.T) {
	prompt := GenreSystemPrompt()

	for _, name := range domain.GenreNames() {
		if !regexp.MustCompile(`(?m)^- ` + regexp.QuoteMeta(name) + `\b`).MatchString(prompt) {
			t.Fatalf("genre %s missing from prompt", name)
		}
	}
	if regexp.MustCompile(`\{\{`).MatchString(prompt) {
		t.Fatal("placeholder left in prompt")
	}
}

func TestPromptFingerprint(t *testing.T) {
	fingerprint := PromptFingerprint()

	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(fingerprint) || fingerprint != PromptFingerprint() {
		t.Fatalf("fingerprint = %s", fingerprint)
	}
}

func TestBuildCreditUserMessage(t *testing.T) {
	requests := []services.CreditRequest{
		{Key: "track-a", Source: domain.CreditSource{Path: "A - B/01.flac", Title: "Song (feat. X)", Artist: "A", AlbumArtist: "A"}},
		{Key: "track-b", Source: domain.CreditSource{Title: "Other"}},
	}

	message, keys, err := BuildCreditUserMessage(requests)

	expected := `{"items":[{"key":"t01","path":"A - B/01.flac","title":"Song (feat. X)","artist":"A","album_artist":"A"},{"key":"t02","path":"","title":"Other","artist":"","album_artist":""}]}`
	if err != nil || string(message) != expected || keys["t01"] != "track-a" || keys["t02"] != "track-b" || len(keys) != 2 {
		t.Fatalf("message = %s keys = %v err = %v", message, keys, err)
	}
}

func TestBuildGenreUserMessage(t *testing.T) {
	requests := []services.GenreRequest{{Key: "k1", Path: "p", Title: "T", Artists: []string{"A", "B"}, Tags: []string{"rock"}}, {Key: "k2"}}

	message, keys, err := BuildGenreUserMessage(requests)

	expected := `{"items":[{"key":"t01","path":"p","title":"T","artists":["A","B"],"tags":["rock"]},{"key":"t02","path":"","title":"","artists":[],"tags":[]}]}`
	if err != nil || string(message) != expected || keys["t02"] != "k2" {
		t.Fatalf("message = %s keys = %v err = %v", message, keys, err)
	}
}

func TestDecodeCreditSuggestions(t *testing.T) {
	keys := map[string]string{"t01": "track-a", "t02": "track-b"}
	raw := `{"items":[
		{"key":"t01","title":"Song","artists":["A","X"]},
		{"key":"t99","title":"Ghost","artists":["G"]},
		{"key":"t02","artists":["B"]},
		{"title":"No key","artists":["C"]}
	]}`

	suggestions, err := DecodeCreditSuggestions([]byte(raw), keys)

	expected := []services.CreditSuggestion{{Key: "track-a", Title: "Song", Artists: []string{"A", "X"}}}
	if diff := cmp.Diff(expected, suggestions); diff != "" || err != nil {
		t.Fatalf("suggestions (-want +got):\n%s err = %v", diff, err)
	}
}

func TestDecodeGenreSuggestions(t *testing.T) {
	keys := map[string]string{"t01": "k1", "t02": "k2"}
	raw := `{"items":[
		{"key":"t01","genre":"Rock","subgenre":"Metal","is_brazilian":false,"is_known":true},
		{"key":"t02","genre":"Pop","subgenre":"","is_known":true},
		{"key":"t03","genre":"Pop","subgenre":"","is_brazilian":true,"is_known":true}
	]}`

	suggestions, err := DecodeGenreSuggestions([]byte(raw), keys)

	expected := []services.GenreSuggestion{{Key: "k1", Genre: "Rock", Subgenre: "Metal", IsKnown: true}}
	if diff := cmp.Diff(expected, suggestions); diff != "" || err != nil {
		t.Fatalf("suggestions (-want +got):\n%s err = %v", diff, err)
	}
}

func TestDecodeSuggestions_Malformed(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{name: "Truncated", raw: `{"items":[{"key":"t01"`},
		{name: "NotJSON", raw: `Sure! Here is the answer`},
		{name: "WrongShape", raw: `{"items":{"key":"t01"}}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, creditErr := DecodeCreditSuggestions([]byte(testCase.raw), map[string]string{})
			_, genreErr := DecodeGenreSuggestions([]byte(testCase.raw), map[string]string{})

			if !errors.Is(creditErr, ErrAdvisorAnswerMalformed) || !errors.Is(genreErr, ErrAdvisorAnswerMalformed) {
				t.Fatalf("errors = %v %v", creditErr, genreErr)
			}
		})
	}
}
