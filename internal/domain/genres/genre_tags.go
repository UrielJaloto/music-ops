package genres

import (
	"slices"
	"strings"

	"github.com/UrielJaloto/music-ops/internal/domain/credits"
)

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

func (pool Pool) ClassifyByTags(evidence TagEvidence) Classification {
	for _, tag := range evidence.tags {
		target, found := pool.findTarget(tag)
		if !found {
			continue
		}
		details := ClassificationDetails{Genre: target.genre, Subgenre: target.subgenre, IsBrazilian: evidence.HasBrazilianSignal(), IsKnown: true}
		if classification, err := pool.NewClassification(details); err == nil {
			return classification
		}
	}
	return NewUnknownClassification()
}

func (pool Pool) findTarget(tag string) (tagTarget, bool) {
	target, found := tagTargets[tag]
	if !found {
		index := slices.IndexFunc(pool.definitions, func(definition GenreDefinition) bool { return strings.EqualFold(definition.Name, tag) })
		if index < 0 {
			return tagTarget{}, false
		}
		return tagTarget{genre: pool.definitions[index].Name}, true
	}
	if definition, inPool := pool.find(target.genre); inPool && !slices.Contains(definition.Subgenres, target.subgenre) {
		target.subgenre = ""
	}
	return target, found
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
		resolved = append(resolved, ballot.Classification.ApplyNationality(decisions[foldForComparison(ballot.MainArtist)]))
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
		key := foldForComparison(ballot.MainArtist)
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

func foldForComparison(artist string) string {
	return strings.ToLower(credits.CollapseWhitespace(artist))
}

func isBrazilianTag(tag string) bool {
	return slices.ContainsFunc(brazilianSignals, func(signal string) bool { return strings.Contains(tag, signal) })
}
