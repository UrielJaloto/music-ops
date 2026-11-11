package test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/UrielJaloto/music-ops"

var contextOrder = map[string][]string{
	"domain":         {"credits", "audio", "filename", "genres", "tracks", "albums", "library", "runs"},
	"services":       {"diagnostics", "settings", "discovery", "conversion", "crediting", "classification", "metadata", "publishing", "importing", "watching", "session", "setup"},
	"presentation":   {"cli"},
	"infrastructure": {"process", "config", "runtime", "filesystem", "lastfm", "storage", "logging", "artifacts", "transcoding", "desktop", "advisor"},
}

var layerTargets = map[string][]string{
	"domain":         {},
	"services":       {"domain"},
	"presentation":   {"domain", "services"},
	"infrastructure": {"domain", "services"},
}

var driverOwners = map[string][]string{
	"github.com/abema/go-mp4":      {"transcoding"},
	"github.com/fsnotify/fsnotify": {"filesystem"},
	"github.com/sqweek/dialog":     {"desktop"},
	"go.etcd.io/bbolt":             {"storage"},
	"golang.org/x/sync":            {"runtime"},
	"golang.org/x/sys":             {"desktop", "filesystem", "transcoding"},
	"golang.org/x/text":            {"filesystem", "transcoding"},
	"golang.org/x/time":            {"lastfm"},
	"gopkg.in/yaml.v3":             {"config"},
}

func TestDependencyRules(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && (entry.Name() == "testdata" || strings.HasPrefix(entry.Name(), ".")) && path != root {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		return checkFile(t, root, path)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func checkFile(t *testing.T, root, path string) error {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, filepath.Dir(path))
	if err != nil {
		return err
	}
	from := filepath.ToSlash(rel)
	if top, _, _ := strings.Cut(from, "/"); top != "cmd" && top != "internal" {
		t.Errorf("%s lives outside cmd and internal", path)
	}
	for _, spec := range file.Imports {
		to, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return err
		}
		if !isAllowed(from, to) {
			t.Errorf("%s imports %s", path, to)
		}
	}
	return nil
}

func isAllowed(from, to string) bool {
	if isStandard(to) {
		return true
	}
	target, isInModule := strings.CutPrefix(to, modulePath+"/")
	if top, _, _ := strings.Cut(from, "/"); top == "cmd" {
		return isInModule && !strings.HasPrefix(target, "internal/test")
	}
	layer, context := locate(from)
	if layer == "test" {
		return isInModule && !strings.HasPrefix(target, "internal/test")
	}
	if !isInModule {
		return slices.Contains(ownersOf(to), context) && layer == "infrastructure"
	}
	targetLayer, targetContext := locate(target)
	if targetLayer == layer {
		return isEarlier(layer, targetContext, context)
	}
	return slices.Contains(layerTargets[layer], targetLayer) && slices.Contains(contextOrder[targetLayer], targetContext)
}

func locate(path string) (string, string) {
	rest, _ := strings.CutPrefix(path, "internal/")
	layer, context, _ := strings.Cut(rest, "/")
	context, _, _ = strings.Cut(context, "/")
	return layer, context
}

func isEarlier(layer, target, context string) bool {
	order := contextOrder[layer]
	targetIndex, contextIndex := slices.Index(order, target), slices.Index(order, context)
	return targetIndex >= 0 && contextIndex >= 0 && targetIndex <= contextIndex
}

func ownersOf(path string) []string {
	for prefix, owners := range driverOwners {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return owners
		}
	}
	return nil
}

func isStandard(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

func TestIsAllowed(t *testing.T) {
	internal := modulePath + "/internal/"
	cases := []struct {
		name     string
		from, to string
		expected bool
	}{
		{"DomainUsesStandardLibrary", "internal/domain/tracks", "strings", true},
		{"DomainUsesEarlierContext", "internal/domain/tracks", internal + "domain/credits", true},
		{"DomainUsesLaterContext", "internal/domain/credits", internal + "domain/tracks", false},
		{"DomainUsesItself", "internal/domain/tracks", internal + "domain/tracks", true},
		{"DomainUsesServices", "internal/domain/tracks", internal + "services/importing", false},
		{"DomainUsesDriver", "internal/domain/tracks", "gopkg.in/yaml.v3", false},
		{"DomainUsesUnknownContext", "internal/domain/tracks", internal + "domain/unknown", false},
		{"UnknownContextUsesDomain", "internal/domain/unknown", internal + "domain/credits", false},
		{"ServiceUsesDomain", "internal/services/importing", internal + "domain/tracks", true},
		{"ServiceUsesEarlierService", "internal/services/importing", internal + "services/publishing", true},
		{"ServiceUsesLaterService", "internal/services/publishing", internal + "services/importing", false},
		{"ServiceUsesInfrastructure", "internal/services/importing", internal + "infrastructure/storage", false},
		{"ServiceUsesPresentation", "internal/services/importing", internal + "presentation/cli", false},
		{"ServiceUsesDriver", "internal/services/importing", "golang.org/x/sync/errgroup", false},
		{"PresentationUsesServices", "internal/presentation/cli", internal + "services/importing", true},
		{"PresentationUsesDomain", "internal/presentation/cli", internal + "domain/albums", true},
		{"PresentationUsesInfrastructure", "internal/presentation/cli", internal + "infrastructure/config", false},
		{"PresentationUsesDriver", "internal/presentation/cli", "github.com/sqweek/dialog", false},
		{"InfrastructureUsesServices", "internal/infrastructure/storage", internal + "services/importing", true},
		{"InfrastructureUsesDomain", "internal/infrastructure/storage", internal + "domain/tracks", true},
		{"InfrastructureUsesEarlierContext", "internal/infrastructure/advisor", internal + "infrastructure/process", true},
		{"InfrastructureUsesLaterContext", "internal/infrastructure/process", internal + "infrastructure/advisor", false},
		{"InfrastructureUsesPresentation", "internal/infrastructure/storage", internal + "presentation/cli", false},
		{"InfrastructureUsesItsDriver", "internal/infrastructure/storage", "go.etcd.io/bbolt/errors", true},
		{"InfrastructureUsesAnotherDriver", "internal/infrastructure/config", "go.etcd.io/bbolt", false},
		{"InfrastructureUsesUnknownModule", "internal/infrastructure/storage", "github.com/x/y", false},
		{"InfrastructureUsesPrefixLookalike", "internal/infrastructure/config", "gopkg.in/yaml.v3x", false},
		{"InfrastructureUsesTest", "internal/infrastructure/storage", internal + "test/e2e", false},
		{"CmdUsesInfrastructure", "cmd/music-ops", internal + "infrastructure/storage", true},
		{"CmdUsesTest", "cmd/music-ops", internal + "test/e2e", false},
		{"CmdUsesDriver", "cmd/music-ops", "github.com/x/y", false},
		{"TestUsesInfrastructure", "internal/test/e2e", internal + "infrastructure/config", true},
		{"TestUsesDriver", "internal/test/e2e", "github.com/x/y", false},
		{"TestUsesTest", "internal/test", internal + "test/e2e", false},
		{"AnyUsesCmd", "internal/services/importing", modulePath + "/cmd/music-ops", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isAllowed(testCase.from, testCase.to); got != testCase.expected {
				t.Fatalf("isAllowed(%q, %q) = %v, want %v", testCase.from, testCase.to, got, testCase.expected)
			}
		})
	}
}
