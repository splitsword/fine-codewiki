package docgen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/splitsword/fine-codewiki/internal/analyzer"
	"github.com/splitsword/fine-codewiki/internal/grapher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateOverviewMarkdown(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "models/user.py", Classes: []analyzer.ClassInfo{{Name: "User"}}},
		{Filename: "services/user_service.py", Classes: []analyzer.ClassInfo{{Name: "UserService"}}},
		{Filename: "utils/logger.py", Functions: []analyzer.FunctionInfo{{Name: "get_logger"}}},
	}
	graph := grapher.BuildGraph(files)

	md, err := GenerateOverviewMarkdown(graph, "python-basic")
	require.NoError(t, err)
	require.NotEmpty(t, md)

	// Should contain project name
	assert.Contains(t, md, "# python-basic")

	// Should contain stats
	assert.Contains(t, md, "Modules")
	assert.Contains(t, md, "Classes")
	assert.Contains(t, md, "Functions")

	// Should contain module list
	assert.Contains(t, md, "## Modules")

	// Should contain module list
	assert.Contains(t, md, "models/user")
	assert.Contains(t, md, "services/user_service")
	assert.Contains(t, md, "utils/logger")
}

func TestGenerateOverviewMarkdownEmptyProject(t *testing.T) {
	graph := grapher.BuildGraph([]*analyzer.FileResult{})
	md, err := GenerateOverviewMarkdown(graph, "empty-project")
	require.NoError(t, err)
	assert.Contains(t, md, "# empty-project")
	assert.Contains(t, md, "No modules found")
}

func TestGenerateAPIReferenceMarkdown(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "models/user.py",
			Classes: []analyzer.ClassInfo{
				{
					Name:    "User",
					Bases:   []string{"BaseModel"},
					Methods: []analyzer.FunctionInfo{{Name: "authenticate", Params: []string{"self", "password"}, ReturnType: "bool"}},
				},
			},
		},
		{
			Filename:  "utils/logger.py",
			Functions: []analyzer.FunctionInfo{{Name: "get_logger", Params: []string{"name"}, ReturnType: "Logger"}},
		},
	}
	graph := grapher.BuildGraph(files)

	md, err := GenerateAPIReferenceMarkdown(graph)
	require.NoError(t, err)
	require.NotEmpty(t, md)

	// Should contain API reference heading
	assert.Contains(t, md, "# API Reference")

	// Should contain class documentation
	assert.Contains(t, md, "## User")
	assert.Contains(t, md, "BaseModel")
	assert.Contains(t, md, "authenticate")

	// Should contain function documentation
	assert.Contains(t, md, "## get_logger")
	assert.Contains(t, md, "name")
	assert.Contains(t, md, "Logger")
}

func TestGenerateArchitectureMarkdown(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "models/user.py",
			Imports:  []analyzer.ImportInfo{{Module: ".base", IsRelative: true}},
		},
		{Filename: "models/base.py"},
		{
			Filename: "services/user_service.py",
			Imports: []analyzer.ImportInfo{
				{Module: "..models.user", IsRelative: true},
				{Module: "..utils.logger", IsRelative: true},
			},
		},
		{Filename: "utils/logger.py"},
	}
	graph := grapher.BuildGraph(files)
	archDSL := "graph TD\n    models_user --> models_base\n"

	md, err := GenerateArchitectureMarkdown(graph, archDSL)
	require.NoError(t, err)
	require.NotEmpty(t, md)

	assert.Contains(t, md, "# Architecture")
	assert.Contains(t, md, "## Module Overview")
	assert.Contains(t, md, "## Dependency Graph")
	assert.Contains(t, md, "```mermaid")
	assert.Contains(t, md, archDSL)
	assert.Contains(t, md, "```")
}

func TestWriteWikiFiles(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".codewiki", "wiki")

	wiki := &Wiki{
		ProjectName:   "test-project",
		Overview:      "# test-project\n",
		APIReference:  "# API Reference\n",
		Architecture:  "# Architecture\n",
		ClassDiagram:  "classDiagram\n",
		ArchitectureDiagram: "graph TD\n",
	}

	err := WriteWikiFiles(wikiDir, wiki)
	require.NoError(t, err)

	// Verify files exist
	assert.FileExists(t, filepath.Join(wikiDir, "overview.md"))
	assert.FileExists(t, filepath.Join(wikiDir, "api-reference.md"))
	assert.FileExists(t, filepath.Join(wikiDir, "architecture.md"))
	assert.FileExists(t, filepath.Join(wikiDir, "class-diagram.mmd"))
	assert.FileExists(t, filepath.Join(wikiDir, "architecture.mmd"))

	// Verify content
	content, err := os.ReadFile(filepath.Join(wikiDir, "overview.md"))
	require.NoError(t, err)
	assert.Equal(t, "# test-project\n", string(content))
}

func TestMarkdownEscape(t *testing.T) {
	assert.Equal(t, "foo\\_bar", markdownEscape("foo_bar"))
	assert.Equal(t, "foo\\*bar\\*", markdownEscape("foo*bar*"))
	assert.Equal(t, "foo\\_bar\\_", markdownEscape("foo_bar_"))
}

func TestGenerateWikiFromGraph(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "models/user.py",
			Classes: []analyzer.ClassInfo{
				{Name: "User", Bases: []string{"BaseModel"}},
			},
			Imports: []analyzer.ImportInfo{{Module: ".base", IsRelative: true}},
		},
		{Filename: "models/base.py", Classes: []analyzer.ClassInfo{{Name: "BaseModel"}}},
	}
	graph := grapher.BuildGraph(files)
	archDSL := "graph TD\n    models_user --> models_base\n"
	classDSL := "classDiagram\n    class User\n    class BaseModel\n    User --|> BaseModel\n"

	wiki, err := GenerateWiki(graph, "test-project", archDSL, classDSL)
	require.NoError(t, err)
	require.NotNil(t, wiki)

	assert.Equal(t, "test-project", wiki.ProjectName)
	assert.NotEmpty(t, wiki.Overview)
	assert.NotEmpty(t, wiki.APIReference)
	assert.NotEmpty(t, wiki.Architecture)
	assert.Equal(t, classDSL, wiki.ClassDiagram)
	assert.Equal(t, archDSL, wiki.ArchitectureDiagram)

	// Verify overview contains key info
	assert.Contains(t, wiki.Overview, "test-project")
	assert.Contains(t, wiki.Overview, "models/user")
	assert.Contains(t, wiki.Overview, "models/base")

	// Verify architecture doc contains embedded diagram
	assert.Contains(t, wiki.Architecture, "```mermaid")
	assert.Contains(t, wiki.Architecture, archDSL)
}
