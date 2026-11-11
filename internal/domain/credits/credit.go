package credits

import (
	"errors"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

var (
	ErrTitleMissing  = errors.New("title is missing")
	ErrArtistMissing = errors.New("artist is missing")
)

const (
	artistTagSeparator  = ";"
	artistJoinSeparator = "; "
	pathArtistSeparator = " - "
)

var (
	bracketedFeaturingPattern = regexp.MustCompile(`(?i)[(\[]\s*(?:feat\.?|ft\.?|featuring|with)\s+([^)\]]+)[)\]]`)
	trailingFeaturingPattern  = regexp.MustCompile(`(?i)\s+-?\s*(?:feat\.?|ft\.?|featuring)\s+(.+)$`)
	guestSeparatorPattern     = regexp.MustCompile(`(?i)\s*[,&]\s*|\s+(?:and|e|x)\s+`)
	leadingFeaturingPattern   = regexp.MustCompile(`(?i)^(?:(?:feat\.?|ft\.?|featuring)(?:\s+|$))+`)
	emptyBracketsPattern      = regexp.MustCompile(`\(\s*\)|\[\s*\]`)
	placeholderArtists        = []string{"various artists", "various", "va", "v.a.", "varios artistas", "vários artistas", "diversos artistas", "artistas variados"}
)

type Title struct {
	value string
}

func NewTitle(raw string) (Title, error) {
	collapsed := CollapseWhitespace(raw)
	if collapsed == "" {
		return Title{}, ErrTitleMissing
	}
	return Title{value: collapsed}, nil
}

func (title Title) String() string {
	return title.value
}

func (title Title) RemoveFeaturing() (Title, []string) {
	remaining, guests := removeBracketedFeaturing(title.value)
	remaining, trailingGuests := removeTrailingFeaturing(remaining)
	cleaned := CollapseWhitespace(emptyBracketsPattern.ReplaceAllString(remaining, " "))
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
		guest := CollapseWhitespace(leadingFeaturingPattern.ReplaceAllString(strings.TrimSpace(part), ""))
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
	if named := slices.DeleteFunc(slices.Clone(unique), isPlaceholderArtist); len(named) > 0 {
		unique = named
	}
	return ArtistCredit{artists: unique}, nil
}

func isPlaceholderArtist(artist string) bool {
	return slices.Contains(placeholderArtists, strings.ToLower(artist))
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

func (credit ArtistCredit) AddGuests(guests []string) ArtistCredit {
	return ArtistCredit{artists: appendUniqueArtists(slices.Clone(credit.artists), guests)}
}

type CreditSource struct {
	Path        string
	Title       string
	Artist      string
	AlbumArtist string
}

func (source CreditSource) FillTitleFromPath() CreditSource {
	if CollapseWhitespace(source.Title) != "" {
		return source
	}
	stem := strings.TrimSuffix(filepath.Base(source.Path), filepath.Ext(source.Path))
	left, right, found := strings.Cut(stem, pathArtistSeparator)
	artist, suffix, isFolderArtist := cutFolderName(right, filepath.Base(filepath.Dir(source.Path)))
	switch {
	case !found:
		source.Title = stem
	case isFolderArtist:
		source.Title = CollapseWhitespace(left + " " + suffix)
		source.Artist = source.resolveArtist(artist)
	default:
		source.Title = right
	}
	return source
}

func cutFolderName(value, folder string) (string, string, bool) {
	expected := []rune(strings.ToLower(strings.Join(strings.Fields(folder), "")))
	matched := 0
	for index, character := range value {
		if matched == len(expected) {
			return strings.TrimSpace(value[:index]), strings.TrimSpace(value[index:]), isSuffixSeparated(value[index:])
		}
		if !unicode.IsSpace(character) && unicode.ToLower(character) != expected[matched] {
			return "", "", false
		}
		if !unicode.IsSpace(character) {
			matched++
		}
	}
	return strings.TrimSpace(value), "", len(expected) > 0 && matched == len(expected)
}

func isSuffixSeparated(rest string) bool {
	trimmed := strings.TrimLeft(rest, " ")
	return strings.HasPrefix(trimmed, "(") || strings.HasPrefix(trimmed, "[")
}

func (source CreditSource) resolveArtist(fallback string) string {
	if CollapseWhitespace(source.Artist+source.AlbumArtist) == "" {
		return fallback
	}
	return source.Artist
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

func (source CreditSource) BuildCreditFromRules() (Credit, error) {
	artists, err := source.resolveMainArtists()
	if err != nil {
		return Credit{}, err
	}
	title, err := NewTitle(source.Title)
	if err != nil {
		return Credit{}, err
	}
	cleaned, guests := title.RemoveFeaturing()
	return NewCredit(cleaned, artists.AddGuests(guests)), nil
}

func (credit Credit) IsConsistentWith(source CreditSource) bool {
	expected, err := source.BuildCreditFromRules()
	if err != nil {
		return false
	}
	if foldForComparison(expected.title.String()) != foldForComparison(credit.title.String()) {
		return false
	}
	return source.containsArtists(credit.artists.artists)
}

func (source CreditSource) resolveMainArtists() (ArtistCredit, error) {
	if artists, err := ParseArtistCredit(source.Artist); err == nil {
		return artists, nil
	}
	if artists, err := ParseArtistCredit(source.AlbumArtist); err == nil {
		return artists, nil
	}
	return NewArtistCredit([]string{extractArtistFromPath(source.Path)})
}

func extractArtistFromPath(path string) string {
	for _, segment := range []string{filepath.Base(filepath.Dir(path)), filepath.Base(path)} {
		artist, _, found := strings.Cut(segment, pathArtistSeparator)
		if found {
			return artist
		}
	}
	return ""
}

func (source CreditSource) containsArtists(artists []string) bool {
	haystack := foldForComparison(strings.Join([]string{source.Title, source.Artist, source.AlbumArtist, source.Path}, " "))
	for _, artist := range artists {
		if !strings.Contains(haystack, foldForComparison(artist)) {
			return false
		}
	}
	return true
}

func foldForComparison(value string) string {
	return strings.ToLower(CollapseWhitespace(value))
}

func appendUniqueArtists(existing, candidates []string) []string {
	result := slices.Clone(existing)
	for _, candidate := range candidates {
		artist := CollapseWhitespace(candidate)
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

func CollapseWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
