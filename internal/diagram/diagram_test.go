package diagram

import (
	"strings"
	"testing"

	"github.com/splitsword/fine-codewiki/internal/analyzer"
	"github.com/splitsword/fine-codewiki/internal/grapher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateArchitectureDiagram(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "models/user.py",
			Classes: []analyzer.ClassInfo{
				{Name: "User", Bases: []string{"BaseModel"}},
			},
			Imports: []analyzer.ImportInfo{
				{Module: ".base", IsRelative: true},
				{Module: "..utils.crypto", IsRelative: true},
			},
		},
		{
			Filename: "models/base.py",
			Classes:  []analyzer.ClassInfo{{Name: "BaseModel"}},
		},
		{
			Filename: "services/user_service.py",
			Classes:  []analyzer.ClassInfo{{Name: "UserService"}},
			Imports: []analyzer.ImportInfo{
				{Module: "..models.user", IsRelative: true},
				{Module: "..repositories.user_repository", IsRelative: true},
				{Module: "..utils.logger", IsRelative: true},
			},
		},
		{
			Filename: "repositories/user_repository.py",
			Classes:  []analyzer.ClassInfo{{Name: "UserRepository"}},
			Imports: []analyzer.ImportInfo{
				{Module: "..models.user", IsRelative: true},
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
		{
			Filename: "main.py",
			Functions: []analyzer.FunctionInfo{{Name: "main"}},
			Imports: []analyzer.ImportInfo{
				{Module: "services.user_service", IsRelative: false},
				{Module: "repositories.user_repository", IsRelative: false},
			},
		},
	}

	graph := grapher.BuildGraph(files)
	dsl, err := GenerateArchitectureDiagram(graph)
	require.NoError(t, err)
	require.NotEmpty(t, dsl)

	// Must be valid Mermaid graph TD
	assert.True(t, strings.HasPrefix(dsl, "graph TD"), "should start with 'graph TD'")

	// Should contain subgraphs for directories
	assert.Contains(t, dsl, "subgraph models")
	assert.Contains(t, dsl, "subgraph services")
	assert.Contains(t, dsl, "subgraph repositories")
	assert.Contains(t, dsl, "subgraph utils")

	// Should contain nodes
	assert.Contains(t, dsl, "models/user")
	assert.Contains(t, dsl, "models/base")
	assert.Contains(t, dsl, "services/user_service")
	assert.Contains(t, dsl, "repositories/user_repository")
	assert.Contains(t, dsl, "utils/crypto")
	assert.Contains(t, dsl, "utils/logger")
	assert.Contains(t, dsl, "main")

	// Should contain edges (using escaped node IDs)
	assert.Contains(t, dsl, "models_user --> models_base")
	assert.Contains(t, dsl, "services_user_service --> models_user")
	assert.Contains(t, dsl, "main --> services_user_service")
}

func TestGenerateArchitectureDiagramEmpty(t *testing.T) {
	graph := grapher.BuildGraph([]*analyzer.FileResult{})
	dsl, err := GenerateArchitectureDiagram(graph)
	require.NoError(t, err)
	assert.Equal(t, "graph TD\n", dsl)
}

func TestGenerateClassDiagram(t *testing.T) {
	files := []*analyzer.FileResult{
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
		},
	}

	graph := grapher.BuildGraph(files)
	dsl, err := GenerateClassDiagram(graph)
	require.NoError(t, err)
	require.NotEmpty(t, dsl)

	// Must be valid Mermaid classDiagram
	assert.True(t, strings.HasPrefix(dsl, "classDiagram"), "should start with 'classDiagram'")

	// Should contain class definitions
	assert.Contains(t, dsl, "class BaseModel")
	assert.Contains(t, dsl, "class User")
	assert.Contains(t, dsl, "class UserService")

	// Should contain methods
	assert.Contains(t, dsl, "+to_dict()")
	assert.Contains(t, dsl, "+authenticate(password)")
	assert.Contains(t, dsl, "+register(username, email, password)")

	// Should contain inheritance
	assert.Contains(t, dsl, "User --|> BaseModel")
}

func TestGenerateClassDiagramNoClasses(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "utils/logger.py", Functions: []analyzer.FunctionInfo{{Name: "get_logger"}}},
	}
	graph := grapher.BuildGraph(files)
	dsl, err := GenerateClassDiagram(graph)
	require.NoError(t, err)
	assert.Equal(t, "classDiagram\n", dsl)
}

func TestGenerateClassDiagramWithMultipleInheritance(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "models/mixins.py",
			Classes: []analyzer.ClassInfo{
				{Name: "TimestampMixin"},
				{Name: "SerializableMixin"},
			},
		},
		{
			Filename: "models/user.py",
			Classes: []analyzer.ClassInfo{
				{
					Name:  "AdminUser",
					Bases: []string{"User", "TimestampMixin", "SerializableMixin"},
				},
			},
		},
	}

	graph := grapher.BuildGraph(files)
	dsl, err := GenerateClassDiagram(graph)
	require.NoError(t, err)
	assert.Contains(t, dsl, "AdminUser --|> User")
	assert.Contains(t, dsl, "AdminUser --|> TimestampMixin")
	assert.Contains(t, dsl, "AdminUser --|> SerializableMixin")
}

func TestMermaidEscape(t *testing.T) {
	// Mermaid identifiers cannot contain certain characters
	assert.Equal(t, "foo_bar", mermaidEscape("foo-bar"))
	assert.Equal(t, "foo_bar", mermaidEscape("foo bar"))
	assert.Equal(t, "foo_bar", mermaidEscape("foo:bar"))
	assert.Equal(t, "foo_bar", mermaidEscape("foo/bar"))
	assert.Equal(t, "foo__bar", mermaidEscape("foo--bar"))
}
