package metadata

import (
	"context"
	"fmt"

	"github.com/UrielJaloto/music-ops/internal/domain/albums"
	"github.com/UrielJaloto/music-ops/internal/domain/credits"
	"github.com/UrielJaloto/music-ops/internal/domain/genres"
	"github.com/UrielJaloto/music-ops/internal/domain/tracks"
	"github.com/UrielJaloto/music-ops/internal/services/classification"
	"github.com/UrielJaloto/music-ops/internal/services/crediting"
)

type CreditNormalizer interface {
	Normalize(ctx context.Context, input crediting.NormalizeCreditsInput) (crediting.NormalizedCredits, error)
}

type GenreClassifier interface {
	Classify(ctx context.Context, input classification.ClassifyGenresInput) (classification.ClassifiedGenres, error)
}

type MetadataTrack struct {
	ID     tracks.TrackID
	Source tracks.SourceDetails
}

type ResolveMetadataInput struct {
	Tracks     []MetadataTrack
	Numbering  albums.NumberingMode
	FixedGenre string
}

type ResolvedTrack struct {
	ID             tracks.TrackID
	Credit         credits.Credit
	Classification genres.Classification
}

type MetadataFailure struct {
	ID     tracks.TrackID
	Path   string
	Reason string
}

type ResolvedMetadata struct {
	Tracks   []ResolvedTrack
	Failed   []MetadataFailure
	Warnings []string
}

type MetadataService struct {
	credits CreditNormalizer
	genres  GenreClassifier
}

func NewMetadataService(credits CreditNormalizer, genres GenreClassifier) MetadataService {
	return MetadataService{credits: credits, genres: genres}
}

func (service MetadataService) Resolve(ctx context.Context, input ResolveMetadataInput) (ResolvedMetadata, error) {
	normalized, err := service.credits.Normalize(ctx, crediting.NormalizeCreditsInput{Tracks: toCreditTracks(input.Tracks)})
	if err != nil {
		return ResolvedMetadata{}, fmt.Errorf("normalizing credits: %w", err)
	}
	genreTracks := toGenreTracks(input.Tracks, normalized.Credits)
	classified, err := service.classify(ctx, genreTracks, input.FixedGenre)
	if err != nil {
		return ResolvedMetadata{}, err
	}
	return ResolvedMetadata{
		Tracks:   toResolvedTracks(genreTracks, classified.Classifications),
		Failed:   toMetadataFailures(normalized.Failed),
		Warnings: append(append([]string{}, normalized.Warnings...), classified.Warnings...),
	}, nil
}

func (service MetadataService) classify(ctx context.Context, genreTracks []classification.GenreTrack, fixedGenre string) (classification.ClassifiedGenres, error) {
	if fixedGenre == "" {
		classified, err := service.genres.Classify(ctx, classification.ClassifyGenresInput{Tracks: genreTracks})
		if err != nil {
			return classification.ClassifiedGenres{}, fmt.Errorf("classifying genres: %w", err)
		}
		return classified, nil
	}
	fixed, err := genres.ParseGenre(fixedGenre)
	if err != nil {
		return classification.ClassifiedGenres{}, err
	}
	classifications := make(map[tracks.TrackID]genres.Classification, len(genreTracks))
	for _, track := range genreTracks {
		classifications[track.ID] = fixed
	}
	return classification.ClassifiedGenres{Classifications: classifications}, nil
}

func toCreditTracks(tracks []MetadataTrack) []crediting.CreditTrack {
	creditTracks := make([]crediting.CreditTrack, 0, len(tracks))
	for _, track := range tracks {
		tags := track.Source.Tags
		creditTracks = append(creditTracks, crediting.CreditTrack{ID: track.ID, Source: credits.CreditSource{
			Path: track.Source.Path, Title: tags.Title, Artist: tags.Artist, AlbumArtist: tags.AlbumArtist,
		}.FillTitleFromPath()})
	}
	return creditTracks
}

func toGenreTracks(tracks []MetadataTrack, credits map[tracks.TrackID]credits.Credit) []classification.GenreTrack {
	genreTracks := make([]classification.GenreTrack, 0, len(tracks))
	for _, track := range tracks {
		credit, found := credits[track.ID]
		if !found {
			continue
		}
		genreTracks = append(genreTracks, classification.GenreTrack{ID: track.ID, Path: track.Source.Path, Credit: credit})
	}
	return genreTracks
}

func toResolvedTracks(tracks []classification.GenreTrack, classifications map[tracks.TrackID]genres.Classification) []ResolvedTrack {
	resolved := make([]ResolvedTrack, 0, len(tracks))
	for _, track := range tracks {
		resolved = append(resolved, ResolvedTrack{ID: track.ID, Credit: track.Credit, Classification: classifications[track.ID]})
	}
	return resolved
}

func toMetadataFailures(failures []crediting.CreditFailure) []MetadataFailure {
	converted := make([]MetadataFailure, 0, len(failures))
	for _, failure := range failures {
		converted = append(converted, MetadataFailure(failure))
	}
	return converted
}
