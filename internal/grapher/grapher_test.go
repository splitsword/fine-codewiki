package grapher

import (
	"fmt"
	"testing"

	"github.com/splitsword/fine-codewiki/internal/analyzer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildGraphFromFiles(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "models/user.py",
			Classes: []analyzer.ClassInfo{
				{Name: "User"},
			},
			Imports: []analyzer.ImportInfo{
				{Module: ".base", Name: "BaseModel", IsRelative: true},
				{Module: "..utils.crypto", Name: "hash_password", IsRelative: true},
			},
		},
		{
			Filename: "services/user_service.py",
			Classes: []analyzer.ClassInfo{
				{Name: "UserService"},
			},
			Imports: []analyzer.ImportInfo{
				{Module: "..models.user", Name: "User", IsRelative: true},
				{Module: "..repositories.user_repository", Name: "UserRepository", IsRelative: true},
				{Module: "..utils.logger", Name: "get_logger", IsRelative: true},
			},
		},
		{
			Filename: "repositories/user_repository.py",
			Classes: []analyzer.ClassInfo{
				{Name: "UserRepository"},
			},
			Imports: []analyzer.ImportInfo{
				{Module: "..models.user", Name: "User", IsRelative: true},
			},
		},
		{
			Filename: "utils/crypto.py",
			Functions: []analyzer.FunctionInfo{
				{Name: "hash_password"},
				{Name: "verify_password"},
			},
		},
		{
			Filename: "main.py",
			Functions: []analyzer.FunctionInfo{
				{Name: "main"},
			},
			Imports: []analyzer.ImportInfo{
				{Module: "services.user_service", Name: "UserService"},
				{Module: "repositories.user_repository", Name: "UserRepository"},
			},
		},
	}

	graph := BuildGraph(files)
	require.NotNil(t, graph)

	// Check nodes
	assert.Len(t, graph.Nodes, 5)
	nodeNames := make(map[string]bool)
	for _, n := range graph.Nodes {
		nodeNames[n.Name] = true
	}
	assert.True(t, nodeNames["models/user"])
	assert.True(t, nodeNames["services/user_service"])
	assert.True(t, nodeNames["repositories/user_repository"])
	assert.True(t, nodeNames["utils/crypto"])
	assert.True(t, nodeNames["main"])

	// Check edges
	assert.Len(t, graph.Edges, 6)

	// Verify specific dependency: user_service -> models/user
	foundUserServiceToModel := false
	for _, e := range graph.Edges {
		if e.From == "services/user_service" && e.To == "models/user" {
			foundUserServiceToModel = true
			assert.Equal(t, "import", e.Type)
		}
	}
	assert.True(t, foundUserServiceToModel, "user_service should depend on models/user")

	// Verify specific dependency: main -> services/user_service
	foundMainToService := false
	for _, e := range graph.Edges {
		if e.From == "main" && e.To == "services/user_service" {
			foundMainToService = true
		}
	}
	assert.True(t, foundMainToService, "main should depend on services/user_service")
}

func TestBuildGraphWithAbsoluteImports(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "app.py",
			Imports: []analyzer.ImportInfo{
				{Module: "fastapi", Name: "FastAPI"},
				{Module: "sqlalchemy.orm", Name: "Session"},
			},
		},
	}

	graph := BuildGraph(files)
	require.NotNil(t, graph)

	// External dependencies should be filtered out (only project-internal edges)
	assert.Len(t, graph.Edges, 0)
}

func TestBuildGraphWithGoStyleAbsoluteImports(t *testing.T) {
	// Go imports are fully-qualified paths with "/" (e.g. "github.com/user/proj/internal/foo").
	// They must not be corrupted by dot→slash replacement, and the suffix-matching
	// logic must resolve them to internal module names.
	files := []*analyzer.FileResult{
		{
			Filename: "internal/cli/cli.go",
			Imports: []analyzer.ImportInfo{
				{Module: "github.com/splitsword/fine-codewiki/internal/analyzer"},
				{Module: "github.com/splitsword/fine-codewiki/internal/grapher"},
				{Module: "fmt"}, // stdlib, should be ignored (not an internal module)
			},
		},
		{
			Filename: "internal/analyzer/analyzer.go",
			Imports: []analyzer.ImportInfo{
				{Module: "github.com/splitsword/fine-codewiki/internal/grapher"},
			},
		},
		{
			Filename: "internal/grapher/grapher.go",
			Imports: []analyzer.ImportInfo{
				{Module: "github.com/splitsword/fine-codewiki/internal/analyzer"},
			},
		},
	}

	graph := BuildGraph(files)
	require.NotNil(t, graph)
	assert.Len(t, graph.Nodes, 3)

	// cli → analyzer, cli → grapher, analyzer ↔ grapher → 4 edges
	assert.Len(t, graph.Edges, 4)
	assert.True(t, hasEdge(graph.Edges, "internal/cli/cli", "internal/analyzer/analyzer"))
	assert.True(t, hasEdge(graph.Edges, "internal/cli/cli", "internal/grapher/grapher"))
	assert.True(t, hasEdge(graph.Edges, "internal/analyzer/analyzer", "internal/grapher/grapher"))
	assert.True(t, hasEdge(graph.Edges, "internal/grapher/grapher", "internal/analyzer/analyzer"))
}

func TestBuildGraphWithNoImports(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "utils/logger.py",
			Functions: []analyzer.FunctionInfo{
				{Name: "get_logger"},
			},
		},
	}

	graph := BuildGraph(files)
	require.NotNil(t, graph)
	assert.Len(t, graph.Nodes, 1)
	assert.Len(t, graph.Edges, 0)
}

func TestBuildGraphEmptyInput(t *testing.T) {
	graph := BuildGraph([]*analyzer.FileResult{})
	require.NotNil(t, graph)
	assert.Len(t, graph.Nodes, 0)
	assert.Len(t, graph.Edges, 0)
}

func TestGroupByDirectory(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "models/user.py"},
		{Filename: "models/base.py"},
		{Filename: "services/user_service.py"},
		{Filename: "utils/crypto.py"},
	}

	graph := BuildGraph(files)
	groups := graph.GroupByDirectory()

	assert.Len(t, groups, 3)
	assert.Len(t, groups["models"], 2)
	assert.Len(t, groups["services"], 1)
	assert.Len(t, groups["utils"], 1)
}

func TestDetectEntryPoints(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "main.py",
			Functions: []analyzer.FunctionInfo{{Name: "main"}},
			Imports: []analyzer.ImportInfo{
				{Module: "services.api", Name: "Api"},
			},
		},
		{
			Filename: "services/api.py",
			Imports: []analyzer.ImportInfo{
				{Module: "models.user", Name: "User"},
			},
		},
		{
			Filename: "models/user.py",
		},
	}

	graph := BuildGraph(files)
	entryPoints := graph.EntryPoints()

	assert.Len(t, entryPoints, 1)
	assert.Equal(t, "main", entryPoints[0].Name)
}

func TestModuleNameFromFilename(t *testing.T) {
	assert.Equal(t, "models/user", moduleNameFromFilename("models/user.py"))
	assert.Equal(t, "services/api", moduleNameFromFilename("services/api.py"))
	assert.Equal(t, "main", moduleNameFromFilename("main.py"))
	assert.Equal(t, "src/components/UserCard", moduleNameFromFilename("src/components/UserCard.tsx"))
}

func TestResolveRelativeImport(t *testing.T) {
	tests := []struct {
		fromFile string
		module   string
		expected string
	}{
		{"models/user.py", ".base", "models/base"},
		{"models/user.py", "..utils.crypto", "utils/crypto"},
		{"services/user_service.py", "..models.user", "models/user"},
		{"app.py", ".config", "config"},
	}

	for _, tt := range tests {
		result := resolveRelativeImport(tt.fromFile, tt.module)
		assert.Equal(t, tt.expected, result, "from %s import %s", tt.fromFile, tt.module)
	}
}

func TestDependenciesOf(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "services/user_service.py",
			Imports: []analyzer.ImportInfo{
				{Module: "..models.user", Name: "User", IsRelative: true},
				{Module: "..utils.logger", Name: "get_logger", IsRelative: true},
			},
		},
		{Filename: "models/user.py"},
		{Filename: "utils/logger.py"},
	}

	graph := BuildGraph(files)
	deps := graph.DependenciesOf("services/user_service")

	assert.Len(t, deps, 2)
	assert.Contains(t, deps, "models/user")
	assert.Contains(t, deps, "utils/logger")
}

func TestDependentsOf(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "services/user_service.py",
			Imports: []analyzer.ImportInfo{
				{Module: "..models.user", Name: "User", IsRelative: true},
			},
		},
		{Filename: "models/user.py"},
	}

	graph := BuildGraph(files)
	deps := graph.DependentsOf("models/user")

	assert.Len(t, deps, 1)
	assert.Equal(t, "services/user_service", deps[0])
}

func TestBuildGraphWithCircularDependencies(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "models/user.py",
			Imports: []analyzer.ImportInfo{
				{Module: ".order", Name: "Order", IsRelative: true},
			},
		},
		{
			Filename: "models/order.py",
			Imports: []analyzer.ImportInfo{
				{Module: ".user", Name: "User", IsRelative: true},
			},
		},
	}

	graph := BuildGraph(files)
	require.NotNil(t, graph)

	// Both nodes should exist
	assert.Len(t, graph.Nodes, 2)
	// Both edges should exist (circular dependency preserved)
	assert.Len(t, graph.Edges, 2)
	assert.True(t, hasEdge(graph.Edges, "models/user", "models/order"))
	assert.True(t, hasEdge(graph.Edges, "models/order", "models/user"))

	// Detect cycles
	cycles := graph.DetectCycles()
	assert.NotEmpty(t, cycles, "should detect circular dependency")
}

func TestDetectCyclesNoCycles(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "main.py",
			Imports: []analyzer.ImportInfo{{Module: "models.user", Name: "User"}},
		},
		{Filename: "models/user.py"},
	}

	graph := BuildGraph(files)
	cycles := graph.DetectCycles()
	assert.Empty(t, cycles)
}

func TestDetectCommunities(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "models/user.py",
			Imports:  []analyzer.ImportInfo{{Module: ".order", Name: "Order", IsRelative: true}},
		},
		{
			Filename: "models/order.py",
			Imports:  []analyzer.ImportInfo{{Module: ".user", Name: "User", IsRelative: true}},
		},
		{
			Filename: "services/api.py",
			Imports: []analyzer.ImportInfo{
				{Module: "..models.user", Name: "User", IsRelative: true},
				{Module: "..models.order", Name: "Order", IsRelative: true},
			},
		},
		{
			Filename:  "utils/logger.py",
			Functions: []analyzer.FunctionInfo{{Name: "get_logger"}},
		},
	}

	graph := BuildGraph(files)
	communities := graph.DetectCommunities()

	// models/user, models/order, and services/api are tightly connected
	// utils/logger is isolated
	assert.GreaterOrEqual(t, len(communities), 2, "should find at least 2 communities")

	// Find isolated node community
	var isolatedCommunity *Cycle
	_ = isolatedCommunity
	foundLogger := false
	for _, nodes := range communities {
		for _, n := range nodes {
			if n.Name == "utils/logger" {
				foundLogger = true
				assert.Len(t, nodes, 1, "isolated node should be in its own community")
			}
		}
	}
	assert.True(t, foundLogger, "logger should be in its own community")
}

func TestDetectCommunitiesDisconnected(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "a.py", Imports: []analyzer.ImportInfo{{Module: ".b", Name: "B", IsRelative: true}}},
		{Filename: "b.py", Imports: []analyzer.ImportInfo{{Module: ".a", Name: "A", IsRelative: true}}},
		{Filename: "c.py", Imports: []analyzer.ImportInfo{{Module: ".d", Name: "D", IsRelative: true}}},
		{Filename: "d.py", Imports: []analyzer.ImportInfo{{Module: ".c", Name: "C", IsRelative: true}}},
	}

	graph := BuildGraph(files)
	communities := graph.DetectCommunities()

	// Should find 2 separate communities (a-b and c-d)
	assert.Equal(t, 2, len(communities))
}

func TestPageRank(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "main.py", Imports: []analyzer.ImportInfo{
			{Module: "services.api", Name: "Api"},
			{Module: "services.user", Name: "UserService"},
		}},
		{Filename: "services/api.py", Imports: []analyzer.ImportInfo{
			{Module: "models.user", Name: "User"},
			{Module: "utils.logger", Name: "get_logger"},
		}},
		{Filename: "services/user.py", Imports: []analyzer.ImportInfo{
			{Module: "models.user", Name: "User"},
			{Module: "utils.logger", Name: "get_logger"},
		}},
		{Filename: "models/user.py"},
		{Filename: "utils/logger.py"},
	}

	graph := BuildGraph(files)
	scores := graph.PageRank()

	require.Len(t, scores, 5)

	// models/user and utils/logger are symmetric (both depended upon by
	// services/api and services/user), so they must have equal PageRank.
	assert.Equal(t, scores["models/user"], scores["utils/logger"], "symmetric nodes should have equal rank")

	// Both models/user and utils/logger receive incoming links, so they
	// should outrank main which has no incoming edges.
	assert.Greater(t, scores["models/user"], scores["main"], "linked nodes should outrank entry point")
	assert.Greater(t, scores["utils/logger"], scores["main"], "linked nodes should outrank entry point")
}

func TestPageRankEmptyGraph(t *testing.T) {
	graph := BuildGraph([]*analyzer.FileResult{})
	scores := graph.PageRank()
	assert.Empty(t, scores)
}

func TestPageRankSingleNode(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "app.py"},
	}
	graph := BuildGraph(files)
	scores := graph.PageRank()
	assert.Len(t, scores, 1)
	assert.Contains(t, scores, "app")
	assert.Greater(t, scores["app"], 0.0)
}

func TestInferModuleRoles(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "main.py", Imports: []analyzer.ImportInfo{
			{Module: "services.api", Name: "Api"},
		}},
		{Filename: "services/api.py", Imports: []analyzer.ImportInfo{
			{Module: "models.user", Name: "User"},
			{Module: "utils.logger", Name: "get_logger"},
		}},
		{Filename: "models/user.py"},
		{Filename: "utils/logger.py"},
	}

	graph := BuildGraph(files)
	roles := graph.InferModuleRoles()

	require.Len(t, roles, 4)

	roleMap := make(map[string]string)
	for _, r := range roles {
		roleMap[r.Name] = r.Role
	}

	// main has no dependents but depends on others -> entry point
	assert.Equal(t, "入口层", roleMap["main"])

	// In this small symmetric 4-node graph, models/user and utils/logger
	// both have the same PageRank and structure, so they fall into the
	// default "supporting module" bucket rather than core/utility.
	assert.Equal(t, "支撑模块", roleMap["models/user"])
	assert.Equal(t, "支撑模块", roleMap["utils/logger"])
}

func TestInferModuleRolesDisconnected(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "a.py"},
		{Filename: "b.py"},
	}
	graph := BuildGraph(files)
	roles := graph.InferModuleRoles()
	require.Len(t, roles, 2)
	for _, r := range roles {
		assert.Equal(t, "独立模块", r.Role, "%s should be isolated", r.Name)
	}
}

func makeTestGraph() *Graph {
	files := []*analyzer.FileResult{
		{
			Filename: "models/user.py",
			Classes:  []analyzer.ClassInfo{{Name: "User"}, {Name: "UserProfile"}},
			Imports:  []analyzer.ImportInfo{{Module: ".base", Name: "BaseModel", IsRelative: true}},
		},
		{
			Filename: "models/base.py",
			Classes:  []analyzer.ClassInfo{{Name: "BaseModel"}},
		},
		{
			Filename: "services/user_service.py",
			Classes:  []analyzer.ClassInfo{{Name: "UserService"}},
			Imports:  []analyzer.ImportInfo{
				{Module: "..models.user", Name: "User", IsRelative: true},
				{Module: "..utils.crypto", Name: "hash_password", IsRelative: true},
			},
		},
		{
			Filename: "utils/crypto.py",
			Functions: []analyzer.FunctionInfo{{Name: "hash_password"}, {Name: "verify_password"}},
		},
		{
			Filename: "main.py",
			Functions: []analyzer.FunctionInfo{{Name: "main"}},
			Imports:  []analyzer.ImportInfo{{Module: "services.user_service", Name: "UserService"}},
		},
	}
	return BuildGraph(files)
}

func TestSubGraphForNodes(t *testing.T) {
	g := makeTestGraph()
	sub := g.SubGraphForNodes([]string{"models/user", "models/base"})
	require.NotNil(t, sub)
	assert.Len(t, sub.Nodes, 2)
	assert.Len(t, sub.Edges, 1) // models/user imports models/base
}

func TestSubGraphForNodesEmpty(t *testing.T) {
	g := makeTestGraph()
	sub := g.SubGraphForNodes([]string{"nonexistent"})
	require.NotNil(t, sub)
	assert.Empty(t, sub.Nodes)
	assert.Empty(t, sub.Edges)
}

func TestSubGraphForDirectory(t *testing.T) {
	g := makeTestGraph()
	sub := g.SubGraphForDirectory("models")
	require.NotNil(t, sub)
	assert.Len(t, sub.Nodes, 2) // models/user, models/base
}

func TestSubGraphForDirectoryNoMatch(t *testing.T) {
	g := makeTestGraph()
	sub := g.SubGraphForDirectory("nonexistent")
	require.NotNil(t, sub)
	assert.Empty(t, sub.Nodes)
}

func TestNeighborSubGraph(t *testing.T) {
	g := makeTestGraph()
	sub := g.NeighborSubGraph("services/user_service")
	require.NotNil(t, sub)
	// services/user_service depends on models/user and utils/crypto
	// main depends on services/user_service
	// So 1-hop neighbor includes user_service itself + user + crypto + main = 4
	assert.Len(t, sub.Nodes, 4)
}

func TestNeighborSubGraphIsolated(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "a.py"},
		{Filename: "b.py"},
	}
	g := BuildGraph(files)
	sub := g.NeighborSubGraph("a")
	require.NotNil(t, sub)
	assert.Len(t, sub.Nodes, 1) // only a itself
}

func TestTopLevelGraph(t *testing.T) {
	g := makeTestGraph()
	top := g.TopLevelGraph()
	require.NotNil(t, top)
	// models, services, utils
	assert.GreaterOrEqual(t, len(top.Nodes), 3)
	// Cross-directory edges: services->models, services->utils
	assert.GreaterOrEqual(t, len(top.Edges), 1)
}

func TestTopLevelGraphSingleDir(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "pkg/a.py", Imports: []analyzer.ImportInfo{{Module: "pkg.b", Name: "B"}}},
		{Filename: "pkg/b.py"},
	}
	g := BuildGraph(files)
	top := g.TopLevelGraph()
	require.NotNil(t, top)
	assert.Len(t, top.Nodes, 1) // only "pkg" dir
	assert.Empty(t, top.Edges)  // intra-directory edges excluded
}

func TestClassesForDirectory(t *testing.T) {
	g := makeTestGraph()
	classes := g.ClassesForDirectory("models")
	assert.Len(t, classes, 3) // User + UserProfile + BaseModel
}

func TestClassesForDirectoryEmpty(t *testing.T) {
	g := makeTestGraph()
	classes := g.ClassesForDirectory("nonexistent")
	assert.Empty(t, classes)
}

func TestNodeByName(t *testing.T) {
	g := makeTestGraph()
	n := g.NodeByName("models/user")
	require.NotNil(t, n)
	assert.Equal(t, "models/user.py", n.Filename)
}

func TestNodeByNameNotFound(t *testing.T) {
	g := makeTestGraph()
	n := g.NodeByName("nonexistent")
	assert.Nil(t, n)
}

func TestBuildGraphLargeGraph(t *testing.T) {
	var files []*analyzer.FileResult
	// Create 100 nodes in a chain: module_0 -> module_1 -> ... -> module_99
	for i := 0; i < 100; i++ {
		f := &analyzer.FileResult{
			Filename: fmt.Sprintf("modules/module_%d.py", i),
		}
		if i < 99 {
			f.Imports = []analyzer.ImportInfo{
				{Module: fmt.Sprintf("..modules.module_%d", i+1), Name: fmt.Sprintf("Mod%d", i+1), IsRelative: true},
			}
		}
		files = append(files, f)
	}

	graph := BuildGraph(files)
	require.NotNil(t, graph)
	assert.Len(t, graph.Nodes, 100)
	assert.Len(t, graph.Edges, 99)

	// Verify chain integrity
	for i := 0; i < 99; i++ {
		from := fmt.Sprintf("modules/module_%d", i)
		to := fmt.Sprintf("modules/module_%d", i+1)
		assert.True(t, hasEdge(graph.Edges, from, to), "expected edge %s -> %s", from, to)
	}
}
