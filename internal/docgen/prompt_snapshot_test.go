package docgen

import (
	"path/filepath"
	"testing"

	"github.com/splitsword/fine-codewiki/internal/analyzer"
	"github.com/splitsword/fine-codewiki/internal/grapher"
	"github.com/splitsword/fine-codewiki/internal/testutil"
)

// TestOverviewPromptSnapshot snapshots the overview prompt to detect
// unintended prompt changes. Run with -update to refresh after intentional edits.
func TestOverviewPromptSnapshot(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "main.py", Functions: []analyzer.FunctionInfo{{Name: "main"}}, Imports: []analyzer.ImportInfo{
			{Module: "services.api", Name: "Api"},
		}},
		{Filename: "services/api.py", Classes: []analyzer.ClassInfo{{Name: "Api"}}, Imports: []analyzer.ImportInfo{
			{Module: "models.user", Name: "User"},
			{Module: "utils.logger", Name: "get_logger"},
		}},
		{Filename: "models/user.py", Classes: []analyzer.ClassInfo{{Name: "User"}}},
		{Filename: "utils/logger.py", Functions: []analyzer.FunctionInfo{{Name: "get_logger"}}},
	}
	graph := grapher.BuildGraph(files)

	prompt := buildOverviewPrompt(graph, "demo-app", "", "")
	testutil.SnapshotCompare(t, prompt, filepath.Join("..", "..", "testdata", "expected", "prompts", "overview.txt"))

	promptWithReadme := buildOverviewPrompt(graph, "demo-app", "# Demo\nThis is a demo project.", "")
	testutil.SnapshotCompare(t, promptWithReadme, filepath.Join("..", "..", "testdata", "expected", "prompts", "overview-with-readme.txt"))
}

// TestArchitecturePromptSnapshot snapshots the architecture prompt to detect
// unintended prompt changes. Run with -update to refresh after intentional edits.
func TestArchitecturePromptSnapshot(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "main.py", Functions: []analyzer.FunctionInfo{{Name: "main"}}, Imports: []analyzer.ImportInfo{
			{Module: "services.api", Name: "Api"},
		}},
		{Filename: "services/api.py", Classes: []analyzer.ClassInfo{{Name: "Api"}}, Imports: []analyzer.ImportInfo{
			{Module: "models.user", Name: "User"},
			{Module: "utils.logger", Name: "get_logger"},
		}},
		{Filename: "models/user.py", Classes: []analyzer.ClassInfo{{Name: "User"}}},
		{Filename: "utils/logger.py", Functions: []analyzer.FunctionInfo{{Name: "get_logger"}}},
	}
	graph := grapher.BuildGraph(files)

	prompt := buildArchitecturePrompt(graph, "demo-app", "", "")
	testutil.SnapshotCompare(t, prompt, filepath.Join("..", "..", "testdata", "expected", "prompts", "architecture.txt"))
}
