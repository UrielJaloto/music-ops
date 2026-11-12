package genres

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/UrielJaloto/music-ops/internal/domain/credits"
)

var (
	ErrGenreNotInPool         = errors.New("genre is not in the pool")
	ErrGenreFormatInvalid     = errors.New(`genre must be "Genre" or "Genre; Subgenre"`)
	ErrSubgenreNotInGenre     = errors.New("subgenre does not belong to the genre")
	ErrGenrePoolEmpty         = errors.New("genre pool is empty")
	ErrGenreDefinitionInvalid = errors.New("genre definition is invalid")
)

const (
	NationalSuffix      = "Nacional"
	unknownGenreValue   = "desconhecido"
	genreJoinSeparator  = "; "
	nationalSuffixSpace = " "
	listSeparator       = ", "
)

type GenreDefinition struct {
	Name           string
	Subgenres      []string
	IsNationalOnly bool
	Hint           string
}

var defaultDefinitions = []GenreDefinition{
	{Name: "Rock", Subgenres: []string{"Metal", "Punk"}},
	{Name: "Indie / Alternative"},
	{Name: "Pop"},
	{Name: "Hip-Hop"},
	{Name: "Rap", Subgenres: []string{"Rap Acústico"}},
	{Name: "R&B / Soul"},
	{Name: "Electronic", Subgenres: []string{"Dance", "House", "Ambient"}},
	{Name: "Jazz / Blues"},
	{Name: "Funk", Hint: "classic international funk"},
	{Name: "Fonk", Hint: "phonk, independent genre"},
	{Name: "Reggae"},
	{Name: "Reggaeton"},
	{Name: "Country / Folk"},
	{Name: "Classical"},
	{Name: "Acoustic"},
	{Name: "Soundtrack"},
	{Name: "Funk BR", Subgenres: []string{"Funk Carioca", "Melódico", "Remix", "Afrobeats"}, IsNationalOnly: true},
	{Name: "MPB", IsNationalOnly: true},
	{Name: "Samba", IsNationalOnly: true},
	{Name: "Pagode", IsNationalOnly: true},
}

type Pool struct {
	definitions []GenreDefinition
}

func NewDefaultPool() Pool {
	return Pool{definitions: cloneDefinitions(defaultDefinitions)}
}

func NewPool(definitions []GenreDefinition) (Pool, error) {
	if len(definitions) == 0 {
		return Pool{}, ErrGenrePoolEmpty
	}
	cleaned := make([]GenreDefinition, 0, len(definitions))
	for _, definition := range definitions {
		definition = definition.clean()
		if err := definition.validateAgainst(cleaned); err != nil {
			return Pool{}, err
		}
		cleaned = append(cleaned, definition)
	}
	return Pool{definitions: cleaned}, nil
}

func (definition GenreDefinition) clean() GenreDefinition {
	subgenres := make([]string, 0, len(definition.Subgenres))
	for _, subgenre := range definition.Subgenres {
		subgenres = append(subgenres, credits.CollapseWhitespace(subgenre))
	}
	return GenreDefinition{
		Name: credits.CollapseWhitespace(definition.Name), Subgenres: subgenres,
		IsNationalOnly: definition.IsNationalOnly, Hint: credits.CollapseWhitespace(definition.Hint),
	}
}

func (definition GenreDefinition) validateAgainst(accepted []GenreDefinition) error {
	names := append([]string{definition.Name}, definition.Subgenres...)
	for index, name := range names {
		if !isValidGenreName(name) || slices.ContainsFunc(names[:index], matchesIgnoringCase(name)) {
			return fmt.Errorf("%w: %q", ErrGenreDefinitionInvalid, name)
		}
	}
	if slices.ContainsFunc(accepted, func(other GenreDefinition) bool { return strings.EqualFold(other.Name, definition.Name) }) {
		return fmt.Errorf("%w: %q is defined twice", ErrGenreDefinitionInvalid, definition.Name)
	}
	return nil
}

func isValidGenreName(name string) bool {
	return name != "" && !strings.Contains(name, genreJoinSeparator[:1]) && !strings.EqualFold(name, unknownGenreValue)
}

func matchesIgnoringCase(name string) func(string) bool {
	return func(candidate string) bool { return strings.EqualFold(candidate, name) }
}

func cloneDefinitions(definitions []GenreDefinition) []GenreDefinition {
	cloned := make([]GenreDefinition, 0, len(definitions))
	for _, definition := range definitions {
		definition.Subgenres = slices.Clone(definition.Subgenres)
		cloned = append(cloned, definition)
	}
	return cloned
}

func (pool Pool) Definitions() []GenreDefinition {
	return cloneDefinitions(pool.definitions)
}

func (pool Pool) ListNames() []string {
	names := make([]string, 0, len(pool.definitions))
	for _, definition := range pool.definitions {
		names = append(names, definition.Name)
	}
	return names
}

func (pool Pool) ListSubgenreNames() []string {
	names := []string{}
	for _, definition := range pool.definitions {
		for _, subgenre := range definition.Subgenres {
			if !slices.Contains(names, subgenre) {
				names = append(names, subgenre)
			}
		}
	}
	return names
}

func (pool Pool) ListHints() map[string]string {
	hints := map[string]string{}
	for _, definition := range pool.definitions {
		if definition.Hint != "" {
			hints[definition.Name] = definition.Hint
		}
	}
	return hints
}

func (pool Pool) ListNationalOnlyNames() []string {
	names := []string{}
	for _, definition := range pool.definitions {
		if definition.IsNationalOnly {
			names = append(names, definition.Name)
		}
	}
	return names
}

func (pool Pool) ListSubgenres() map[string][]string {
	subgenres := map[string][]string{}
	for _, definition := range pool.definitions {
		if len(definition.Subgenres) > 0 {
			subgenres[definition.Name] = slices.Clone(definition.Subgenres)
		}
	}
	return subgenres
}

type ClassificationDetails struct {
	Genre       string
	Subgenre    string
	IsBrazilian bool
	IsKnown     bool
}

type Classification struct {
	genre          string
	subgenre       string
	isBrazilian    bool
	isKnown        bool
	isNationalOnly bool
}

func (pool Pool) NewClassification(details ClassificationDetails) (Classification, error) {
	if !details.IsKnown {
		return NewUnknownClassification(), nil
	}
	definition, found := pool.find(details.Genre)
	if !found {
		return Classification{}, ErrGenreNotInPool
	}
	if details.Subgenre != "" && !slices.Contains(definition.Subgenres, details.Subgenre) {
		return Classification{}, ErrSubgenreNotInGenre
	}
	return Classification{
		genre: details.Genre, subgenre: details.Subgenre, isBrazilian: details.IsBrazilian,
		isKnown: true, isNationalOnly: definition.IsNationalOnly,
	}, nil
}

func (pool Pool) ParseGenre(value string) (Classification, error) {
	parts := strings.Split(value, genreJoinSeparator[:1])
	if len(parts) > 2 {
		return Classification{}, fmt.Errorf("%w: %q", ErrGenreFormatInvalid, value)
	}
	name, isBrazilian := cutNationalSuffix(parts[0])
	index := slices.IndexFunc(pool.definitions, func(definition GenreDefinition) bool { return strings.EqualFold(definition.Name, name) })
	if index < 0 {
		return Classification{}, fmt.Errorf("%w: %q; use one of: %s", ErrGenreNotInPool, name, strings.Join(pool.ListNames(), listSeparator))
	}
	definition, subgenre := pool.definitions[index], ""
	if len(parts) == 2 {
		name, _ := cutNationalSuffix(parts[1])
		position := slices.IndexFunc(definition.Subgenres, matchesIgnoringCase(name))
		if position < 0 {
			return Classification{}, fmt.Errorf("%w: %q; sub-genres of %s: %s", ErrSubgenreNotInGenre, name, definition.Name, definition.formatSubgenres())
		}
		subgenre = definition.Subgenres[position]
	}
	return pool.NewClassification(ClassificationDetails{Genre: definition.Name, Subgenre: subgenre, IsBrazilian: isBrazilian, IsKnown: true})
}

func (definition GenreDefinition) formatSubgenres() string {
	if len(definition.Subgenres) == 0 {
		return "none"
	}
	return strings.Join(definition.Subgenres, listSeparator)
}

func cutNationalSuffix(value string) (string, bool) {
	trimmed := strings.Join(strings.Fields(value), " ")
	suffix := nationalSuffixSpace + NationalSuffix
	if len(trimmed) > len(suffix) && strings.EqualFold(trimmed[len(trimmed)-len(suffix):], suffix) {
		return trimmed[:len(trimmed)-len(suffix)], true
	}
	return trimmed, false
}

func (pool Pool) find(name string) (GenreDefinition, bool) {
	index := slices.IndexFunc(pool.definitions, func(definition GenreDefinition) bool { return definition.Name == name })
	if index < 0 {
		return GenreDefinition{}, false
	}
	return pool.definitions[index], true
}

func NewUnknownClassification() Classification {
	return Classification{}
}

func (classification Classification) ApplyNationality(isBrazilian bool) Classification {
	changed := classification
	changed.isBrazilian = isBrazilian
	return changed
}

func (classification Classification) IsKnown() bool {
	return classification.isKnown
}

func (classification Classification) IsBrazilian() bool {
	return classification.isBrazilian
}

func (classification Classification) Genre() Genre {
	if !classification.isKnown {
		return Genre{value: unknownGenreValue}
	}
	if classification.isBrazilian && !classification.isNationalOnly {
		return Genre{value: joinGenreParts(appendNationalSuffix(classification.genre), appendNationalSuffix(classification.subgenre))}
	}
	return Genre{value: joinGenreParts(classification.genre, classification.subgenre)}
}

type Genre struct {
	value string
}

func (genre Genre) String() string {
	return genre.value
}

func appendNationalSuffix(name string) string {
	if name == "" {
		return ""
	}
	return name + nationalSuffixSpace + NationalSuffix
}

func joinGenreParts(genre, subgenre string) string {
	if subgenre == "" {
		return genre
	}
	return genre + genreJoinSeparator + subgenre
}
