package domain

import (
	"slices"
	"strings"
)

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

type tagTarget struct {
	genre    string
	subgenre string
}

var brazilianSignals = []string{"brazil", "brasil", "nacional", "mpb", "funk carioca", "sertanejo", "pagode", "samba"}

var tagTargets = map[string]tagTarget{
	"hip-hop":             {genre: "Hip-Hop"},
	"hip hop":             {genre: "Hip-Hop"},
	"underground hip-hop": {genre: "Hip-Hop"},
	"rap":                 {genre: "Rap"},
	"brazilian rap":       {genre: "Rap"},
	"rap nacional":        {genre: "Rap"},
	"gangsta rap":         {genre: "Rap"},
	"trap":                {genre: "Rap"},
	"acoustic rap":        {genre: "Rap", subgenre: "Rap Acústico"},
	"heavy metal":         {genre: "Rock", subgenre: "Metal"},
	"metal":               {genre: "Rock", subgenre: "Metal"},
	"thrash metal":        {genre: "Rock", subgenre: "Metal"},
	"death metal":         {genre: "Rock", subgenre: "Metal"},
	"black metal":         {genre: "Rock", subgenre: "Metal"},
	"metalcore":           {genre: "Rock", subgenre: "Metal"},
	"nu metal":            {genre: "Rock", subgenre: "Metal"},
	"doom metal":          {genre: "Rock", subgenre: "Metal"},
	"punk":                {genre: "Rock", subgenre: "Punk"},
	"punk rock":           {genre: "Rock", subgenre: "Punk"},
	"pop punk":            {genre: "Rock", subgenre: "Punk"},
	"hardcore punk":       {genre: "Rock", subgenre: "Punk"},
	"post-punk":           {genre: "Rock", subgenre: "Punk"},
	"hard rock":           {genre: "Rock"},
	"classic rock":        {genre: "Rock"},
	"rock":                {genre: "Rock"},
	"brazilian rock":      {genre: "Rock"},
	"grunge":              {genre: "Rock"},
	"progressive rock":    {genre: "Rock"},
	"psychedelic rock":    {genre: "Rock"},
	"alternative rock":    {genre: "Indie / Alternative"},
	"indie":               {genre: "Indie / Alternative"},
	"indie rock":          {genre: "Indie / Alternative"},
	"alternative":         {genre: "Indie / Alternative"},
	"indie pop":           {genre: "Indie / Alternative"},
	"shoegaze":            {genre: "Indie / Alternative"},
	"pop":                 {genre: "Pop"},
	"pop rock":            {genre: "Pop"},
	"synthpop":            {genre: "Pop"},
	"k-pop":               {genre: "Pop"},
	"brazilian pop":       {genre: "Pop"},
	"dance":               {genre: "Electronic", subgenre: "Dance"},
	"edm":                 {genre: "Electronic", subgenre: "Dance"},
	"eurodance":           {genre: "Electronic", subgenre: "Dance"},
	"dance-pop":           {genre: "Electronic", subgenre: "Dance"},
	"house":               {genre: "Electronic", subgenre: "House"},
	"deep house":          {genre: "Electronic", subgenre: "House"},
	"tech house":          {genre: "Electronic", subgenre: "House"},
	"electro house":       {genre: "Electronic", subgenre: "House"},
	"progressive house":   {genre: "Electronic", subgenre: "House"},
	"ambient":             {genre: "Electronic", subgenre: "Ambient"},
	"chillout":            {genre: "Electronic", subgenre: "Ambient"},
	"electronic":          {genre: "Electronic"},
	"electronica":         {genre: "Electronic"},
	"techno":              {genre: "Electronic"},
	"trance":              {genre: "Electronic"},
	"dubstep":             {genre: "Electronic"},
	"drum and bass":       {genre: "Electronic"},
	"r&b":                 {genre: "R&B / Soul"},
	"rnb":                 {genre: "R&B / Soul"},
	"soul":                {genre: "R&B / Soul"},
	"neo-soul":            {genre: "R&B / Soul"},
	"jazz":                {genre: "Jazz / Blues"},
	"blues":               {genre: "Jazz / Blues"},
	"smooth jazz":         {genre: "Jazz / Blues"},
	"funk":                {genre: "Funk"},
	"phonk":               {genre: "Fonk"},
	"drift phonk":         {genre: "Fonk"},
	"brazilian phonk":     {genre: "Fonk"},
	"reggae":              {genre: "Reggae"},
	"dancehall":           {genre: "Reggae"},
	"reggaeton":           {genre: "Reggaeton"},
	"country":             {genre: "Country / Folk"},
	"folk":                {genre: "Country / Folk"},
	"americana":           {genre: "Country / Folk"},
	"classical":           {genre: "Classical"},
	"orchestral":          {genre: "Classical"},
	"acoustic":            {genre: "Acoustic"},
	"soundtrack":          {genre: "Soundtrack"},
	"video game music":    {genre: "Soundtrack"},
	"ost":                 {genre: "Soundtrack"},
	"score":               {genre: "Soundtrack"},
	"funk carioca":        {genre: "Funk BR", subgenre: "Funk Carioca"},
	"baile funk":          {genre: "Funk BR", subgenre: "Funk Carioca"},
	"funk brasileiro":     {genre: "Funk BR"},
	"funk melody":         {genre: "Funk BR", subgenre: "Melódico"},
	"mpb":                 {genre: "MPB"},
	"bossa nova":          {genre: "MPB"},
	"samba":               {genre: "Samba"},
	"pagode":              {genre: "Pagode"},
}

type TagEvidence struct {
	tags []string
}

func NewTagEvidence(tags []string) TagEvidence {
	normalized := make([]string, 0, len(tags))
	for _, tag := range tags {
		cleaned := strings.ToLower(strings.TrimSpace(tag))
		if cleaned == "" || slices.Contains(normalized, cleaned) {
			continue
		}
		normalized = append(normalized, cleaned)
	}
	return TagEvidence{tags: normalized}
}

func (evidence TagEvidence) Tags() []string {
	return slices.Clone(evidence.tags)
}

func (evidence TagEvidence) HasBrazilianSignal() bool {
	return slices.ContainsFunc(evidence.tags, isBrazilianTag)
}

func ClassifyByTags(evidence TagEvidence) Classification {
	for _, tag := range evidence.tags {
		target, found := tagTargets[tag]
		if !found {
			continue
		}
		return Classification{genre: target.genre, subgenre: target.subgenre, isBrazilian: evidence.HasBrazilianSignal(), isKnown: true}
	}
	return UnknownClassification()
}

type NationalityBallot struct {
	MainArtist     string
	Classification Classification
	Evidence       TagEvidence
}

func ResolveNationality(ballots []NationalityBallot) []Classification {
	decisions := decideNationalityPerArtist(ballots)
	resolved := make([]Classification, 0, len(ballots))
	for _, ballot := range ballots {
		resolved = append(resolved, ballot.Classification.WithNationality(decisions[artistKey(ballot.MainArtist)]))
	}
	return resolved
}

type nationalityTally struct {
	brazilianVotes     int
	internationalVotes int
	hasSignal          bool
}

func (tally nationalityTally) isBrazilian() bool {
	if tally.brazilianVotes != tally.internationalVotes {
		return tally.brazilianVotes > tally.internationalVotes
	}
	return tally.hasSignal
}

func decideNationalityPerArtist(ballots []NationalityBallot) map[string]bool {
	tallies := map[string]nationalityTally{}
	for _, ballot := range ballots {
		key := artistKey(ballot.MainArtist)
		tallies[key] = countBallot(tallies[key], ballot)
	}
	decisions := make(map[string]bool, len(tallies))
	for key, tally := range tallies {
		decisions[key] = tally.isBrazilian()
	}
	return decisions
}

func countBallot(tally nationalityTally, ballot NationalityBallot) nationalityTally {
	if ballot.Classification.IsBrazilian() {
		tally.brazilianVotes++
	} else {
		tally.internationalVotes++
	}
	tally.hasSignal = tally.hasSignal || ballot.Evidence.HasBrazilianSignal()
	return tally
}

func artistKey(artist string) string {
	return strings.ToLower(collapseWhitespace(artist))
}

func isBrazilianTag(tag string) bool {
	return slices.ContainsFunc(brazilianSignals, func(signal string) bool { return strings.Contains(tag, signal) })
}
