package domain

import "strings"

const libraryPathSeparator = "/"

type Library struct {
	existing map[string]string
}

func NewLibrary(existingRelativePaths []string, root string) Library {
	existing := make(map[string]string, len(existingRelativePaths))
	for _, relative := range existingRelativePaths {
		existing[duplicateKeyOf(relative)] = strings.TrimRight(root, "/\\") + libraryPathSeparator + relative
	}
	return Library{existing: existing}
}

func (library Library) Admit(tracks []*Track) error {
	if err := library.markExisting(tracks); err != nil {
		return err
	}
	keepers := map[string]*Track{}
	for _, track := range tracks {
		if !track.CanPublish() {
			continue
		}
		key := track.Target().DuplicateKey()
		keepers[key] = betterCandidate(keepers[key], track)
	}
	return markLosers(tracks, keepers)
}

func (library Library) markExisting(tracks []*Track) error {
	for _, track := range tracks {
		existingPath, isInLibrary := library.existing[track.Target().DuplicateKey()]
		if !track.CanPublish() || !isInLibrary {
			continue
		}
		if err := track.MarkDuplicate(existingPath); err != nil {
			return err
		}
	}
	return nil
}

func markLosers(tracks []*Track, keepers map[string]*Track) error {
	for _, track := range tracks {
		if !track.CanPublish() {
			continue
		}
		keeper := keepers[track.Target().DuplicateKey()]
		if keeper == track {
			continue
		}
		if err := track.MarkDuplicate(keeper.Source().Path); err != nil {
			return err
		}
	}
	return nil
}

func betterCandidate(current, candidate *Track) *Track {
	if current == nil || candidate.CompareSourceQuality(*current) > 0 {
		return candidate
	}
	return current
}
