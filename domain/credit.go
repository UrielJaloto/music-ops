package domain

import (
	"regexp"
	"slices"
	"strings"
)

var (
	ErrTitleMissing  = newBusinessError("title is missing")
	ErrArtistMissing = newBusinessError("artist is missing")
)

const (
	artistTagSeparator   = ";"
	artistJoinSeparator  = "; "
	pathArtistSeparator  = " - "
	windowsPathSeparator = "\\"
	slashPathSeparator   = "/"
)

var (
	bracketedFeaturingPattern = regexp.MustCompile(`(?i)[(\[]\s*(?:feat\.?|ft\.?|featuring|with)\s+([^)\]]+)[)\]]`)
	trailingFeaturingPattern  = regexp.MustCompile(`(?i)\s+-?\s*(?:feat\.?|ft\.?|featuring)\s+(.+)$`)
	guestSeparatorPattern     = regexp.MustCompile(`(?i)\s*[,&]\s*|\s+(?:and|e|x)\s+`)
	emptyBracketsPattern      = regexp.MustCompile(`\(\s*\)|\[\s*\]`)
)

type Title struct {
	value string
}

func NewTitle(raw string) (Title, error) {
	collapsed := collapseWhitespace(raw)
	if collapsed == "" {
		return Title{}, ErrTitleMissing
	}
	return Title{value: collapsed}, nil
}

func (title Title) String() string {
	return title.value
}

func (title Title) WithoutFeaturing() (Title, []string) {
	remaining, guests := removeBracketedFeaturing(title.value)
	remaining, trailingGuests := removeTrailingFeaturing(remaining)
	cleaned := collapseWhitespace(emptyBracketsPattern.ReplaceAllString(remaining, " "))
	if cleaned == "" {
		return title, []string{}
	}
	return Title{value: cleaned}, append(guests, trailingGuests...)
}

func removeBracketedFeaturing(value string) (string, []string) {
	matches := bracketedFeaturingPattern.FindAllStringSubmatch(value, -1)
	guests := make([]string, 0, len(matches))
	for _, match := range matches {
		guests = append(guests, splitGuests(match[1])...)
	}
	return bracketedFeaturingPattern.ReplaceAllString(value, " "), guests
}

func removeTrailingFeaturing(value string) (string, []string) {
	match := trailingFeaturingPattern.FindStringSubmatchIndex(value)
	if match == nil {
		return value, []string{}
	}
	return value[:match[0]], splitGuests(value[match[2]:match[3]])
}

func splitGuests(list string) []string {
	guests := []string{}
	for _, part := range guestSeparatorPattern.Split(list, -1) {
		guest := collapseWhitespace(part)
		if guest == "" {
			continue
		}
		guests = append(guests, guest)
	}
	return guests
}

type ArtistCredit struct {
	artists []string
}

func NewArtistCredit(artists []string) (ArtistCredit, error) {
	unique := appendUniqueArtists(nil, artists)
	if len(unique) == 0 {
		return ArtistCredit{}, ErrArtistMissing
	}
	return ArtistCredit{artists: unique}, nil
}

func ParseArtistCredit(tag string) (ArtistCredit, error) {
	return NewArtistCredit(strings.Split(tag, artistTagSeparator))
}

func (credit ArtistCredit) MainArtist() string {
	return credit.artists[0]
}

func (credit ArtistCredit) GuestArtists() []string {
	return slices.Clone(credit.artists[1:])
}

func (credit ArtistCredit) Artists() []string {
	return slices.Clone(credit.artists)
}

func (credit ArtistCredit) String() string {
	return strings.Join(credit.artists, artistJoinSeparator)
}

func (credit ArtistCredit) HasGuests() bool {
	return len(credit.artists) > 1
}

func (credit ArtistCredit) WithGuests(guests []string) ArtistCredit {
	return ArtistCredit{artists: appendUniqueArtists(slices.Clone(credit.artists), guests)}
}

type CreditSource struct {
	Path        string
	Title       string
	Artist      string
	AlbumArtist string
}

type Credit struct {
	title   Title
	artists ArtistCredit
}

func NewCredit(title Title, artists ArtistCredit) Credit {
	return Credit{title: title, artists: artists}
}

func (credit Credit) Title() Title {
	return credit.title
}

func (credit Credit) Artists() ArtistCredit {
	return credit.artists
}

func CreditFromRules(source CreditSource) (Credit, error) {
	artists, err := mainArtistsFromSource(source)
	if err != nil {
		return Credit{}, err
	}
	title, err := NewTitle(source.Title)
	if err != nil {
		return Credit{}, err
	}
	cleaned, guests := title.WithoutFeaturing()
	return NewCredit(cleaned, artists.WithGuests(guests)), nil
}

func (credit Credit) IsConsistentWith(source CreditSource) bool {
	expected, err := CreditFromRules(source)
	if err != nil {
		return false
	}
	if foldForComparison(expected.title.String()) != foldForComparison(credit.title.String()) {
		return false
	}
	return areArtistsTraceable(credit.artists.artists, source)
}

func mainArtistsFromSource(source CreditSource) (ArtistCredit, error) {
	if artists, err := ParseArtistCredit(source.Artist); err == nil {
		return artists, nil
	}
	if artists, err := ParseArtistCredit(source.AlbumArtist); err == nil {
		return artists, nil
	}
	return NewArtistCredit([]string{artistFromPath(source.Path)})
}

func artistFromPath(path string) string {
	segments := strings.Split(strings.ReplaceAll(path, windowsPathSeparator, slashPathSeparator), slashPathSeparator)
	for _, segment := range []string{parentSegment(segments), segments[len(segments)-1]} {
		artist, _, found := strings.Cut(segment, pathArtistSeparator)
		if found {
			return artist
		}
	}
	return ""
}

func parentSegment(segments []string) string {
	if len(segments) < 2 {
		return ""
	}
	return segments[len(segments)-2]
}

func areArtistsTraceable(artists []string, source CreditSource) bool {
	haystack := foldForComparison(strings.Join([]string{source.Title, source.Artist, source.AlbumArtist, source.Path}, " "))
	for _, artist := range artists {
		if !strings.Contains(haystack, foldForComparison(artist)) {
			return false
		}
	}
	return true
}

func foldForComparison(value string) string {
	return strings.ToLower(collapseWhitespace(value))
}

func appendUniqueArtists(existing, candidates []string) []string {
	result := slices.Clone(existing)
	for _, candidate := range candidates {
		artist := collapseWhitespace(candidate)
		if artist == "" || containsFolded(result, artist) {
			continue
		}
		result = append(result, artist)
	}
	return result
}

func containsFolded(values []string, candidate string) bool {
	return slices.ContainsFunc(values, func(value string) bool { return strings.EqualFold(value, candidate) })
}

func collapseWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
