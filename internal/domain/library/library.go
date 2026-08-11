package library

import (
	"errors"
	"strings"

	"github.com/UrielJaloto/music-ops/internal/domain/filename"
	"github.com/UrielJaloto/music-ops/internal/domain/tracks"
)

const libraryPathSeparator = "/"

type Library struct {
	existing map[string]string
}

func NewLibrary(existingRelativePaths []string, root string) Library {
	existing := make(map[string]string, len(existingRelativePaths))
	for _, relative := range existingRelativePaths {
		existing[filename.BuildDuplicateKey(relative)] = strings.TrimRight(root, "/\\") + libraryPathSeparator + relative
	}
	return Library{existing: existing}
}

func (library Library) Admit(candidates []*tracks.Track) error {
	existingErr := library.markExisting(candidates)
	keepers := map[string]*tracks.Track{}
	for _, track := range candidates {
		if !track.CanPublish() {
			continue
		}
		key := track.Target().DuplicateKey()
		keepers[key] = pickBetterCandidate(keepers[key], track)
	}
	return errors.Join(existingErr, markLosers(candidates, keepers))
}

func (library Library) markExisting(tracks []*tracks.Track) error {
	var err error
	for _, track := range tracks {
		existingPath, isInLibrary := library.existing[track.Target().DuplicateKey()]
		if track.CanPublish() && isInLibrary {
			err = errors.Join(err, track.MarkDuplicate(existingPath))
		}
	}
	return err
}

func markLosers(tracks []*tracks.Track, keepers map[string]*tracks.Track) error {
	var err error
	for _, track := range tracks {
		keeper := keepers[track.Target().DuplicateKey()]
		if track.CanPublish() && keeper != track {
			err = errors.Join(err, track.MarkDuplicate(keeper.Source().Path))
		}
	}
	return err
}

func pickBetterCandidate(current, candidate *tracks.Track) *tracks.Track {
	if current == nil || candidate.CompareSourceQuality(*current) > 0 {
		return candidate
	}
	return current
}
