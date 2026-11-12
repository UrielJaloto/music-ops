package tracks

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/UrielJaloto/music-ops/internal/domain/genres"
)

var (
	ErrTagUnknown      = errors.New("tag is not supported")
	ErrTagValueMissing = errors.New("tag value is missing")
	ErrYearInvalid     = errors.New("year must have four digits")
)

const (
	TagAlbum = "album"
	TagGenre = "genre"
	TagYear  = "year"
)

var yearPattern = regexp.MustCompile(`^\d{4}$`)

type TagOverrides struct {
	Album string
	Genre string
	Year  string
}

func NewTagOverrides(values map[string]string) (TagOverrides, error) {
	overrides := TagOverrides{}
	fields := map[string]*string{TagAlbum: &overrides.Album, TagGenre: &overrides.Genre, TagYear: &overrides.Year}
	for key, value := range values {
		field, found := fields[strings.ToLower(strings.TrimSpace(key))]
		if !found {
			return TagOverrides{}, fmt.Errorf("%w: %q", ErrTagUnknown, key)
		}
		*field = strings.Join(strings.Fields(value), " ")
		if *field == "" {
			return TagOverrides{}, fmt.Errorf("%w: %q", ErrTagValueMissing, key)
		}
	}
	if overrides.Year != "" && !yearPattern.MatchString(overrides.Year) {
		return TagOverrides{}, fmt.Errorf("%w: %q", ErrYearInvalid, overrides.Year)
	}
	return overrides, nil
}

func (overrides TagOverrides) CanonicalizeGenre(pool genres.Pool) (TagOverrides, error) {
	if overrides.Genre == "" {
		return overrides, nil
	}
	classification, err := pool.ParseGenre(overrides.Genre)
	if err != nil {
		return TagOverrides{}, err
	}
	overrides.Genre = classification.Genre().String()
	return overrides, nil
}

func (overrides TagOverrides) Apply(tags SourceTags) SourceTags {
	if overrides.Album != "" {
		tags.Album = overrides.Album
	}
	if overrides.Year != "" {
		tags.Date = overrides.Year
	}
	return tags
}
