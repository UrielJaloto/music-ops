package infrastructure

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/domain"
	"github.com/UrielJaloto/music-ops/services"
)

func environmentOf(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, found := values[name]
		return value, found
	}
}

func configStoreWithFile(t *testing.T, content string, environment map[string]string) ConfigStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "music-ops", "config.yaml")
	store := NewConfigStore(path, environmentOf(environment))
	if content == "" {
		return store
	}
	writeTestFile(t, path, content)
	return store
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestConfigStore_Load_Defaults(t *testing.T) {
	config, err := configStoreWithFile(t, "", nil).Load(context.Background(), nil)

	expected := Config{
		Output:        OutputConfig{Dir: `D:\Music`, Layout: "flat", AlbumTemplate: "{albumartist} - {album}"},
		Encoder:       EncoderConfig{FDKLibrary: `C:\Program Files\freac\codecs\FDK-AAC.dll`},
		LastFM:        LastFMConfig{BaseURL: "https://ws.audioscrobbler.com/2.0/"},
		Advisor:       AdvisorConfig{Port: 8089, BatchSize: 40},
		NumberingMode: "per_directory",
		Watch:         WatchConfig{Settle: 20 * time.Second},
	}
	if err != nil || config != expected {
		t.Fatalf("config = %+v err = %v", config, err)
	}
	if config.Layout() != domain.LayoutFlat || config.Numbering() != domain.NumberingPerDirectory || config.EncodingEngine() != domain.EngineFDK {
		t.Fatalf("conversions = %v %v %v", config.Layout(), config.Numbering(), config.EncodingEngine())
	}
}

func TestConfigStore_Load_Precedence(t *testing.T) {
	cases := []struct {
		name        string
		file        string
		environment map[string]string
		overrides   map[string]string
		expected    string
	}{
		{name: "FileOverridesDefault", file: "output:\n  dir: E:/Music\n", expected: "E:/Music"},
		{name: "EnvironmentOverridesFile", file: "output:\n  dir: E:/Music\n", environment: map[string]string{"MUSIC_OPS_OUTPUT_DIR": "F:/Music"}, expected: "F:/Music"},
		{name: "OverrideBeatsEnvironment", environment: map[string]string{"MUSIC_OPS_OUTPUT_DIR": "F:/Music"}, overrides: map[string]string{"output.dir": "G:/Music"}, expected: "G:/Music"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			config, err := configStoreWithFile(t, testCase.file, testCase.environment).Load(context.Background(), testCase.overrides)

			if err != nil || config.Output.Dir != testCase.expected {
				t.Fatalf("dir = %q err = %v", config.Output.Dir, err)
			}
		})
	}
}

func TestConfigStore_Load_Errors(t *testing.T) {
	cases := []struct {
		name          string
		file          string
		overrides     map[string]string
		expectedError error
	}{
		{name: "UnknownKeyInFile", file: "output:\n  color: blue\n", expectedError: services.ErrSettingUnknown},
		{name: "UnknownSectionInFile", file: "colors:\n  main: blue\n", expectedError: services.ErrSettingUnknown},
		{name: "UnknownKeyInOverride", overrides: map[string]string{"output.color": "blue"}, expectedError: services.ErrSettingUnknown},
		{name: "InvalidLayout", overrides: map[string]string{"output.layout": "tree"}, expectedError: services.ErrSettingInvalid},
		{name: "InvalidNumbering", overrides: map[string]string{"numbering.mode": "random"}, expectedError: services.ErrSettingInvalid},
		{name: "InvalidDuration", overrides: map[string]string{"watch.settle": "soon"}, expectedError: services.ErrSettingInvalid},
		{name: "NotANumber", overrides: map[string]string{"advisor.port": "http"}, expectedError: services.ErrSettingInvalid},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := configStoreWithFile(t, testCase.file, nil).Load(context.Background(), testCase.overrides)

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("error = %v, want %v", err, testCase.expectedError)
			}
		})
	}
}

func TestConfigStore_Load_MalformedFile(t *testing.T) {
	_, err := configStoreWithFile(t, "output: [", nil).Load(context.Background(), nil)

	if err == nil {
		t.Fatal("malformed YAML accepted")
	}
}

func TestConfigStore_Load_Boundaries(t *testing.T) {
	cases := []struct {
		key     string
		value   int
		isValid bool
	}{
		{"encoder.workers", -1, false},
		{"encoder.workers", 0, true},
		{"encoder.workers", 64, true},
		{"encoder.workers", 65, false},
		{"advisor.port", 1023, false},
		{"advisor.port", 1024, true},
		{"advisor.port", 65535, true},
		{"advisor.port", 65536, false},
		{"advisor.batch_size", 0, false},
		{"advisor.batch_size", 1, true},
		{"advisor.batch_size", 200, true},
		{"advisor.batch_size", 201, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.key+"="+strconv.Itoa(testCase.value), func(t *testing.T) {
			_, err := configStoreWithFile(t, "", nil).Load(context.Background(), map[string]string{testCase.key: strconv.Itoa(testCase.value)})

			if (err == nil) != testCase.isValid || (err != nil && !errors.Is(err, services.ErrSettingInvalid)) {
				t.Fatalf("err = %v, valid = %v", err, testCase.isValid)
			}
		})
	}
}

func TestConfigStore_Load_Conversions(t *testing.T) {
	overrides := map[string]string{"output.layout": "album_folders", "numbering.mode": "keep", "encoder.fdk_library": ""}

	config, err := configStoreWithFile(t, "", nil).Load(context.Background(), overrides)

	if err != nil || config.Layout() != domain.LayoutAlbumFolders || config.Numbering() != domain.NumberingKeepSource || config.EncodingEngine() != domain.EngineNative {
		t.Fatalf("config = %+v err = %v", config, err)
	}
}

func TestConfigStore_Save(t *testing.T) {
	store := configStoreWithFile(t, "", nil)

	if err := store.Save(context.Background(), "output.dir", "E:/Music"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), "advisor.port", "9000"); err != nil {
		t.Fatal(err)
	}
	config, err := store.Load(context.Background(), nil)

	if err != nil || config.Output.Dir != "E:/Music" || config.Advisor.Port != 9000 {
		t.Fatalf("config = %+v err = %v", config, err)
	}
	if _, err := os.Stat(store.Location()); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestConfigStore_Save_Errors(t *testing.T) {
	cases := []struct {
		name          string
		key           string
		value         string
		expectedError error
	}{
		{name: "UnknownKey", key: "output.color", value: "x", expectedError: services.ErrSettingUnknown},
		{name: "InvalidValue", key: "advisor.port", value: "80", expectedError: services.ErrSettingInvalid},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			store := configStoreWithFile(t, "", nil)

			err := store.Save(context.Background(), testCase.key, testCase.value)

			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("error = %v", err)
			}
			if _, statErr := os.Stat(store.Location()); statErr == nil {
				t.Fatal("file written for a rejected value")
			}
		})
	}
}

func TestConfigStore_SettingsStoreContract(t *testing.T) {
	store := configStoreWithFile(t, "lastfm:\n  api_key: secret\n", map[string]string{"MUSIC_OPS_WATCH_INBOX": "D:/Inbox"})

	values, err := store.Values(context.Background())

	if err != nil || values["lastfm.api_key"] != "secret" || values["watch.inbox"] != "D:/Inbox" || values["advisor.port"] != "8089" {
		t.Fatalf("values = %v err = %v", values, err)
	}
	if len(store.Keys()) != 16 || store.Keys()[0] != "output.dir" {
		t.Fatalf("keys = %v", store.Keys())
	}
	if DefaultConfigPath(`C:\AppData`) != filepath.Join(`C:\AppData`, "music-ops", "config.yaml") {
		t.Fatalf("default path = %s", DefaultConfigPath(`C:\AppData`))
	}
}

func TestConfigStore_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := configStoreWithFile(t, "", nil)

	if _, err := store.Load(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("load err = %v", err)
	}
	if err := store.Save(ctx, "output.dir", "x"); !errors.Is(err, context.Canceled) {
		t.Fatalf("save err = %v", err)
	}
}

func TestReadEnvironmentPaths(t *testing.T) {
	paths := ReadEnvironmentPaths(environmentOf(map[string]string{"APPDATA": `C:\Roaming`, "LOCALAPPDATA": `C:\Local`}))

	if paths != (EnvironmentPaths{AppData: `C:\Roaming`, LocalAppData: `C:\Local`}) {
		t.Fatalf("paths = %+v", paths)
	}
}
