package infrastructure

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const modulePath = "github.com/UrielJaloto/music-ops"

type dependencyEdge struct {
	from string
	to   string
}

var allowedLayerDependencies = map[string][]string{
	"domain":         {},
	"services":       {"domain"},
	"presentation":   {"domain", "services"},
	"infrastructure": {"domain", "services"},
	"cmd":            {"domain", "services", "presentation", "infrastructure"},
}

var thirdPartyAllowedLayers = map[string]bool{
	"presentation":   true,
	"infrastructure": true,
	"cmd":            true,
}

var forbiddenDomainImports = map[string]bool{
	"os":           true,
	"os/exec":      true,
	"net":          true,
	"net/http":     true,
	"io/fs":        true,
	"database/sql": true,
}

var allowedDomainContextDependencies = map[string][]string{
	"business_error": {},
	"audio":          {"business_error"},
	"credit":         {"business_error"},
	"genre":          {"business_error", "credit"},
	"filename":       {"business_error", "credit"},
	"track":          {"business_error", "audio", "credit", "genre", "filename"},
	"album":          {"business_error", "track"},
	"library":        {"business_error", "filename", "track"},
	"run":            {"business_error", "track"},
}

func TestLayerDependencies_AreLowerTriangular(t *testing.T) {
	var edges []dependencyEdge
	for _, imported := range moduleImports(t) {
		edges = append(edges, layerEdges(t, imported)...)
	}

	violations := findViolations(allowedLayerDependencies, edges)

	reportViolations(t, edges, violations)
}

func TestDomainContextDependencies_FollowDeclaredOrder(t *testing.T) {
	domainPackage := loadTypedPackage(t, "domain")
	edges := crossFileEdges(domainPackage, func(types.Object) bool { return true })
	contextEdges := make([]dependencyEdge, 0, len(edges))
	for _, edge := range edges {
		contextEdges = append(contextEdges, dependencyEdge{from: domainContext(t, edge.from), to: domainContext(t, edge.to)})
	}

	violations := findViolations(allowedDomainContextDependencies, withoutSelfEdges(contextEdges))

	reportViolations(t, contextEdges, violations)
}

func TestServices_DoNotReferenceOtherServices(t *testing.T) {
	servicesPackage := loadTypedPackage(t, "services")

	edges := crossFileEdges(servicesPackage, isServiceInternalReference)

	for _, edge := range edges {
		t.Errorf("services/%s references a name declared in services/%s", edge.from, edge.to)
	}
}

func TestMatrixChecker(t *testing.T) {
	cases := []struct {
		name               string
		edges              []dependencyEdge
		expectedViolations []dependencyEdge
	}{
		{name: "AllowedEdge_Passes", edges: []dependencyEdge{{"services", "domain"}}, expectedViolations: nil},
		{name: "EdgeAboveDiagonal_Fails", edges: []dependencyEdge{{"domain", "services"}}, expectedViolations: []dependencyEdge{{"domain", "services"}}},
		{name: "SymmetricEdges_Fail", edges: []dependencyEdge{{"services", "domain"}, {"domain", "services"}}, expectedViolations: []dependencyEdge{{"domain", "services"}}},
		{name: "PresentationToInfrastructure_Fails", edges: []dependencyEdge{{"presentation", "infrastructure"}}, expectedViolations: []dependencyEdge{{"presentation", "infrastructure"}}},
		{name: "UnknownElement_Fails", edges: []dependencyEdge{{"utils", "domain"}}, expectedViolations: []dependencyEdge{{"utils", "domain"}}},
		{name: "ImportOfCmd_Fails", edges: []dependencyEdge{{"infrastructure", "cmd"}}, expectedViolations: []dependencyEdge{{"infrastructure", "cmd"}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			violations := findViolations(allowedLayerDependencies, testCase.edges)

			if fmt.Sprint(violations) != fmt.Sprint(testCase.expectedViolations) {
				t.Fatalf("violations = %v, want %v", violations, testCase.expectedViolations)
			}
		})
	}
}

func TestServiceReferenceRule(t *testing.T) {
	servicePackage := types.NewPackage("example/services", "services")
	structType := types.NewStruct(nil, nil)
	interfaceType := types.NewInterfaceType(nil, nil)
	cases := []struct {
		name     string
		object   types.Object
		expected bool
	}{
		{"DataTransferStruct_IsAllowed", types.NewTypeName(0, servicePackage, "FoundFiles", structType), false},
		{"ServiceType_IsForbidden", types.NewTypeName(0, servicePackage, "SourceService", structType), true},
		{"Interface_IsForbidden", types.NewTypeName(0, servicePackage, "WorkPool", interfaceType), true},
		{"Constructor_IsForbidden", types.NewFunc(0, servicePackage, "NewSourceService", types.NewSignatureType(nil, nil, nil, nil, nil, false)), true},
		{"BusinessError_IsAllowed", types.NewVar(0, servicePackage, "ErrFolderMissing", types.Universe.Lookup("error").Type()), false},
		{"Unexported_IsAllowed", types.NewFunc(0, servicePackage, "wrapError", types.NewSignatureType(nil, nil, nil, nil, nil, false)), false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isServiceInternalReference(testCase.object); got != testCase.expected {
				t.Fatalf("isServiceInternalReference = %v, want %v", got, testCase.expected)
			}
		})
	}
}

func findViolations(allowed map[string][]string, edges []dependencyEdge) []dependencyEdge {
	var violations []dependencyEdge
	for _, edge := range edges {
		if isAllowedEdge(allowed, edge) {
			continue
		}
		violations = append(violations, edge)
	}
	return violations
}

func isAllowedEdge(allowed map[string][]string, edge dependencyEdge) bool {
	for _, target := range allowed[edge.from] {
		if target == edge.to {
			return true
		}
	}
	return false
}

type typedPackage struct {
	fileSet *token.FileSet
	syntax  []*ast.File
	types   *types.Package
	info    *types.Info
}

func moduleImports(t *testing.T) []*build.Package {
	t.Helper()
	var imported []*build.Package
	err := filepath.WalkDir("..", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") && path != ".." {
			return filepath.SkipDir
		}
		found, err := build.ImportDir(path, 0)
		if err == nil {
			relative, _ := filepath.Rel("..", path)
			found.ImportPath = modulePath + "/" + filepath.ToSlash(relative)
			imported = append(imported, found)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking packages: %v", err)
	}
	return imported
}

func loadTypedPackage(t *testing.T, layer string) typedPackage {
	t.Helper()
	folder := filepath.Join("..", layer)
	found, err := build.ImportDir(folder, 0)
	if err != nil {
		t.Fatalf("reading package %s: %v", layer, err)
	}
	loaded := typedPackage{fileSet: token.NewFileSet(), info: &types.Info{Uses: map[*ast.Ident]types.Object{}}}
	for _, name := range found.GoFiles {
		file, err := parser.ParseFile(loaded.fileSet, filepath.Join(folder, name), nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		loaded.syntax = append(loaded.syntax, file)
	}
	config := types.Config{Importer: importer.ForCompiler(loaded.fileSet, "source", nil)}
	loaded.types, err = config.Check(modulePath+"/"+layer, loaded.fileSet, loaded.syntax, loaded.info)
	if err != nil {
		t.Fatalf("type-checking %s: %v", layer, err)
	}
	return loaded
}

func packageLayer(packagePath string) string {
	relative := strings.TrimPrefix(strings.TrimPrefix(packagePath, modulePath), "/")
	return strings.Split(relative, "/")[0]
}

func layerEdges(t *testing.T, imported *build.Package) []dependencyEdge {
	t.Helper()
	layer := packageLayer(imported.ImportPath)
	var edges []dependencyEdge
	for _, importPath := range imported.Imports {
		if strings.HasPrefix(importPath, modulePath) {
			edges = append(edges, dependencyEdge{from: layer, to: packageLayer(importPath)})
			continue
		}
		checkExternalImport(t, layer, importPath)
	}
	return edges
}

func checkExternalImport(t *testing.T, layer, importPath string) {
	t.Helper()
	if isStandardLibrary(importPath) {
		if layer == "domain" && forbiddenDomainImports[importPath] {
			t.Errorf("domain imports forbidden package %s", importPath)
		}
		return
	}
	if !thirdPartyAllowedLayers[layer] {
		t.Errorf("%s imports third-party package %s", layer, importPath)
	}
}

func isStandardLibrary(importPath string) bool {
	return !strings.Contains(strings.Split(importPath, "/")[0], ".")
}

func crossFileEdges(loadedPackage typedPackage, isRelevant func(types.Object) bool) []dependencyEdge {
	edges := make([]dependencyEdge, 0, len(loadedPackage.syntax))
	for _, file := range loadedPackage.syntax {
		fromFile := filepath.Base(loadedPackage.fileSet.Position(file.Pos()).Filename)
		edges = append(edges, fileEdges(loadedPackage, file, fromFile, isRelevant)...)
	}
	return uniqueEdges(edges)
}

func fileEdges(loadedPackage typedPackage, file *ast.File, fromFile string, isRelevant func(types.Object) bool) []dependencyEdge {
	var edges []dependencyEdge
	ast.Inspect(file, func(node ast.Node) bool {
		identifier, isIdentifier := node.(*ast.Ident)
		if !isIdentifier {
			return true
		}
		object := loadedPackage.info.Uses[identifier]
		if object == nil || object.Pkg() != loadedPackage.types || !isRelevant(object) {
			return true
		}
		toFile := filepath.Base(loadedPackage.fileSet.Position(object.Pos()).Filename)
		if toFile != fromFile {
			edges = append(edges, dependencyEdge{from: fromFile, to: toFile})
		}
		return true
	})
	return edges
}

func isServiceInternalReference(object types.Object) bool {
	if !object.Exported() {
		return false
	}
	switch declared := object.(type) {
	case *types.Func:
		return true
	case *types.TypeName:
		return isServiceOrInterface(declared)
	}
	return false
}

func isServiceOrInterface(declared *types.TypeName) bool {
	_, isInterface := declared.Type().Underlying().(*types.Interface)
	return isInterface || strings.HasSuffix(declared.Name(), "Service")
}

func domainContext(t *testing.T, fileName string) string {
	t.Helper()
	base := strings.TrimSuffix(fileName, ".go")
	best := ""
	for context := range allowedDomainContextDependencies {
		if (base == context || strings.HasPrefix(base, context+"_")) && len(context) > len(best) {
			best = context
		}
	}
	if best == "" {
		t.Errorf("domain file %s does not belong to a declared context", fileName)
		return base
	}
	return best
}

func withoutSelfEdges(edges []dependencyEdge) []dependencyEdge {
	var result []dependencyEdge
	for _, edge := range uniqueEdges(edges) {
		if edge.from != edge.to {
			result = append(result, edge)
		}
	}
	return result
}

func uniqueEdges(edges []dependencyEdge) []dependencyEdge {
	seen := map[dependencyEdge]bool{}
	var result []dependencyEdge
	for _, edge := range edges {
		if seen[edge] {
			continue
		}
		seen[edge] = true
		result = append(result, edge)
	}
	sort.Slice(result, func(left, right int) bool {
		return fmt.Sprint(result[left]) < fmt.Sprint(result[right])
	})
	return result
}

func reportViolations(t *testing.T, edges, violations []dependencyEdge) {
	t.Helper()
	if len(violations) == 0 {
		return
	}
	for _, edge := range uniqueEdges(edges) {
		t.Logf("matrix: %s -> %s", edge.from, edge.to)
	}
	for _, violation := range violations {
		t.Errorf("forbidden dependency: %s -> %s", violation.from, violation.to)
	}
}
