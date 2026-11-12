package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/UrielJaloto/music-ops/internal/domain/genres"
	"gopkg.in/yaml.v3"
)

type genreFileEntry struct {
	Name         string   `yaml:"name"`
	Subgenres    []string `yaml:"subgenres"`
	NationalOnly bool     `yaml:"national_only"`
	Hint         string   `yaml:"hint"`
}

func loadGenrePool(path string) (genres.Pool, error) {
	if path == "" {
		return genres.NewDefaultPool(), nil
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return genres.Pool{}, fmt.Errorf("reading the genres file: %w", err)
	}
	entries := []genreFileEntry{}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&entries); err != nil && !errors.Is(err, io.EOF) {
		return genres.Pool{}, fmt.Errorf("parsing the genres file %s: %w", path, err)
	}
	definitions := make([]genres.GenreDefinition, 0, len(entries))
	for _, entry := range entries {
		definitions = append(definitions, genres.GenreDefinition{Name: entry.Name, Subgenres: entry.Subgenres, IsNationalOnly: entry.NationalOnly, Hint: entry.Hint})
	}
	pool, err := genres.NewPool(definitions)
	if err != nil {
		return genres.Pool{}, fmt.Errorf("checking the genres file %s: %w", path, err)
	}
	return pool, nil
}
