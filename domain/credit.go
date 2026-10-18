package domain

import (
	"regexp"
	"strings"
)

var ErrTitleMissing = newBusinessError("title is missing")

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
	guests := []string{}
	for _, match := range bracketedFeaturingPattern.FindAllStringSubmatch(value, -1) {
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

func collapseWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
