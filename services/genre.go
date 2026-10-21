package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/UrielJaloto/music-ops/domain"
)

type GenreTagSource interface {
	TrackTags(ctx context.Context, artist, title string) ([]string, error)
	ArtistTags(ctx context.Context, artist string) ([]string, error)
}

type GenreRequest struct {
	Key     string
	Path    string
	Title   string
	Artists []string
	Tags    []string
}

type GenreSuggestion struct {
	Key         string
	Genre       string
	Subgenre    string
	IsBrazilian bool
	IsKnown     bool
}

type GenreAdvisor interface {
	SuggestGenres(ctx context.Context, requests []GenreRequest) ([]GenreSuggestion, error)
}

type GenreWorkPool interface {
	Run(ctx context.Context, size int, task func(ctx context.Context, index int) error) error
}

type GenreTrack struct {
	ID     domain.TrackID
	Path   string
	Credit domain.Credit
}

type ClassifyGenresInput struct {
	Tracks []GenreTrack
}

type ClassifiedGenres struct {
	Classifications map[domain.TrackID]domain.Classification
	Warnings        []string
}

type GenreService struct {
	tags      GenreTagSource
	advisor   GenreAdvisor
	pool      GenreWorkPool
	batchSize int
}

func NewGenreService(tags GenreTagSource, advisor GenreAdvisor, pool GenreWorkPool, batchSize int) GenreService {
	return GenreService{tags: tags, advisor: advisor, pool: pool, batchSize: batchSize}
}

func (service GenreService) Classify(ctx context.Context, input ClassifyGenresInput) (ClassifiedGenres, error) {
	if !isValidBatchSize(service.batchSize) {
		return ClassifiedGenres{}, ErrBatchSizeOutOfRange
	}
	evidence, warnings, err := service.collectEvidence(ctx, input.Tracks)
	if err != nil {
		return ClassifiedGenres{}, err
	}
	classifications, adviceWarnings, err := service.adviseGenres(ctx, input.Tracks, evidence)
	if err != nil {
		return ClassifiedGenres{}, err
	}
	return ClassifiedGenres{
		Classifications: resolveTrackNationality(input.Tracks, classifications, evidence),
		Warnings:        append(warnings, adviceWarnings...),
	}, nil
}

type tagLookup struct {
	tags    []string
	warning string
}

func (service GenreService) collectEvidence(ctx context.Context, tracks []GenreTrack) ([]domain.TagEvidence, []string, error) {
	pairs := uniqueKeys(tracks, trackTagKey)
	trackLookups, err := service.lookupTags(ctx, pairs, func(ctx context.Context, key tagKey) ([]string, error) {
		return service.tags.TrackTags(ctx, key.artist, key.title)
	})
	if err != nil {
		return nil, nil, err
	}
	trackTags := lookupsByKey(pairs, trackLookups)
	missingArtists := artistsWithoutTrackTags(tracks, trackTags)
	artistLookups, err := service.lookupTags(ctx, missingArtists, func(ctx context.Context, key tagKey) ([]string, error) {
		return service.tags.ArtistTags(ctx, key.artist)
	})
	if err != nil {
		return nil, nil, err
	}
	artistTags := lookupsByKey(missingArtists, artistLookups)
	evidence := make([]domain.TagEvidence, 0, len(tracks))
	for _, track := range tracks {
		evidence = append(evidence, domain.NewTagEvidence(chooseTags(track, trackTags, artistTags)))
	}
	return evidence, append(lookupWarnings(trackLookups), lookupWarnings(artistLookups)...), nil
}

type tagKey struct {
	artist string
	title  string
}

func trackTagKey(track GenreTrack) tagKey {
	return tagKey{artist: track.Credit.Artists().MainArtist(), title: track.Credit.Title().String()}
}

func artistTagKey(track GenreTrack) tagKey {
	return tagKey{artist: track.Credit.Artists().MainArtist()}
}

func foldedTagKey(key tagKey) tagKey {
	return tagKey{artist: strings.ToLower(key.artist), title: strings.ToLower(key.title)}
}

func uniqueKeys(tracks []GenreTrack, keyOf func(GenreTrack) tagKey) []tagKey {
	seen := map[tagKey]bool{}
	keys := []tagKey{}
	for _, track := range tracks {
		key := keyOf(track)
		if seen[foldedTagKey(key)] {
			continue
		}
		seen[foldedTagKey(key)] = true
		keys = append(keys, key)
	}
	return keys
}

func (service GenreService) lookupTags(ctx context.Context, keys []tagKey, fetch func(context.Context, tagKey) ([]string, error)) ([]tagLookup, error) {
	lookups := make([]tagLookup, len(keys))
	err := service.pool.Run(ctx, len(keys), func(ctx context.Context, index int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		tags, err := fetch(ctx, keys[index])
		if err != nil {
			lookups[index] = tagLookup{tags: []string{}, warning: fmt.Sprintf("genre tags unavailable for %s %s: %v", keys[index].artist, keys[index].title, err)}
			return nil
		}
		lookups[index] = tagLookup{tags: tags}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("fetching genre tags: %w", err)
	}
	return lookups, nil
}

func lookupsByKey(keys []tagKey, lookups []tagLookup) map[tagKey][]string {
	byKey := make(map[tagKey][]string, len(keys))
	for index, key := range keys {
		byKey[foldedTagKey(key)] = lookups[index].tags
	}
	return byKey
}

func artistsWithoutTrackTags(tracks []GenreTrack, found map[tagKey][]string) []tagKey {
	var missing []GenreTrack
	for _, track := range tracks {
		if len(found[foldedTagKey(trackTagKey(track))]) == 0 {
			missing = append(missing, track)
		}
	}
	return uniqueKeys(missing, artistTagKey)
}

func chooseTags(track GenreTrack, trackTags, artistTags map[tagKey][]string) []string {
	if tags := trackTags[foldedTagKey(trackTagKey(track))]; len(tags) > 0 {
		return tags
	}
	return artistTags[foldedTagKey(artistTagKey(track))]
}

func lookupWarnings(lookups []tagLookup) []string {
	warnings := []string{}
	for _, lookup := range lookups {
		if lookup.warning != "" {
			warnings = append(warnings, lookup.warning)
		}
	}
	return warnings
}

func (service GenreService) adviseGenres(ctx context.Context, tracks []GenreTrack, evidence []domain.TagEvidence) ([]domain.Classification, []string, error) {
	classifications := make([]domain.Classification, 0, len(tracks))
	warnings := []string{}
	for start := 0; start < len(tracks); start += service.batchSize {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		end := min(start+service.batchSize, len(tracks))
		suggestions, batchErr := service.advisor.SuggestGenres(ctx, toGenreRequests(tracks[start:end], evidence[start:end]))
		for index := start; index < end; index++ {
			classification, warning := chooseClassification(tracks[index], evidence[index], suggestions, batchErr)
			classifications = append(classifications, classification)
			warnings = appendWarning(warnings, warning)
		}
	}
	return classifications, warnings, nil
}

func toGenreRequests(tracks []GenreTrack, evidence []domain.TagEvidence) []GenreRequest {
	requests := make([]GenreRequest, 0, len(tracks))
	for index, track := range tracks {
		requests = append(requests, GenreRequest{
			Key:     string(track.ID),
			Path:    track.Path,
			Title:   track.Credit.Title().String(),
			Artists: track.Credit.Artists().Artists(),
			Tags:    evidence[index].Tags(),
		})
	}
	return requests
}

func chooseClassification(track GenreTrack, evidence domain.TagEvidence, suggestions []GenreSuggestion, batchErr error) (domain.Classification, string) {
	classification, err := suggestedClassification(track, suggestions, batchErr)
	if err == nil {
		return classification, ""
	}
	return domain.ClassifyByTags(evidence), fmt.Sprintf("genre tag rules used for %s: %v", track.Path, err)
}

func suggestedClassification(track GenreTrack, suggestions []GenreSuggestion, batchErr error) (domain.Classification, error) {
	if batchErr != nil {
		return domain.Classification{}, batchErr
	}
	for _, suggestion := range suggestions {
		if suggestion.Key != string(track.ID) {
			continue
		}
		return domain.NewClassification(domain.ClassificationDetails{
			Genre: suggestion.Genre, Subgenre: suggestion.Subgenre, IsBrazilian: suggestion.IsBrazilian, IsKnown: suggestion.IsKnown,
		})
	}
	return domain.Classification{}, errSuggestionMissing
}

func appendWarning(warnings []string, warning string) []string {
	if warning == "" {
		return warnings
	}
	return append(warnings, warning)
}

func resolveTrackNationality(tracks []GenreTrack, classifications []domain.Classification, evidence []domain.TagEvidence) map[domain.TrackID]domain.Classification {
	ballots := make([]domain.NationalityBallot, 0, len(tracks))
	for index, track := range tracks {
		ballots = append(ballots, domain.NationalityBallot{MainArtist: track.Credit.Artists().MainArtist(), Classification: classifications[index], Evidence: evidence[index]})
	}
	resolved := domain.ResolveNationality(ballots)
	byID := make(map[domain.TrackID]domain.Classification, len(tracks))
	for index, track := range tracks {
		byID[track.ID] = resolved[index]
	}
	return byID
}
