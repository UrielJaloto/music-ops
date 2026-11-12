package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/UrielJaloto/music-ops/internal/domain/albums"
	"github.com/UrielJaloto/music-ops/internal/domain/filename"
	"github.com/UrielJaloto/music-ops/internal/domain/genres"
	"github.com/UrielJaloto/music-ops/internal/services/settings"
	"gopkg.in/yaml.v3"
)

const (
	configFileName       = "config.yaml"
	environmentPrefix    = "MUSIC_OPS_"
	keySeparator         = "."
	configFilePermission = 0o600
	configDirPermission  = 0o750
	layoutFlat           = "flat"
	layoutAlbumFolders   = "album_folders"
	numberingPerDir      = "per_directory"
	numberingKeep        = "keep"
	minimumWorkers       = 0
	maximumWorkers       = 64
	minimumPort          = 1024
	maximumPort          = 65535
	minimumBatch         = 1
	maximumBatch         = 200
	applicationFolder    = "music-ops"
	musicFolder          = "Music"
	defaultAlbumTemplate = "{albumartist} - {album}"
	defaultLastFMURL     = "https://ws.audioscrobbler.com/2.0/"
	defaultAdvisorPort   = "8089"
	defaultBatchSize     = "40"
	defaultWorkers       = "0"
	defaultWatchSettle   = "20s"
)

var defaultValues = map[string]string{
	"output.dir": "", "output.layout": layoutFlat, "output.album_template": defaultAlbumTemplate,
	"encoder.fdk_library": "", "encoder.ffmpeg": "", "encoder.ffprobe": "", "encoder.workers": defaultWorkers,
	"lastfm.api_key": "", "lastfm.base_url": defaultLastFMURL,
	"advisor.llama_server": "", "advisor.model_file": "", "advisor.port": defaultAdvisorPort, "advisor.batch_size": defaultBatchSize,
	"numbering.mode": numberingPerDir,
	"watch.inbox":    "", "watch.settle": defaultWatchSettle,
	"genres.file": "",
}

var configKeys = []string{
	"output.dir", "output.layout", "output.album_template",
	"encoder.fdk_library", "encoder.ffmpeg", "encoder.ffprobe", "encoder.workers",
	"lastfm.api_key", "lastfm.base_url",
	"advisor.llama_server", "advisor.model_file", "advisor.port", "advisor.batch_size",
	"numbering.mode",
	"watch.inbox", "watch.settle",
	"genres.file",
}

var configValidators = map[string]func(string) error{
	"output.layout":      requireOneOf(layoutFlat, layoutAlbumFolders),
	"numbering.mode":     requireOneOf(numberingPerDir, numberingKeep),
	"encoder.workers":    requireIntWithin(minimumWorkers, maximumWorkers),
	"advisor.port":       requireIntWithin(minimumPort, maximumPort),
	"advisor.batch_size": requireIntWithin(minimumBatch, maximumBatch),
	"watch.settle":       validateDuration,
}

type OutputConfig struct {
	Dir           string
	Layout        string
	AlbumTemplate string
}

type EncoderConfig struct {
	FDKLibrary string
	FFmpeg     string
	FFprobe    string
	Workers    int
}

type LastFMConfig struct {
	APIKey  string
	BaseURL string
}

type AdvisorConfig struct {
	LlamaServer string
	ModelFile   string
	Port        int
	BatchSize   int
}

type WatchConfig struct {
	Inbox  string
	Settle time.Duration
}

type Config struct {
	Output        OutputConfig
	Encoder       EncoderConfig
	LastFM        LastFMConfig
	Advisor       AdvisorConfig
	NumberingMode string
	Watch         WatchConfig
	Genres        genres.Pool
}

func (config Config) ParseLayout() filename.Layout {
	if config.Output.Layout == layoutAlbumFolders {
		return filename.LayoutAlbumFolders
	}
	return filename.LayoutFlat
}

func (config Config) ParseNumbering() albums.NumberingMode {
	if config.NumberingMode == numberingKeep {
		return albums.NumberingKeepSource
	}
	return albums.NumberingPerDirectory
}

var ErrConfigFolderUnknown = errors.New("the user configuration folder is unknown")

type Locations struct {
	ConfigDir string
	WorkDir   string
	MusicDir  string
}

func FindUserLocations() Locations {
	return Locations{
		ConfigDir: resolveUserFolder(os.UserConfigDir, applicationFolder),
		WorkDir:   resolveUserFolder(os.UserCacheDir, applicationFolder),
		MusicDir:  resolveUserFolder(os.UserHomeDir, musicFolder),
	}
}

func resolveUserFolder(base func() (string, error), name string) string {
	folder, err := base()
	if err != nil {
		return ""
	}
	return filepath.Join(folder, name)
}

func buildConfigPath(configDir string) string {
	if configDir == "" {
		return ""
	}
	return filepath.Join(configDir, configFileName)
}

type ConfigStore struct {
	path        string
	musicDir    string
	environment func(string) (string, bool)
	rename      func(oldPath, newPath string) error
}

func NewConfigStore(locations Locations, environment func(string) (string, bool)) ConfigStore {
	return ConfigStore{
		path:        buildConfigPath(locations.ConfigDir),
		musicDir:    locations.MusicDir,
		environment: environment,
		rename:      os.Rename,
	}
}

func (store ConfigStore) Load(ctx context.Context, overrides map[string]string) (Config, error) {
	values, err := store.Values(ctx)
	if err != nil {
		return Config{}, err
	}
	if err := mergeKnownKeys(values, overrides); err != nil {
		return Config{}, err
	}
	config, err := toConfig(values)
	if err != nil {
		return Config{}, err
	}
	if config.Output.Dir == "" {
		config.Output.Dir = store.musicDir
	}
	config.Genres, err = loadGenrePool(values["genres.file"])
	if err != nil {
		return Config{}, err
	}
	return config, nil
}

func (store ConfigStore) Values(ctx context.Context) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	values := maps.Clone(defaultValues)
	fileValues, err := store.readUserFile()
	if err != nil {
		return nil, err
	}
	if err := mergeKnownKeys(values, fileValues); err != nil {
		return nil, err
	}
	store.applyEnvironment(values)
	return values, nil
}

func (store ConfigStore) Save(ctx context.Context, key, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !slices.Contains(configKeys, key) {
		return fmt.Errorf("%w: %s", settings.ErrSettingUnknown, key)
	}
	if err := validateSetting(key, value); err != nil {
		return err
	}
	fileValues, err := store.readUserFile()
	if err != nil {
		return err
	}
	fileValues[key] = value
	return store.writeUserFile(fileValues)
}

func (store ConfigStore) Location() string {
	return store.path
}

func (store ConfigStore) Keys() []string {
	return slices.Clone(configKeys)
}

func (store ConfigStore) readUserFile() (map[string]string, error) {
	if store.path == "" {
		return map[string]string{}, nil
	}
	data, err := os.ReadFile(filepath.Clean(store.path))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading configuration %s: %w", store.path, err)
	}
	values, err := parseConfigYAML(data)
	if err != nil {
		return nil, fmt.Errorf("reading configuration %s: %w", store.path, err)
	}
	return values, nil
}

func (store ConfigStore) writeUserFile(values map[string]string) error {
	if store.path == "" {
		return ErrConfigFolderUnknown
	}
	data, _ := yaml.Marshal(nestConfigValues(values))
	if err := os.MkdirAll(filepath.Dir(store.path), configDirPermission); err != nil {
		return fmt.Errorf("creating configuration folder: %w", err)
	}
	temporary := store.path + ".tmp"
	if err := os.WriteFile(temporary, data, configFilePermission); err != nil {
		return fmt.Errorf("writing configuration: %w", err)
	}
	if err := store.rename(temporary, store.path); err != nil {
		return fmt.Errorf("replacing configuration: %w", err)
	}
	return nil
}

func (store ConfigStore) applyEnvironment(values map[string]string) {
	for _, key := range configKeys {
		if value, isSet := store.environment(buildEnvironmentName(key)); isSet {
			values[key] = value
		}
	}
}

func buildEnvironmentName(key string) string {
	return environmentPrefix + strings.ToUpper(strings.ReplaceAll(key, keySeparator, "_"))
}

func parseConfigYAML(data []byte) (map[string]string, error) {
	var sections map[string]map[string]string
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&sections); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	values := map[string]string{}
	for section, entries := range sections {
		flattenSection(values, section, entries)
	}
	return values, nil
}

func flattenSection(values map[string]string, section string, entries map[string]string) {
	for name, value := range entries {
		values[section+keySeparator+name] = value
	}
}

func nestConfigValues(values map[string]string) map[string]map[string]string {
	sections := map[string]map[string]string{}
	for key, value := range values {
		section, name, _ := strings.Cut(key, keySeparator)
		if sections[section] == nil {
			sections[section] = map[string]string{}
		}
		sections[section][name] = value
	}
	return sections
}

func mergeKnownKeys(target, source map[string]string) error {
	for key, value := range source {
		if !slices.Contains(configKeys, key) {
			return fmt.Errorf("%w: %s", settings.ErrSettingUnknown, key)
		}
		target[key] = value
	}
	return nil
}

func validateSetting(key, value string) error {
	validator, hasValidator := configValidators[key]
	if !hasValidator {
		return nil
	}
	if err := validator(value); err != nil {
		return fmt.Errorf("%w: %s: %w", settings.ErrSettingInvalid, key, err)
	}
	return nil
}

func toConfig(values map[string]string) (Config, error) {
	for _, key := range configKeys {
		if err := validateSetting(key, values[key]); err != nil {
			return Config{}, err
		}
	}
	return Config{
		Output:        OutputConfig{Dir: values["output.dir"], Layout: values["output.layout"], AlbumTemplate: values["output.album_template"]},
		Encoder:       EncoderConfig{FDKLibrary: values["encoder.fdk_library"], FFmpeg: values["encoder.ffmpeg"], FFprobe: values["encoder.ffprobe"], Workers: parseIntOrZero(values["encoder.workers"])},
		LastFM:        LastFMConfig{APIKey: values["lastfm.api_key"], BaseURL: values["lastfm.base_url"]},
		Advisor:       toAdvisorConfig(values),
		NumberingMode: values["numbering.mode"],
		Watch:         WatchConfig{Inbox: values["watch.inbox"], Settle: parseDurationOrZero(values["watch.settle"])},
	}, nil
}

func toAdvisorConfig(values map[string]string) AdvisorConfig {
	return AdvisorConfig{
		LlamaServer: values["advisor.llama_server"],
		ModelFile:   values["advisor.model_file"],
		Port:        parseIntOrZero(values["advisor.port"]),
		BatchSize:   parseIntOrZero(values["advisor.batch_size"]),
	}
}

func parseDurationOrZero(value string) time.Duration {
	duration, _ := time.ParseDuration(value)
	return duration
}

func parseIntOrZero(value string) int {
	number, _ := strconv.Atoi(value)
	return number
}

func requireOneOf(allowed ...string) func(string) error {
	return func(value string) error {
		if !slices.Contains(allowed, value) {
			return fmt.Errorf("must be one of %s", strings.Join(allowed, ", "))
		}
		return nil
	}
}

func requireIntWithin(minimum, maximum int) func(string) error {
	return func(value string) error {
		number, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("must be a whole number: %w", err)
		}
		if number < minimum || number > maximum {
			return fmt.Errorf("must be between %d and %d", minimum, maximum)
		}
		return nil
	}
}

func validateDuration(value string) error {
	if _, err := time.ParseDuration(value); err != nil {
		return fmt.Errorf("must be a duration such as 20s: %w", err)
	}
	return nil
}
