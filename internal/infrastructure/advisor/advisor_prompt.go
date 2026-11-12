package advisor

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/UrielJaloto/music-ops/internal/domain/genres"
	"github.com/UrielJaloto/music-ops/internal/services/classification"
	"github.com/UrielJaloto/music-ops/internal/services/crediting"
)

const (
	AdvisorPromptVersion  = "2026-10-1"
	genrePoolPlaceholder  = "{{GENRE_POOL}}"
	fingerprintLength     = 12
	shortKeyFormat        = "t%02d"
	creditSchemaName      = "credit_suggestions"
	genreSchemaName       = "genre_suggestions"
	nationalOnlyMarker    = " (national-only)"
	subgenreListSeparator = ", "
)

var ErrAdvisorAnswerMalformed = errors.New("advisor answer is malformed")

//go:embed advisor_prompts/credit_system.md
var creditSystemPrompt string

//go:embed advisor_prompts/genre_system.md
var genreSystemPromptTemplate string

func BuildCreditSystemPrompt() string {
	return creditSystemPrompt
}

func BuildGenreSystemPrompt(catalog genres.Pool) string {
	return strings.Replace(genreSystemPromptTemplate, genrePoolPlaceholder, renderGenrePool(catalog), 1)
}

func renderGenrePool(catalog genres.Pool) string {
	nationalOnly := catalog.ListNationalOnlyNames()
	subgenres := catalog.ListSubgenres()
	hints := catalog.ListHints()
	lines := make([]string, 0, len(catalog.ListNames()))
	for _, name := range catalog.ListNames() {
		entry := genrePoolEntry{name: name, hint: hints[name], subgenres: subgenres[name], isNationalOnly: slices.Contains(nationalOnly, name)}
		lines = append(lines, entry.renderLine())
	}
	return strings.Join(lines, "\n")
}

type genrePoolEntry struct {
	name           string
	hint           string
	subgenres      []string
	isNationalOnly bool
}

func (entry genrePoolEntry) renderLine() string {
	line := "- " + entry.name
	if entry.hint != "" {
		line += " (" + entry.hint + ")"
	}
	if entry.isNationalOnly {
		line += nationalOnlyMarker
	}
	if len(entry.subgenres) > 0 {
		line += ": " + strings.Join(entry.subgenres, subgenreListSeparator)
	}
	return line
}

func BuildCreditSchema() map[string]any {
	return buildItemsSchema(map[string]any{
		"key":     buildStringSchema(),
		"title":   buildStringSchema(),
		"artists": map[string]any{"type": "array", "items": buildStringSchema()},
	}, []string{"key", "title", "artists"})
}

func BuildGenreSchema(catalog genres.Pool) map[string]any {
	return buildItemsSchema(map[string]any{
		"key":          buildStringSchema(),
		"genre":        map[string]any{"type": "string", "enum": catalog.ListNames()},
		"subgenre":     map[string]any{"type": "string", "enum": append(catalog.ListSubgenreNames(), "")},
		"is_brazilian": map[string]any{"type": "boolean"},
		"is_known":     map[string]any{"type": "boolean"},
	}, []string{"key", "genre", "subgenre", "is_brazilian", "is_known"})
}

func buildStringSchema() map[string]any {
	return map[string]any{"type": "string"}
}

func buildItemsSchema(properties map[string]any, required []string) map[string]any {
	item := map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           map[string]any{"items": map[string]any{"type": "array", "items": item}},
		"required":             []string{"items"},
	}
}

func FingerprintCreditPrompt() string {
	return fingerprint(AdvisorPromptVersion + BuildCreditSystemPrompt() + fmt.Sprint(BuildCreditSchema()))
}

func FingerprintGenrePrompt(catalog genres.Pool) string {
	return fingerprint(AdvisorPromptVersion + BuildGenreSystemPrompt(catalog) + fmt.Sprint(BuildGenreSchema(catalog)))
}

func fingerprint(material string) string {
	digest := sha256.Sum256([]byte(material))
	return hex.EncodeToString(digest[:])[:fingerprintLength]
}

type creditMessageItem struct {
	Key         string `json:"key"`
	Path        string `json:"path"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	AlbumArtist string `json:"album_artist"`
}

type genreMessageItem struct {
	Key     string   `json:"key"`
	Path    string   `json:"path"`
	Title   string   `json:"title"`
	Artists []string `json:"artists"`
	Tags    []string `json:"tags"`
}

type advisorMessage[T any] struct {
	Items []T `json:"items"`
}

func BuildCreditUserMessage(requests []crediting.CreditRequest) ([]byte, map[string]string, error) {
	items := make([]creditMessageItem, 0, len(requests))
	keys := map[string]string{}
	for index, request := range requests {
		short := fmt.Sprintf(shortKeyFormat, index+1)
		keys[short] = request.Key
		source := request.Source
		items = append(items, creditMessageItem{Key: short, Path: source.Path, Title: source.Title, Artist: source.Artist, AlbumArtist: source.AlbumArtist})
	}
	message, err := json.Marshal(advisorMessage[creditMessageItem]{Items: items})
	return message, keys, err
}

func BuildGenreUserMessage(requests []classification.GenreRequest) ([]byte, map[string]string, error) {
	items := make([]genreMessageItem, 0, len(requests))
	keys := map[string]string{}
	for index, request := range requests {
		short := fmt.Sprintf(shortKeyFormat, index+1)
		keys[short] = request.Key
		items = append(items, genreMessageItem{Key: short, Path: request.Path, Title: request.Title, Artists: replaceNilWithEmpty(request.Artists), Tags: replaceNilWithEmpty(request.Tags)})
	}
	message, err := json.Marshal(advisorMessage[genreMessageItem]{Items: items})
	return message, keys, err
}

func replaceNilWithEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

type creditAnswerItem struct {
	Key     *string   `json:"key"`
	Title   *string   `json:"title"`
	Artists *[]string `json:"artists"`
}

type genreAnswerItem struct {
	Key         *string `json:"key"`
	Genre       *string `json:"genre"`
	Subgenre    *string `json:"subgenre"`
	IsBrazilian *bool   `json:"is_brazilian"`
	IsKnown     *bool   `json:"is_known"`
}

func DecodeCreditSuggestions(raw []byte, keys map[string]string) ([]crediting.CreditSuggestion, error) {
	var answer advisorMessage[creditAnswerItem]
	if err := json.Unmarshal(raw, &answer); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAdvisorAnswerMalformed, err)
	}
	suggestions := []crediting.CreditSuggestion{}
	for _, item := range answer.Items {
		key, isKnown := lookupMappedKey(item.Key, keys)
		if !isKnown || item.Title == nil || item.Artists == nil {
			continue
		}
		suggestions = append(suggestions, crediting.CreditSuggestion{Key: key, Title: *item.Title, Artists: *item.Artists})
	}
	return suggestions, nil
}

func DecodeGenreSuggestions(raw []byte, keys map[string]string) ([]classification.GenreSuggestion, error) {
	var answer advisorMessage[genreAnswerItem]
	if err := json.Unmarshal(raw, &answer); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAdvisorAnswerMalformed, err)
	}
	suggestions := []classification.GenreSuggestion{}
	for _, item := range answer.Items {
		key, isKnown := lookupMappedKey(item.Key, keys)
		if !isKnown || !item.isComplete() {
			continue
		}
		suggestions = append(suggestions, classification.GenreSuggestion{Key: key, Genre: *item.Genre, Subgenre: *item.Subgenre, IsBrazilian: *item.IsBrazilian, IsKnown: *item.IsKnown})
	}
	return suggestions, nil
}

func (item genreAnswerItem) isComplete() bool {
	return item.Genre != nil && item.Subgenre != nil && item.IsBrazilian != nil && item.IsKnown != nil
}

func lookupMappedKey(short *string, keys map[string]string) (string, bool) {
	if short == nil {
		return "", false
	}
	key, isKnown := keys[*short]
	return key, isKnown
}
