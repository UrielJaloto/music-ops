package services

import (
	"context"
	"fmt"

	"github.com/UrielJaloto/music-ops/domain"
)

type CreditNormalizer interface {
	Normalize(ctx context.Context, input NormalizeCreditsInput) (NormalizedCredits, error)
}

type GenreClassifier interface {
	Classify(ctx context.Context, input ClassifyGenresInput) (ClassifiedGenres, error)
}

type MetadataTrack struct {
	ID     domain.TrackID
	Source domain.SourceDetails
}

type ResolveMetadataInput struct {
	Tracks    []MetadataTrack
	Numbering domain.NumberingMode
}

type ResolvedTrack struct {
	ID             domain.TrackID
	Credit         domain.Credit
	Classification domain.Classification
}

type MetadataFailure struct {
	ID     domain.TrackID
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
	normalized, err := service.credits.Normalize(ctx, NormalizeCreditsInput{Tracks: toCreditTracks(input.Tracks)})
	if err != nil {
		return ResolvedMetadata{}, fmt.Errorf("normalizing credits: %w", err)
	}
	genreTracks := toGenreTracks(input.Tracks, normalized.Credits)
	classified, err := service.genres.Classify(ctx, ClassifyGenresInput{Tracks: genreTracks})
	if err != nil {
		return ResolvedMetadata{}, fmt.Errorf("classifying genres: %w", err)
	}
	return ResolvedMetadata{
		Tracks:   toResolvedTracks(genreTracks, classified.Classifications),
		Failed:   toMetadataFailures(normalized.Failed),
		Warnings: append(append([]string{}, normalized.Warnings...), classified.Warnings...),
	}, nil
}

func toCreditTracks(tracks []MetadataTrack) []CreditTrack {
	creditTracks := make([]CreditTrack, 0, len(tracks))
	for _, track := range tracks {
		tags := track.Source.Tags
		creditTracks = append(creditTracks, CreditTrack{ID: track.ID, Source: domain.CreditSource{
			Path: track.Source.Path, Title: tags.Title, Artist: tags.Artist, AlbumArtist: tags.AlbumArtist,
		}})
	}
	return creditTracks
}

func toGenreTracks(tracks []MetadataTrack, credits map[domain.TrackID]domain.Credit) []GenreTrack {
	genreTracks := make([]GenreTrack, 0, len(tracks))
	for _, track := range tracks {
		credit, found := credits[track.ID]
		if !found {
			continue
		}
		genreTracks = append(genreTracks, GenreTrack{ID: track.ID, Path: track.Source.Path, Credit: credit})
	}
	return genreTracks
}

func toResolvedTracks(tracks []GenreTrack, classifications map[domain.TrackID]domain.Classification) []ResolvedTrack {
	resolved := make([]ResolvedTrack, 0, len(tracks))
	for _, track := range tracks {
		resolved = append(resolved, ResolvedTrack{ID: track.ID, Credit: track.Credit, Classification: classifications[track.ID]})
	}
	return resolved
}

func toMetadataFailures(failures []CreditFailure) []MetadataFailure {
	converted := make([]MetadataFailure, 0, len(failures))
	for _, failure := range failures {
		converted = append(converted, MetadataFailure(failure))
	}
	return converted
}
