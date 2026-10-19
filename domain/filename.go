package domain

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf16"
)

var ErrTemplatePlaceholderUnknown = newBusinessError("album template has an unknown placeholder")

const (
	MaxFileNameUnits     = 180
	FileExtension        = ".m4a"
	GuestSeparator       = "; "
	artistTitleSeparator = " - "
	featuringPrefix      = " (feat. "
	featuringSuffix      = ")"
	reservedNameSuffix   = "_"
	unknownPathValue     = "Unknown"
	yearLength           = 4
	relativeSeparator    = "/"
	forbiddenPathRunes   = "\\/:*?\"<>|"
	trimmedTrailingRunes = " ."
)

var reservedNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true, "lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

var templatePlaceholderPattern = regexp.MustCompile(`\{[^{}]*\}`)

func SanitizePathComponent(raw string, maxUnits int) string {
	cleaned := collapseWhitespace(strings.Map(dropForbiddenRune, raw))
	cleaned = protectReservedName(trimPathEnd(cleaned))
	return trimPathEnd(truncateUnits(cleaned, maxUnits))
}

type FileName struct {
	value string
}

func NewFileName(credit Credit) FileName {
	artist := SanitizePathComponent(credit.Artists().MainArtist(), MaxFileNameUnits)
	title := SanitizePathComponent(credit.Title().String(), MaxFileNameUnits)
	available := MaxFileNameUnits - utf16Units(FileExtension)
	withFeaturing := SanitizePathComponent(artist+artistTitleSeparator+title+featuringPart(credit.Artists()), MaxFileNameUnits)
	if utf16Units(withFeaturing) <= available {
		return FileName{value: withFeaturing + FileExtension}
	}
	prefix := artist + artistTitleSeparator
	titleRoom := max(available-utf16Units(prefix), 0)
	stem := SanitizePathComponent(prefix+truncateUnits(title, titleRoom), available)
	return FileName{value: stem + FileExtension}
}

func (name FileName) String() string {
	return name.value
}

type Layout int

const (
	LayoutFlat Layout = iota
	LayoutAlbumFolders
)

type TargetPathDetails struct {
	Layout      Layout
	Template    string
	AlbumArtist string
	Album       string
	Date        string
	FileName    FileName
}

type TargetPath struct {
	relative string
}

func NewTargetPath(details TargetPathDetails) (TargetPath, error) {
	if details.Layout == LayoutFlat {
		return TargetPath{relative: details.FileName.String()}, nil
	}
	folder, err := expandAlbumTemplate(details)
	if err != nil {
		return TargetPath{}, err
	}
	return TargetPath{relative: folder + relativeSeparator + details.FileName.String()}, nil
}

func (path TargetPath) Relative() string {
	return path.relative
}

func (path TargetPath) DuplicateKey() string {
	return duplicateKeyOf(path.relative)
}

func duplicateKeyOf(relative string) string {
	return strings.ToLower(collapseWhitespace(relative))
}

func expandAlbumTemplate(details TargetPathDetails) (string, error) {
	values := map[string]string{
		"{albumartist}": orUnknown(details.AlbumArtist),
		"{album}":       orUnknown(details.Album),
		"{year}":        orUnknown(yearOf(details.Date)),
	}
	for _, placeholder := range templatePlaceholderPattern.FindAllString(details.Template, -1) {
		if _, isKnown := values[placeholder]; !isKnown {
			return "", ErrTemplatePlaceholderUnknown
		}
	}
	expanded := templatePlaceholderPattern.ReplaceAllStringFunc(details.Template, func(placeholder string) string { return values[placeholder] })
	return orUnknown(SanitizePathComponent(expanded, MaxFileNameUnits)), nil
}

func yearOf(date string) string {
	trimmed := strings.TrimSpace(date)
	if len(trimmed) < yearLength {
		return trimmed
	}
	return trimmed[:yearLength]
}

func orUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return unknownPathValue
	}
	return value
}

func featuringPart(artists ArtistCredit) string {
	if !artists.HasGuests() {
		return ""
	}
	return featuringPrefix + strings.Join(artists.GuestArtists(), GuestSeparator) + featuringSuffix
}

func dropForbiddenRune(character rune) rune {
	if unicode.IsControl(character) || strings.ContainsRune(forbiddenPathRunes, character) {
		return -1
	}
	return character
}

func trimPathEnd(value string) string {
	return strings.TrimLeft(strings.TrimRight(value, trimmedTrailingRunes), " ")
}

func protectReservedName(value string) string {
	base, extension, hasExtension := strings.Cut(value, ".")
	if !reservedNames[strings.ToLower(base)] {
		return value
	}
	if !hasExtension {
		return base + reservedNameSuffix
	}
	return base + reservedNameSuffix + "." + extension
}

func truncateUnits(value string, maxUnits int) string {
	runes := []rune(value)
	cut := fittingRuneCount(runes, maxUnits)
	for cut > 0 && cut < len(runes) && unicode.Is(unicode.Mn, runes[cut]) {
		cut--
	}
	return string(runes[:cut])
}

func fittingRuneCount(runes []rune, maxUnits int) int {
	units := 0
	for index, character := range runes {
		units += utf16.RuneLen(character)
		if units > maxUnits {
			return index
		}
	}
	return len(runes)
}

func utf16Units(value string) int {
	units := 0
	for _, character := range value {
		units += utf16.RuneLen(character)
	}
	return units
}
