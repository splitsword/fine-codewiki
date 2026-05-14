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

	// Must be valid Mermaid graph TD (after semantic comment)
	assert.Contains(t, dsl, "graph TD")

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

	// Should contain click handlers for interactive navigation
	assert.Contains(t, dsl, "click models_user \"javascript:navigateToModule('models/user')\"")
	assert.Contains(t, dsl, "click main \"javascript:navigateToModule('main')\"")
}

func TestGenerateArchitectureDiagramEmpty(t *testing.T) {
	graph := grapher.BuildGraph([]*analyzer.FileResult{})
	dsl, err := GenerateArchitectureDiagram(graph)
	require.NoError(t, err)
	assert.Contains(t, dsl, "graph TD\n")
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

	// Must be valid Mermaid classDiagram (after semantic comment)
	assert.Contains(t, dsl, "classDiagram")

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
	assert.Contains(t, dsl, "classDiagram\n")
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

func TestGenerateArchitectureDiagramCircularDeps(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "models/user.py",
			Imports:  []analyzer.ImportInfo{{Module: ".order", Name: "Order", IsRelative: true}},
		},
		{
			Filename: "models/order.py",
			Imports:  []analyzer.ImportInfo{{Module: ".user", Name: "User", IsRelative: true}},
		},
	}

	graph := grapher.BuildGraph(files)
	dsl, err := GenerateArchitectureDiagram(graph)
	require.NoError(t, err)

	// Circular dependency edges should use dotted style
	assert.Contains(t, dsl, "models_user -.-> models_order")
	assert.Contains(t, dsl, "models_order -.-> models_user")
}

func TestGenerateArchitectureDiagramStability(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "models/user.py", Imports: []analyzer.ImportInfo{{Module: ".base", Name: "BaseModel", IsRelative: true}}},
		{Filename: "models/base.py"},
		{Filename: "services/api.py", Imports: []analyzer.ImportInfo{{Module: "..models.user", Name: "User", IsRelative: true}}},
	}

	graph := grapher.BuildGraph(files)

	// Generate multiple times and verify identical output
	var outputs []string
	for i := 0; i < 5; i++ {
		dsl, err := GenerateArchitectureDiagram(graph)
		require.NoError(t, err)
		outputs = append(outputs, dsl)
	}

	for i := 1; i < len(outputs); i++ {
		assert.Equal(t, outputs[0], outputs[i], "architecture diagram should be deterministic")
	}

	// Same for class diagram
	var classOutputs []string
	for i := 0; i < 5; i++ {
		dsl, err := GenerateClassDiagram(graph)
		require.NoError(t, err)
		classOutputs = append(classOutputs, dsl)
	}

	for i := 1; i < len(classOutputs); i++ {
		assert.Equal(t, classOutputs[0], classOutputs[i], "class diagram should be deterministic")
	}
}

func TestGenerateClassDiagramEmptyClass(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "models/placeholder.py",
			Classes: []analyzer.ClassInfo{
				{Name: "EmptyClass", Methods: []analyzer.FunctionInfo{}},
			},
		},
	}

	graph := grapher.BuildGraph(files)
	dsl, err := GenerateClassDiagram(graph)
	require.NoError(t, err)

	// Should contain empty class definition
	assert.Contains(t, dsl, "class EmptyClass {")
	assert.Contains(t, dsl, "}")
}

func TestGenerateDependencyDiagram(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "models/user.py",
			Imports:  []analyzer.ImportInfo{{Module: ".base", Name: "BaseModel", IsRelative: true}},
		},
		{Filename: "models/base.py"},
		{
			Filename: "services/api.py",
			Imports: []analyzer.ImportInfo{
				{Module: "..models.user", Name: "User", IsRelative: true},
				{Module: "..models.base", Name: "BaseModel", IsRelative: true},
			},
		},
	}

	graph := grapher.BuildGraph(files)
	dsl, err := GenerateDependencyDiagram(graph)
	require.NoError(t, err)
	require.NotEmpty(t, dsl)

	// Must be valid Mermaid graph LR
	assert.True(t, strings.Contains(dsl, "graph LR"), "should contain 'graph LR'")

	// Should contain node declarations
	assert.Contains(t, dsl, "models_user[models/user]")
	assert.Contains(t, dsl, "models_base[models/base]")
	assert.Contains(t, dsl, "services_api[services/api]")

	// Should contain edges
	assert.Contains(t, dsl, "models_user --> models_base")
	assert.Contains(t, dsl, "services_api --> models_user")
	assert.Contains(t, dsl, "services_api --> models_base")
}

func TestGenerateDependencyDiagramEmpty(t *testing.T) {
	graph := grapher.BuildGraph([]*analyzer.FileResult{})
	dsl, err := GenerateDependencyDiagram(graph)
	require.NoError(t, err)
	assert.Equal(t, "%% 依赖图：展示模块间的完整导入依赖关系\ngraph LR\n", dsl)
}

func TestGenerateDependencyDiagramCircularDeps(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "models/user.py",
			Imports:  []analyzer.ImportInfo{{Module: ".order", Name: "Order", IsRelative: true}},
		},
		{
			Filename: "models/order.py",
			Imports:  []analyzer.ImportInfo{{Module: ".user", Name: "User", IsRelative: true}},
		},
	}

	graph := grapher.BuildGraph(files)
	dsl, err := GenerateDependencyDiagram(graph)
	require.NoError(t, err)

	// Circular dependency edges should use dotted style
	assert.Contains(t, dsl, "models_user -.-> models_order")
	assert.Contains(t, dsl, "models_order -.-> models_user")
}

func TestGenerateDependencyDiagramStability(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "models/user.py", Imports: []analyzer.ImportInfo{{Module: ".base", Name: "BaseModel", IsRelative: true}}},
		{Filename: "models/base.py"},
		{Filename: "services/api.py", Imports: []analyzer.ImportInfo{{Module: "..models.user", Name: "User", IsRelative: true}}},
	}

	graph := grapher.BuildGraph(files)

	var outputs []string
	for i := 0; i < 5; i++ {
		dsl, err := GenerateDependencyDiagram(graph)
		require.NoError(t, err)
		outputs = append(outputs, dsl)
	}

	for i := 1; i < len(outputs); i++ {
		assert.Equal(t, outputs[0], outputs[i], "dependency diagram should be deterministic")
	}
}

func makeDiagramTestGraph() *grapher.Graph {
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
			Imports:  []analyzer.ImportInfo{{Module: "..models.user", Name: "User", IsRelative: true}},
		},
		{Filename: "main.py", Functions: []analyzer.FunctionInfo{{Name: "main"}},
			Imports: []analyzer.ImportInfo{{Module: "services.user_service", Name: "UserService"}}},
	}
	return grapher.BuildGraph(files)
}

func TestGenerateTopLevelDiagram(t *testing.T) {
	g := makeDiagramTestGraph()
	dsl, err := GenerateTopLevelDiagram(g)
	require.NoError(t, err)
	assert.Contains(t, dsl, "graph TD")
	assert.Contains(t, dsl, "%% 顶层架构概览")
}

func TestGenerateTopLevelDiagramEmpty(t *testing.T) {
	dsl, err := GenerateTopLevelDiagram(&grapher.Graph{})
	require.NoError(t, err)
	assert.Empty(t, dsl)
}

func TestGenerateSubArchDiagram(t *testing.T) {
	g := makeDiagramTestGraph()
	sub := g.SubGraphForDirectory("models")
	dsl, err := GenerateSubArchDiagram(sub, "数据模型")
	require.NoError(t, err)
	assert.Contains(t, dsl, "graph TD")
	assert.Contains(t, dsl, "%% 数据模型")
	assert.Contains(t, dsl, "user")
}

func TestGenerateSubArchDiagramEmpty(t *testing.T) {
	dsl, err := GenerateSubArchDiagram(&grapher.Graph{}, "empty")
	require.NoError(t, err)
	assert.Empty(t, dsl)
}

func TestGenerateSubClassDiagram(t *testing.T) {
	classes := []analyzer.ClassInfo{
		{Name: "User", Bases: []string{"BaseModel"}, Methods: []analyzer.FunctionInfo{
			{Name: "__init__", Params: []string{"self", "name"}, ReturnType: ""},
		}},
		{Name: "BaseModel", Methods: []analyzer.FunctionInfo{
			{Name: "save", Params: []string{"self"}, ReturnType: "bool"},
		}},
	}
	dsl, err := GenerateSubClassDiagram(classes, "数据模型")
	require.NoError(t, err)
	assert.Contains(t, dsl, "classDiagram")
	assert.Contains(t, dsl, "%% 数据模型")
	assert.Contains(t, dsl, "User")
	assert.Contains(t, dsl, "BaseModel")
}

func TestGenerateSubClassDiagramEmpty(t *testing.T) {
	dsl, err := GenerateSubClassDiagram(nil, "empty")
	require.NoError(t, err)
	assert.Empty(t, dsl)
}

func TestGenerateSubClassDiagramDedup(t *testing.T) {
	// Same class from different files should be deduplicated
	classes := []analyzer.ClassInfo{
		{Name: "User", Methods: []analyzer.FunctionInfo{{Name: "foo"}}},
		{Name: "User", Methods: []analyzer.FunctionInfo{{Name: "foo"}, {Name: "bar"}}},
	}
	dsl, err := GenerateSubClassDiagram(classes, "test")
	require.NoError(t, err)
	// Should keep the one with more methods
	assert.Contains(t, dsl, "bar")
	// Should only appear once
	assert.Equal(t, 1, strings.Count(dsl, "class User {"))
}
