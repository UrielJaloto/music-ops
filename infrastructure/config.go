package infrastructure

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/UrielJaloto/music-ops/domain"
	"github.com/UrielJaloto/music-ops/services"
)

//go:embed config_default.yaml
var defaultConfigYAML []byte

const (
	configFolderName     = "music-ops"
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
	appDataVariable      = "APPDATA"
	localAppDataVariable = "LOCALAPPDATA"
)

var configKeys = []string{
	"output.dir", "output.layout", "output.album_template",
	"encoder.fdk_library", "encoder.ffmpeg", "encoder.ffprobe", "encoder.workers",
	"lastfm.api_key", "lastfm.base_url",
	"advisor.llama_server", "advisor.model_file", "advisor.port", "advisor.batch_size",
	"numbering.mode",
	"watch.inbox", "watch.settle",
}

var configValidators = map[string]func(string) error{
	"output.layout":      oneOf(layoutFlat, layoutAlbumFolders),
	"numbering.mode":     oneOf(numberingPerDir, numberingKeep),
	"encoder.workers":    intWithin(minimumWorkers, maximumWorkers),
	"advisor.port":       intWithin(minimumPort, maximumPort),
	"advisor.batch_size": intWithin(minimumBatch, maximumBatch),
	"watch.settle":       validDuration,
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
}

func (config Config) EncodingEngine() domain.EncoderEngine {
	if config.Encoder.FDKLibrary == "" {
		return domain.EngineNative
	}
	return domain.EngineFDK
}

func (config Config) Layout() domain.Layout {
	if config.Output.Layout == layoutAlbumFolders {
		return domain.LayoutAlbumFolders
	}
	return domain.LayoutFlat
}

func (config Config) Numbering() domain.NumberingMode {
	if config.NumberingMode == numberingKeep {
		return domain.NumberingKeepSource
	}
	return domain.NumberingPerDirectory
}

type ConfigStore struct {
	path        string
	environment func(string) (string, bool)
}

func NewConfigStore(path string, environment func(string) (string, bool)) ConfigStore {
	return ConfigStore{path: path, environment: environment}
}

func DefaultConfigPath(appData string) string {
	return filepath.Join(appData, configFolderName, configFileName)
}

func (store ConfigStore) Load(ctx context.Context, overrides map[string]string) (Config, error) {
	values, err := store.Values(ctx)
	if err != nil {
		return Config{}, err
	}
	if err := mergeKnown(values, overrides); err != nil {
		return Config{}, err
	}
	return toConfig(values)
}

type EnvironmentPaths struct {
	AppData      string
	LocalAppData string
}

func ReadEnvironmentPaths(environment func(string) (string, bool)) EnvironmentPaths {
	appData, _ := environment(appDataVariable)
	localAppData, _ := environment(localAppDataVariable)
	return EnvironmentPaths{AppData: appData, LocalAppData: localAppData}
}

func (store ConfigStore) Values(ctx context.Context) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	values, err := parseConfigYAML(defaultConfigYAML)
	if err != nil {
		return nil, fmt.Errorf("reading default configuration: %w", err)
	}
	fileValues, err := store.readUserFile()
	if err != nil {
		return nil, err
	}
	if err := mergeKnown(values, fileValues); err != nil {
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
		return fmt.Errorf("%w: %s", services.ErrSettingUnknown, key)
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
	data, err := yaml.Marshal(nestConfigValues(values))
	if err != nil {
		return fmt.Errorf("encoding configuration: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(store.path), configDirPermission); err != nil {
		return fmt.Errorf("creating configuration folder: %w", err)
	}
	temporary := store.path + ".tmp"
	if err := os.WriteFile(temporary, data, configFilePermission); err != nil {
		return fmt.Errorf("writing configuration: %w", err)
	}
	if err := os.Rename(temporary, store.path); err != nil {
		return fmt.Errorf("replacing configuration: %w", err)
	}
	return nil
}

func (store ConfigStore) applyEnvironment(values map[string]string) {
	for _, key := range configKeys {
		if value, isSet := store.environment(environmentName(key)); isSet {
			values[key] = value
		}
	}
}

func environmentName(key string) string {
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

func mergeKnown(target, source map[string]string) error {
	for key, value := range source {
		if !slices.Contains(configKeys, key) {
			return fmt.Errorf("%w: %s", services.ErrSettingUnknown, key)
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
		return fmt.Errorf("%w: %s: %w", services.ErrSettingInvalid, key, err)
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
		Encoder:       EncoderConfig{FDKLibrary: values["encoder.fdk_library"], FFmpeg: values["encoder.ffmpeg"], FFprobe: values["encoder.ffprobe"], Workers: atoiOrZero(values["encoder.workers"])},
		LastFM:        LastFMConfig{APIKey: values["lastfm.api_key"], BaseURL: values["lastfm.base_url"]},
		Advisor:       toAdvisorConfig(values),
		NumberingMode: values["numbering.mode"],
		Watch:         WatchConfig{Inbox: values["watch.inbox"], Settle: durationOrZero(values["watch.settle"])},
	}, nil
}

func toAdvisorConfig(values map[string]string) AdvisorConfig {
	return AdvisorConfig{
		LlamaServer: values["advisor.llama_server"],
		ModelFile:   values["advisor.model_file"],
		Port:        atoiOrZero(values["advisor.port"]),
		BatchSize:   atoiOrZero(values["advisor.batch_size"]),
	}
}

func durationOrZero(value string) time.Duration {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0
	}
	return duration
}

func atoiOrZero(value string) int {
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return number
}

func oneOf(allowed ...string) func(string) error {
	return func(value string) error {
		if !slices.Contains(allowed, value) {
			return fmt.Errorf("must be one of %s", strings.Join(allowed, ", "))
		}
		return nil
	}
}

func intWithin(minimum, maximum int) func(string) error {
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

func validDuration(value string) error {
	if _, err := time.ParseDuration(value); err != nil {
		return fmt.Errorf("must be a duration such as 20s: %w", err)
	}
	return nil
}
