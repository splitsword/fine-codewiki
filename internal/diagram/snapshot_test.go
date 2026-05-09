package diagram

import (
	"path/filepath"
	"testing"

	"github.com/splitsword/fine-codewiki/internal/analyzer"
	"github.com/splitsword/fine-codewiki/internal/grapher"
	"github.com/splitsword/fine-codewiki/internal/testutil"
	"github.com/stretchr/testify/require"
)

// TestPythonBasicDiagramSnapshots generates architecture, class, and dependency
// diagrams for the python-basic test repo and compares them against stored
// snapshots. Run with -update to refresh snapshots after intentional changes.
func TestPythonBasicDiagramSnapshots(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "main.py",
			Functions: []analyzer.FunctionInfo{{Name: "main"}},
			Imports: []analyzer.ImportInfo{
				{Module: "services.user_service", Name: "UserService"},
				{Module: "repositories.user_repository", Name: "UserRepository"},
			},
		},
		{
			Filename: "models/user.py",
			Classes: []analyzer.ClassInfo{
				{
					Name:    "User",
					Bases:   []string{"BaseModel"},
					Methods: []analyzer.FunctionInfo{
						{Name: "create", Params: []string{"cls", "username", "email", "password"}, ReturnType: "User"},
						{Name: "authenticate", Params: []string{"self", "password"}, ReturnType: "bool"},
						{Name: "deactivate", Params: []string{"self"}, ReturnType: "None"},
					},
				},
			},
			Imports: []analyzer.ImportInfo{
				{Module: ".base", Name: "BaseModel", IsRelative: true},
				{Module: "..utils.crypto", Name: "hash_password", IsRelative: true},
			},
		},
		{
			Filename: "models/base.py",
			Classes: []analyzer.ClassInfo{
				{
					Name: "BaseModel",
					Methods: []analyzer.FunctionInfo{
						{Name: "to_dict", ReturnType: "Dict[str, Any]"},
						{Name: "validate", ReturnType: "bool"},
					},
				},
			},
		},
		{
			Filename: "services/user_service.py",
			Classes: []analyzer.ClassInfo{
				{
					Name: "UserService",
					Methods: []analyzer.FunctionInfo{
						{Name: "__init__", Params: []string{"self", "repository"}},
						{Name: "register", Params: []string{"self", "username", "email", "password"}, ReturnType: "User"},
						{Name: "authenticate", Params: []string{"self", "username", "password"}, ReturnType: "Optional[User]"},
						{Name: "list_users", Params: []string{"self"}, ReturnType: "List[User]"},
					},
				},
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
				{
					Name: "UserRepository",
					Methods: []analyzer.FunctionInfo{
						{Name: "save", Params: []string{"self", "user"}, ReturnType: "None"},
						{Name: "find_by_id", Params: []string{"self", "user_id"}, ReturnType: "Optional[User]"},
						{Name: "find_by_username", Params: []string{"self", "username"}, ReturnType: "Optional[User]"},
						{Name: "find_all", Params: []string{"self"}, ReturnType: "List[User]"},
					},
				},
			},
			Imports: []analyzer.ImportInfo{
				{Module: "..models.user", Name: "User", IsRelative: true},
			},
		},
		{
			Filename:  "utils/crypto.py",
			Functions: []analyzer.FunctionInfo{{Name: "hash_password"}, {Name: "verify_password"}},
		},
		{
			Filename:  "utils/logger.py",
			Functions: []analyzer.FunctionInfo{{Name: "get_logger"}},
		},
	}

	graph := grapher.BuildGraph(files)

	archDSL, err := GenerateArchitectureDiagram(graph)
	require.NoError(t, err)
	testutil.SnapshotCompare(t, archDSL, filepath.Join("..", "..", "testdata", "expected", "diagrams", "python-basic", "architecture.mmd"))

	classDSL, err := GenerateClassDiagram(graph)
	require.NoError(t, err)
	testutil.SnapshotCompare(t, classDSL, filepath.Join("..", "..", "testdata", "expected", "diagrams", "python-basic", "class-diagram.mmd"))

	depDSL, err := GenerateDependencyDiagram(graph)
	require.NoError(t, err)
	testutil.SnapshotCompare(t, depDSL, filepath.Join("..", "..", "testdata", "expected", "diagrams", "python-basic", "dependency.mmd"))
}
