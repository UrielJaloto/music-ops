package classification

import (
	"context"
	"fmt"
	"strings"

	"github.com/UrielJaloto/music-ops/internal/domain/credits"
	"github.com/UrielJaloto/music-ops/internal/domain/genres"
	"github.com/UrielJaloto/music-ops/internal/domain/tracks"
	"github.com/UrielJaloto/music-ops/internal/services/crediting"
)

type GenreTagSource interface {
	FetchTrackTags(ctx context.Context, artist, title string) ([]string, error)
	FetchArtistTags(ctx context.Context, artist string) ([]string, error)
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
	ID     tracks.TrackID
	Path   string
	Credit credits.Credit
}

type ClassifyGenresInput struct {
	Tracks []GenreTrack
}

type ClassifiedGenres struct {
	Classifications map[tracks.TrackID]genres.Classification
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
	if !crediting.IsValidBatchSize(service.batchSize) {
		return ClassifiedGenres{}, crediting.ErrBatchSizeOutOfRange
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

func (service GenreService) collectEvidence(ctx context.Context, tracks []GenreTrack) ([]genres.TagEvidence, []string, error) {
	pairs := collectUniqueKeys(tracks, buildTrackTagKey)
	trackLookups, err := service.lookupTags(ctx, pairs, func(ctx context.Context, key tagKey) ([]string, error) {
		return service.tags.FetchTrackTags(ctx, key.artist, key.title)
	})
	if err != nil {
		return nil, nil, err
	}
	trackTags := indexLookupsByKey(pairs, trackLookups)
	missingArtists := findArtistsWithoutTrackTags(tracks, trackTags)
	artistLookups, err := service.lookupTags(ctx, missingArtists, func(ctx context.Context, key tagKey) ([]string, error) {
		return service.tags.FetchArtistTags(ctx, key.artist)
	})
	if err != nil {
		return nil, nil, err
	}
	artistTags := indexLookupsByKey(missingArtists, artistLookups)
	evidence := make([]genres.TagEvidence, 0, len(tracks))
	for _, track := range tracks {
		evidence = append(evidence, genres.NewTagEvidence(chooseTags(track, trackTags, artistTags)))
	}
	return evidence, append(collectLookupWarnings(trackLookups), collectLookupWarnings(artistLookups)...), nil
}

type tagKey struct {
	artist string
	title  string
}

func buildTrackTagKey(track GenreTrack) tagKey {
	return tagKey{artist: track.Credit.Artists().MainArtist(), title: track.Credit.Title().String()}
}

func buildArtistTagKey(track GenreTrack) tagKey {
	return tagKey{artist: track.Credit.Artists().MainArtist()}
}

func (key tagKey) foldCase() tagKey {
	return tagKey{artist: strings.ToLower(key.artist), title: strings.ToLower(key.title)}
}

func collectUniqueKeys(tracks []GenreTrack, keyOf func(GenreTrack) tagKey) []tagKey {
	seen := map[tagKey]bool{}
	keys := []tagKey{}
	for _, track := range tracks {
		key := keyOf(track)
		if seen[key.foldCase()] {
			continue
		}
		seen[key.foldCase()] = true
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

func indexLookupsByKey(keys []tagKey, lookups []tagLookup) map[tagKey][]string {
	byKey := make(map[tagKey][]string, len(keys))
	for index, key := range keys {
		byKey[key.foldCase()] = lookups[index].tags
	}
	return byKey
}

func findArtistsWithoutTrackTags(tracks []GenreTrack, found map[tagKey][]string) []tagKey {
	var missing []GenreTrack
	for _, track := range tracks {
		if len(found[(buildTrackTagKey(track)).foldCase()]) == 0 {
			missing = append(missing, track)
		}
	}
	return collectUniqueKeys(missing, buildArtistTagKey)
}

func chooseTags(track GenreTrack, trackTags, artistTags map[tagKey][]string) []string {
	if tags := trackTags[(buildTrackTagKey(track)).foldCase()]; len(tags) > 0 {
		return tags
	}
	return artistTags[(buildArtistTagKey(track)).foldCase()]
}

func collectLookupWarnings(lookups []tagLookup) []string {
	warnings := []string{}
	for _, lookup := range lookups {
		if lookup.warning != "" {
			warnings = append(warnings, lookup.warning)
		}
	}
	return warnings
}

func (service GenreService) adviseGenres(ctx context.Context, tracks []GenreTrack, evidence []genres.TagEvidence) ([]genres.Classification, []string, error) {
	classifications := make([]genres.Classification, 0, len(tracks))
	warnings := []string{}
	for start := 0; start < len(tracks); start += service.batchSize {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		end := min(start+service.batchSize, len(tracks))
		chosen, batchWarnings := service.classifyBatch(ctx, tracks[start:end], evidence[start:end])
		classifications = append(classifications, chosen...)
		warnings = append(warnings, batchWarnings...)
	}
	return classifications, warnings, nil
}

func (service GenreService) classifyBatch(ctx context.Context, tracks []GenreTrack, evidence []genres.TagEvidence) ([]genres.Classification, []string) {
	suggestions, batchErr := service.advisor.SuggestGenres(ctx, toGenreRequests(tracks, evidence))
	batch := genreBatch{suggestions: suggestions, err: batchErr}
	classifications := make([]genres.Classification, 0, len(tracks))
	warnings := []string{}
	for index, track := range tracks {
		classification, warning := batch.chooseClassification(track, evidence[index])
		classifications = append(classifications, classification)
		warnings = appendWarning(warnings, warning)
	}
	return classifications, warnings
}

func toGenreRequests(tracks []GenreTrack, evidence []genres.TagEvidence) []GenreRequest {
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

type genreBatch struct {
	suggestions []GenreSuggestion
	err         error
}

func (batch genreBatch) chooseClassification(track GenreTrack, evidence genres.TagEvidence) (genres.Classification, string) {
	classification, err := suggestedClassification(track, batch.suggestions, batch.err)
	if err == nil {
		return classification, ""
	}
	return genres.ClassifyByTags(evidence), fmt.Sprintf("genre tag rules used for %s: %v", track.Path, err)
}

func suggestedClassification(track GenreTrack, suggestions []GenreSuggestion, batchErr error) (genres.Classification, error) {
	if batchErr != nil {
		return genres.Classification{}, batchErr
	}
	for _, suggestion := range suggestions {
		if suggestion.Key != string(track.ID) {
			continue
		}
		return genres.NewClassification(genres.ClassificationDetails{
			Genre: suggestion.Genre, Subgenre: suggestion.Subgenre, IsBrazilian: suggestion.IsBrazilian, IsKnown: suggestion.IsKnown,
		})
	}
	return genres.Classification{}, crediting.ErrSuggestionMissing
}

func appendWarning(warnings []string, warning string) []string {
	if warning == "" {
		return warnings
	}
	return append(warnings, warning)
}

func resolveTrackNationality(genreTracks []GenreTrack, classifications []genres.Classification, evidence []genres.TagEvidence) map[tracks.TrackID]genres.Classification {
	ballots := make([]genres.NationalityBallot, 0, len(genreTracks))
	for index, track := range genreTracks {
		ballots = append(ballots, genres.NationalityBallot{MainArtist: track.Credit.Artists().MainArtist(), Classification: classifications[index], Evidence: evidence[index]})
	}
	resolved := genres.ResolveNationality(ballots)
	byID := make(map[tracks.TrackID]genres.Classification, len(genreTracks))
	for index, track := range genreTracks {
		byID[track.ID] = resolved[index]
	}
	return byID
}
