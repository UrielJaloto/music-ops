package infrastructure

import (
	"fmt"
	"go/ast"
	"go/types"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
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
	loaded := loadModulePackages(t)
	var edges []dependencyEdge
	for _, loadedPackage := range loaded {
		edges = append(edges, layerEdges(t, loadedPackage)...)
	}

	violations := findViolations(allowedLayerDependencies, edges)

	reportViolations(t, edges, violations)
}

func TestDomainContextDependencies_FollowDeclaredOrder(t *testing.T) {
	domainPackage := findLoadedPackage(t, loadModulePackages(t), "domain")
	edges := crossFileEdges(domainPackage, func(object types.Object) bool { return true })
	contextEdges := make([]dependencyEdge, 0, len(edges))
	for _, edge := range edges {
		contextEdges = append(contextEdges, dependencyEdge{from: domainContext(t, edge.from), to: domainContext(t, edge.to)})
	}

	violations := findViolations(allowedDomainContextDependencies, withoutSelfEdges(contextEdges))

	reportViolations(t, contextEdges, violations)
}

func TestServices_DoNotReferenceOtherServices(t *testing.T) {
	servicesPackage := findLoadedPackage(t, loadModulePackages(t), "services")

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

func loadModulePackages(t *testing.T) []*packages.Package {
	t.Helper()
	config := &packages.Config{
		Mode: packages.NeedName | packages.NeedImports | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Dir:  "..",
	}
	loaded, err := packages.Load(config, "./...")
	if err != nil {
		t.Fatalf("loading packages: %v", err)
	}
	if packages.PrintErrors(loaded) > 0 {
		t.Fatal("packages have errors")
	}
	return loaded
}

func findLoadedPackage(t *testing.T, loaded []*packages.Package, layer string) *packages.Package {
	t.Helper()
	for _, loadedPackage := range loaded {
		if packageLayer(loadedPackage.PkgPath) == layer {
			return loadedPackage
		}
	}
	t.Fatalf("package %s not found", layer)
	return nil
}

func packageLayer(packagePath string) string {
	relative := strings.TrimPrefix(strings.TrimPrefix(packagePath, modulePath), "/")
	return strings.Split(relative, "/")[0]
}

func layerEdges(t *testing.T, loadedPackage *packages.Package) []dependencyEdge {
	t.Helper()
	layer := packageLayer(loadedPackage.PkgPath)
	var edges []dependencyEdge
	for importPath := range loadedPackage.Imports {
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

func crossFileEdges(loadedPackage *packages.Package, isRelevant func(types.Object) bool) []dependencyEdge {
	var edges []dependencyEdge
	for _, file := range loadedPackage.Syntax {
		fromFile := filepath.Base(loadedPackage.Fset.Position(file.Pos()).Filename)
		edges = append(edges, fileEdges(loadedPackage, file, fromFile, isRelevant)...)
	}
	return uniqueEdges(edges)
}

func fileEdges(loadedPackage *packages.Package, file *ast.File, fromFile string, isRelevant func(types.Object) bool) []dependencyEdge {
	var edges []dependencyEdge
	ast.Inspect(file, func(node ast.Node) bool {
		identifier, isIdentifier := node.(*ast.Ident)
		if !isIdentifier {
			return true
		}
		object := loadedPackage.TypesInfo.Uses[identifier]
		if object == nil || object.Pkg() != loadedPackage.Types || !isRelevant(object) {
			return true
		}
		toFile := filepath.Base(loadedPackage.Fset.Position(object.Pos()).Filename)
		if toFile != fromFile {
			edges = append(edges, dependencyEdge{from: fromFile, to: toFile})
		}
		return true
	})
	return edges
}

func isServiceInternalReference(object types.Object) bool {
	if !object.Exported() || isField(object) {
		return false
	}
	return !isDataTransferName(object.Name())
}

func isField(object types.Object) bool {
	variable, isVariable := object.(*types.Var)
	return isVariable && variable.IsField()
}

func isDataTransferName(name string) bool {
	return strings.HasSuffix(name, "Input") || strings.HasSuffix(name, "Output")
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
