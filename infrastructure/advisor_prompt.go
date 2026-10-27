package infrastructure

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/UrielJaloto/music-ops/domain"
	"github.com/UrielJaloto/music-ops/services"
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

func CreditSystemPrompt() string {
	return creditSystemPrompt
}

func GenreSystemPrompt() string {
	return strings.Replace(genreSystemPromptTemplate, genrePoolPlaceholder, renderGenrePool(), 1)
}

func renderGenrePool() string {
	nationalOnly := domain.NationalOnlyGenreNames()
	subgenres := domain.GenreSubgenres()
	hints := domain.GenreHints()
	lines := make([]string, 0, len(domain.GenreNames()))
	for _, name := range domain.GenreNames() {
		lines = append(lines, genrePoolLine(name, subgenres[name], hints[name], slices.Contains(nationalOnly, name)))
	}
	return strings.Join(lines, "\n")
}

func genrePoolLine(name string, subgenres []string, hint string, isNationalOnly bool) string {
	line := "- " + name
	if hint != "" {
		line += " (" + hint + ")"
	}
	if isNationalOnly {
		line += nationalOnlyMarker
	}
	if len(subgenres) > 0 {
		line += ": " + strings.Join(subgenres, subgenreListSeparator)
	}
	return line
}

func CreditSchema() map[string]any {
	return itemsSchema(map[string]any{
		"key":     stringSchema(),
		"title":   stringSchema(),
		"artists": map[string]any{"type": "array", "items": stringSchema()},
	}, []string{"key", "title", "artists"})
}

func GenreSchema() map[string]any {
	return itemsSchema(map[string]any{
		"key":          stringSchema(),
		"genre":        map[string]any{"type": "string", "enum": domain.GenreNames()},
		"subgenre":     map[string]any{"type": "string", "enum": append(domain.SubgenreNames(), "")},
		"is_brazilian": map[string]any{"type": "boolean"},
		"is_known":     map[string]any{"type": "boolean"},
	}, []string{"key", "genre", "subgenre", "is_brazilian", "is_known"})
}

func stringSchema() map[string]any {
	return map[string]any{"type": "string"}
}

func itemsSchema(properties map[string]any, required []string) map[string]any {
	item := map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           map[string]any{"items": map[string]any{"type": "array", "items": item}},
		"required":             []string{"items"},
	}
}

func PromptFingerprint() string {
	material := AdvisorPromptVersion + CreditSystemPrompt() + GenreSystemPrompt() + fmt.Sprint(CreditSchema()) + fmt.Sprint(GenreSchema())
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

func BuildCreditUserMessage(requests []services.CreditRequest) ([]byte, map[string]string, error) {
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

func BuildGenreUserMessage(requests []services.GenreRequest) ([]byte, map[string]string, error) {
	items := make([]genreMessageItem, 0, len(requests))
	keys := map[string]string{}
	for index, request := range requests {
		short := fmt.Sprintf(shortKeyFormat, index+1)
		keys[short] = request.Key
		items = append(items, genreMessageItem{Key: short, Path: request.Path, Title: request.Title, Artists: nonNilStrings(request.Artists), Tags: nonNilStrings(request.Tags)})
	}
	message, err := json.Marshal(advisorMessage[genreMessageItem]{Items: items})
	return message, keys, err
}

func nonNilStrings(values []string) []string {
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

func DecodeCreditSuggestions(raw []byte, keys map[string]string) ([]services.CreditSuggestion, error) {
	var answer advisorMessage[creditAnswerItem]
	if err := json.Unmarshal(raw, &answer); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAdvisorAnswerMalformed, err)
	}
	suggestions := []services.CreditSuggestion{}
	for _, item := range answer.Items {
		key, isKnown := mappedKey(item.Key, keys)
		if !isKnown || item.Title == nil || item.Artists == nil {
			continue
		}
		suggestions = append(suggestions, services.CreditSuggestion{Key: key, Title: *item.Title, Artists: *item.Artists})
	}
	return suggestions, nil
}

func DecodeGenreSuggestions(raw []byte, keys map[string]string) ([]services.GenreSuggestion, error) {
	var answer advisorMessage[genreAnswerItem]
	if err := json.Unmarshal(raw, &answer); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAdvisorAnswerMalformed, err)
	}
	suggestions := []services.GenreSuggestion{}
	for _, item := range answer.Items {
		key, isKnown := mappedKey(item.Key, keys)
		if !isKnown || !item.isComplete() {
			continue
		}
		suggestions = append(suggestions, services.GenreSuggestion{Key: key, Genre: *item.Genre, Subgenre: *item.Subgenre, IsBrazilian: *item.IsBrazilian, IsKnown: *item.IsKnown})
	}
	return suggestions, nil
}

func (item genreAnswerItem) isComplete() bool {
	return item.Genre != nil && item.Subgenre != nil && item.IsBrazilian != nil && item.IsKnown != nil
}

func mappedKey(short *string, keys map[string]string) (string, bool) {
	if short == nil {
		return "", false
	}
	key, isKnown := keys[*short]
	return key, isKnown
}
