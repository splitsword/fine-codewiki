package grapher

import (
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
