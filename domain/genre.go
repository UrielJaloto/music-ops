package domain

import "slices"

var (
	ErrGenreNotInPool     = newBusinessError("genre is not in the pool")
	ErrSubgenreNotInGenre = newBusinessError("subgenre does not belong to the genre")
)

const (
	NationalSuffix      = "Nacional"
	unknownGenreValue   = "desconhecido"
	genreJoinSeparator  = "; "
	nationalSuffixSpace = " "
)

type genreDefinition struct {
	name           string
	subgenres      []string
	isNationalOnly bool
	hint           string
}

var genrePool = []genreDefinition{
	{name: "Rock", subgenres: []string{"Metal", "Punk"}},
	{name: "Indie / Alternative"},
	{name: "Pop"},
	{name: "Hip-Hop"},
	{name: "Rap", subgenres: []string{"Rap Acústico"}},
	{name: "R&B / Soul"},
	{name: "Electronic", subgenres: []string{"Dance", "House", "Ambient"}},
	{name: "Jazz / Blues"},
	{name: "Funk", hint: "classic international funk"},
	{name: "Fonk", hint: "phonk, independent genre"},
	{name: "Reggae"},
	{name: "Reggaeton"},
	{name: "Country / Folk"},
	{name: "Classical"},
	{name: "Acoustic"},
	{name: "Soundtrack"},
	{name: "Funk BR", subgenres: []string{"Funk Carioca", "Melódico", "Remix", "Afrobeats"}, isNationalOnly: true},
	{name: "MPB", isNationalOnly: true},
	{name: "Samba", isNationalOnly: true},
	{name: "Pagode", isNationalOnly: true},
}

func GenreNames() []string {
	names := make([]string, 0, len(genrePool))
	for _, definition := range genrePool {
		names = append(names, definition.name)
	}
	return names
}

func SubgenreNames() []string {
	names := []string{}
	for _, definition := range genrePool {
		names = append(names, definition.subgenres...)
	}
	return names
}

func GenreHints() map[string]string {
	hints := map[string]string{}
	for _, definition := range genrePool {
		if definition.hint == "" {
			continue
		}
		hints[definition.name] = definition.hint
	}
	return hints
}

type ClassificationDetails struct {
	Genre       string
	Subgenre    string
	IsBrazilian bool
	IsKnown     bool
}

type Classification struct {
	genre       string
	subgenre    string
	isBrazilian bool
	isKnown     bool
}

func NewClassification(details ClassificationDetails) (Classification, error) {
	if !details.IsKnown {
		return UnknownClassification(), nil
	}
	definition, found := findGenreDefinition(details.Genre)
	if !found {
		return Classification{}, ErrGenreNotInPool
	}
	if details.Subgenre != "" && !slices.Contains(definition.subgenres, details.Subgenre) {
		return Classification{}, ErrSubgenreNotInGenre
	}
	return Classification{genre: details.Genre, subgenre: details.Subgenre, isBrazilian: details.IsBrazilian, isKnown: true}, nil
}

func UnknownClassification() Classification {
	return Classification{}
}

func (classification Classification) WithNationality(isBrazilian bool) Classification {
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
	if classification.isSuffixed() {
		return Genre{value: joinGenreParts(withNationalSuffix(classification.genre), withNationalSuffix(classification.subgenre))}
	}
	return Genre{value: joinGenreParts(classification.genre, classification.subgenre)}
}

func (classification Classification) isSuffixed() bool {
	definition, _ := findGenreDefinition(classification.genre)
	return classification.isBrazilian && !definition.isNationalOnly
}

type Genre struct {
	value string
}

func (genre Genre) String() string {
	return genre.value
}

func findGenreDefinition(name string) (genreDefinition, bool) {
	index := slices.IndexFunc(genrePool, func(definition genreDefinition) bool { return definition.name == name })
	if index < 0 {
		return genreDefinition{}, false
	}
	return genrePool[index], true
}

func withNationalSuffix(name string) string {
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
