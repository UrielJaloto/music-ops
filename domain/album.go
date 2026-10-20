package domain

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

const albumPathSeparators = "/\\"

type NumberingMode int

const (
	NumberingPerDirectory NumberingMode = iota
	NumberingKeepSource
)

type AlbumGroup struct {
	directory string
	tracks    []*Track
}

func GroupByDirectory(tracks []*Track) []AlbumGroup {
	groups := []AlbumGroup{}
	indexes := map[string]int{}
	for _, track := range tracks {
		directory := track.Source().Directory
		index, found := indexes[directory]
		if !found {
			index = len(groups)
			indexes[directory] = index
			groups = append(groups, AlbumGroup{directory: directory})
		}
		groups[index].tracks = append(groups[index].tracks, track)
	}
	return groups
}

func (group AlbumGroup) Directory() string {
	return group.directory
}

func (group AlbumGroup) Tracks() []*Track {
	return slices.Clone(group.tracks)
}

func (group AlbumGroup) Number(mode NumberingMode) error {
	if mode == NumberingKeepSource {
		return group.keepSourceNumbers()
	}
	ordered := group.orderedTracks()
	for index, track := range ordered {
		if err := track.AssignPosition(index+1, len(ordered)); err != nil {
			return err
		}
	}
	return nil
}

func (group AlbumGroup) keepSourceNumbers() error {
	for _, track := range group.tracks {
		position := track.Source().Tags.Position
		total := position.TrackTotal
		if total == 0 {
			total = len(group.tracks)
		}
		if err := track.AssignPosition(position.TrackNumber, total); err != nil {
			return err
		}
	}
	return nil
}

func (group AlbumGroup) orderedTracks() []*Track {
	ordered := slices.Clone(group.tracks)
	if hasUniqueSourceNumbers(ordered) {
		slices.SortStableFunc(ordered, compareBySourcePosition)
		return ordered
	}
	slices.SortStableFunc(ordered, func(left, right *Track) int {
		return compareNatural(sourceFileName(left), sourceFileName(right))
	})
	return ordered
}

func hasUniqueSourceNumbers(tracks []*Track) bool {
	seen := map[[2]int]bool{}
	for _, track := range tracks {
		position := track.Source().Tags.Position
		key := [2]int{position.DiscNumber, position.TrackNumber}
		if position.TrackNumber <= 0 || seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

func compareBySourcePosition(left, right *Track) int {
	leftPosition := left.Source().Tags.Position
	rightPosition := right.Source().Tags.Position
	if byDisc := cmp.Compare(leftPosition.DiscNumber, rightPosition.DiscNumber); byDisc != 0 {
		return byDisc
	}
	return cmp.Compare(leftPosition.TrackNumber, rightPosition.TrackNumber)
}

func sourceFileName(track *Track) string {
	path := track.Source().Path
	return path[strings.LastIndexAny(path, albumPathSeparators)+1:]
}

func compareNatural(left, right string) int {
	leftChunks := naturalChunks(strings.ToLower(left))
	rightChunks := naturalChunks(strings.ToLower(right))
	for index := 0; index < min(len(leftChunks), len(rightChunks)); index++ {
		if result := compareChunk(leftChunks[index], rightChunks[index]); result != 0 {
			return result
		}
	}
	return cmp.Compare(len(leftChunks), len(rightChunks))
}

func compareChunk(left, right string) int {
	leftNumber, leftError := strconv.Atoi(left)
	rightNumber, rightError := strconv.Atoi(right)
	if leftError == nil && rightError == nil && leftNumber != rightNumber {
		return cmp.Compare(leftNumber, rightNumber)
	}
	return strings.Compare(left, right)
}

func naturalChunks(value string) []string {
	chunks := []string{}
	var current strings.Builder
	isDigitChunk := false
	for index, character := range value {
		isDigit := unicode.IsDigit(character)
		if index > 0 && isDigit != isDigitChunk {
			chunks = append(chunks, current.String())
			current.Reset()
		}
		isDigitChunk = isDigit
		current.WriteRune(character)
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}
