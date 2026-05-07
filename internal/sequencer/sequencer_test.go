package sequencer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/splitsword/fine-codewiki/internal/analyzer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateSequenceDiagramBasic(t *testing.T) {
	seq := Sequence{
		Title:        "create_user flow",
		Participants: []string{"api/routes", "services/user_service"},
		Messages: []Message{
			{From: "api/routes", To: "services/user_service", Label: "create_user"},
		},
	}

	dsl := GenerateSequenceDiagram(seq)
	assert.Contains(t, dsl, "sequenceDiagram")
	assert.Contains(t, dsl, "participant api_routes as api/routes")
	assert.Contains(t, dsl, "participant services_user_service as services/user_service")
	assert.Contains(t, dsl, "api_routes->>services_user_service: create_user")
}

func TestGenerateSequenceDiagramMultiLevel(t *testing.T) {
	seq := Sequence{
		Title:        "full create flow",
		Participants: []string{"api/routes", "services/user_service", "repositories/user_repository"},
		Messages: []Message{
			{From: "api/routes", To: "services/user_service", Label: "create_user"},
			{From: "services/user_service", To: "repositories/user_repository", Label: "save"},
		},
	}

	dsl := GenerateSequenceDiagram(seq)
	lines := splitLines(dsl)
	require.True(t, len(lines) >= 5)
	assert.Contains(t, dsl, "api/routes")
	assert.Contains(t, dsl, "services/user_service")
	assert.Contains(t, dsl, "repositories/user_repository")
	assert.Contains(t, dsl, "api_routes->>services_user_service: create_user")
	assert.Contains(t, dsl, "services_user_service->>repositories_user_repository: save")
}

func TestGenerateSequenceDiagramEmpty(t *testing.T) {
	seq := Sequence{Title: "empty"}
	dsl := GenerateSequenceDiagram(seq)
	assert.Equal(t, "sequenceDiagram\n", dsl)
}

func TestGenerateSequenceDiagramDeduplication(t *testing.T) {
	seq := Sequence{
		Title:        "dedup test",
		Participants: []string{"a", "b"},
		Messages: []Message{
			{From: "a", To: "b", Label: "foo"},
			{From: "a", To: "b", Label: "foo"}, // duplicate
			{From: "a", To: "b", Label: "bar"},
		},
	}

	dsl := GenerateSequenceDiagram(seq)
	count := 0
	for _, line := range splitLines(dsl) {
		if contains(line, "->>") {
			count++
		}
	}
	assert.Equal(t, 2, count, "duplicate consecutive messages should be deduplicated")
}

func TestFindSequencesFiltersShortChains(t *testing.T) {
	edges := []CallEdge{
		{From: FunctionRef{Module: "a", Name: "f1"}, To: FunctionRef{Module: "b", Name: "f2"}},
	}

	seqs := FindSequences(edges, 2)
	assert.Empty(t, seqs, "single-edge chain should be filtered when minEdges=2")
}

func TestFindSequencesFindsLongChains(t *testing.T) {
	edges := []CallEdge{
		{From: FunctionRef{Module: "a", Name: "f1"}, To: FunctionRef{Module: "b", Name: "f2"}},
		{From: FunctionRef{Module: "b", Name: "f2"}, To: FunctionRef{Module: "c", Name: "f3"}},
	}

	seqs := FindSequences(edges, 2)
	require.Len(t, seqs, 1)
	assert.Equal(t, "a.f1 -> c.f3", seqs[0].Title)
	require.Len(t, seqs[0].Messages, 2)
	assert.Equal(t, "f2", seqs[0].Messages[0].Label)
	assert.Equal(t, "f3", seqs[0].Messages[1].Label)
}

func TestFindSequencesCrossModuleRequirement(t *testing.T) {
	edges := []CallEdge{
		{From: FunctionRef{Module: "a", Name: "f1"}, To: FunctionRef{Module: "a", Name: "f2"}},
		{From: FunctionRef{Module: "a", Name: "f2"}, To: FunctionRef{Module: "a", Name: "f3"}},
	}

	seqs := FindSequences(edges, 2)
	assert.Empty(t, seqs, "intra-module chain should be filtered")
}

func TestFindSequencesNoCycles(t *testing.T) {
	edges := []CallEdge{
		{From: FunctionRef{Module: "a", Name: "f1"}, To: FunctionRef{Module: "b", Name: "f2"}},
		{From: FunctionRef{Module: "b", Name: "f2"}, To: FunctionRef{Module: "a", Name: "f1"}},
	}

	seqs := FindSequences(edges, 2)
	// Should find a->b->a as a valid 2-edge cross-module path
	require.Len(t, seqs, 1)
	assert.Equal(t, "a.f1 -> a.f1", seqs[0].Title)
}

func TestBuildCallGraphSingleCall(t *testing.T) {
	tmpDir := t.TempDir()

	writeFile(t, tmpDir, "module_a.py", `
def foo():
    pass
`)
	writeFile(t, tmpDir, "module_b.py", `
def bar():
    foo()
`)

	files := []*analyzer.FileResult{
		{Filename: "module_a.py", Functions: []analyzer.FunctionInfo{{Name: "foo"}}},
		{Filename: "module_b.py", Functions: []analyzer.FunctionInfo{{Name: "bar"}}},
	}

	edges, err := BuildCallGraph(tmpDir, files)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	assert.Equal(t, "module_b", edges[0].From.Module)
	assert.Equal(t, "bar", edges[0].From.Name)
	assert.Equal(t, "module_a", edges[0].To.Module)
	assert.Equal(t, "foo", edges[0].To.Name)
}

func TestBuildCallGraphMultiLevel(t *testing.T) {
	tmpDir := t.TempDir()

	writeFile(t, tmpDir, "api.py", `
def handle():
    create_user()
`)
	writeFile(t, tmpDir, "service.py", `
def create_user():
    save()
`)
	writeFile(t, tmpDir, "repo.py", `
def save():
    pass
`)

	files := []*analyzer.FileResult{
		{Filename: "api.py", Functions: []analyzer.FunctionInfo{{Name: "handle"}}},
		{Filename: "service.py", Functions: []analyzer.FunctionInfo{{Name: "create_user"}}},
		{Filename: "repo.py", Functions: []analyzer.FunctionInfo{{Name: "save"}}},
	}

	edges, err := BuildCallGraph(tmpDir, files)
	require.NoError(t, err)
	require.Len(t, edges, 2)

	// api.handle -> service.create_user
	found1 := false
	found2 := false
	for _, e := range edges {
		if e.From.Module == "api" && e.To.Module == "service" {
			found1 = true
		}
		if e.From.Module == "service" && e.To.Module == "repo" {
			found2 = true
		}
	}
	assert.True(t, found1, "expected api->service edge")
	assert.True(t, found2, "expected service->repo edge")
}

func TestBuildCallGraphIgnoresSelfDef(t *testing.T) {
	tmpDir := t.TempDir()

	writeFile(t, tmpDir, "module.py", `
def foo(x):
    return x + 1
`)

	files := []*analyzer.FileResult{
		{Filename: "module.py", Functions: []analyzer.FunctionInfo{{Name: "foo"}}},
	}

	edges, err := BuildCallGraph(tmpDir, files)
	require.NoError(t, err)
	assert.Empty(t, edges, "function definition line should not count as a self-call")
}

func TestBuildCallGraphMethodCall(t *testing.T) {
	tmpDir := t.TempDir()

	writeFile(t, tmpDir, "service.py", `
class UserService:
    def create(self):
        self.save()

    def save(self):
        pass
`)

	files := []*analyzer.FileResult{
		{
			Filename: "service.py",
			Classes: []analyzer.ClassInfo{
				{
					Name: "UserService",
					Methods: []analyzer.FunctionInfo{
						{Name: "create"},
						{Name: "save"},
					},
				},
			},
		},
	}

	edges, err := BuildCallGraph(tmpDir, files)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	assert.Equal(t, "service", edges[0].From.Module)
	assert.Equal(t, "UserService.create", edges[0].From.Name)
	assert.Equal(t, "service", edges[0].To.Module)
	assert.Equal(t, "UserService.save", edges[0].To.Name)
}

func TestBuildCallGraphSkipsComments(t *testing.T) {
	tmpDir := t.TempDir()

	writeFile(t, tmpDir, "a.py", `
def helper():
    pass
`)
	writeFile(t, tmpDir, "b.py", `
def work():
    # helper() - commented out
    pass
`)

	files := []*analyzer.FileResult{
		{Filename: "a.py", Functions: []analyzer.FunctionInfo{{Name: "helper"}}},
		{Filename: "b.py", Functions: []analyzer.FunctionInfo{{Name: "work"}}},
	}

	edges, err := BuildCallGraph(tmpDir, files)
	require.NoError(t, err)
	assert.Empty(t, edges, "calls in full-line comments should be skipped")
}

func TestBuildCallGraphRecursive(t *testing.T) {
	tmpDir := t.TempDir()

	writeFile(t, tmpDir, "module.py", `
def factorial(n):
    if n <= 1:
        return 1
    return n * factorial(n - 1)
`)

	files := []*analyzer.FileResult{
		{Filename: "module.py", Functions: []analyzer.FunctionInfo{{Name: "factorial"}}},
	}

	edges, err := BuildCallGraph(tmpDir, files)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	assert.Equal(t, "factorial", edges[0].From.Name)
	assert.Equal(t, "factorial", edges[0].To.Name)
}

func TestPathToSequence(t *testing.T) {
	path := []CallEdge{
		{From: FunctionRef{Module: "a", Name: "f1"}, To: FunctionRef{Module: "b", Name: "f2"}},
		{From: FunctionRef{Module: "b", Name: "f2"}, To: FunctionRef{Module: "c", Name: "f3"}},
	}

	seq := pathToSequence(path)
	assert.Equal(t, "a.f1 -> c.f3", seq.Title)
	assert.Equal(t, []string{"a", "b", "c"}, seq.Participants)
	require.Len(t, seq.Messages, 2)
	assert.Equal(t, Message{From: "a", To: "b", Label: "f2"}, seq.Messages[0])
	assert.Equal(t, Message{From: "b", To: "c", Label: "f3"}, seq.Messages[1])
}

func TestBuildCallGraphGo(t *testing.T) {
	tmpDir := t.TempDir()

	writeFile(t, tmpDir, "main.go", `
package main

func helper() {}

func main() {
	helper()
}
`)

	files := []*analyzer.FileResult{
		{Filename: "main.go", Functions: []analyzer.FunctionInfo{{Name: "helper"}, {Name: "main"}}},
	}

	edges, err := BuildCallGraph(tmpDir, files)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	assert.Equal(t, "main", edges[0].From.Name)
	assert.Equal(t, "helper", edges[0].To.Name)
}

func TestBuildCallGraphJava(t *testing.T) {
	tmpDir := t.TempDir()

	writeFile(t, tmpDir, "Main.java", `
public class Main {
	public static void main(String[] args) {
		Util.print();
	}
}
`)
	writeFile(t, tmpDir, "Util.java", `
public class Util {
	public static void print() {
		System.out.println("hello");
	}
}
`)

	files := []*analyzer.FileResult{
		{Filename: "Main.java", Classes: []analyzer.ClassInfo{{Name: "Main", Methods: []analyzer.FunctionInfo{{Name: "main"}}}}},
		{Filename: "Util.java", Classes: []analyzer.ClassInfo{{Name: "Util", Methods: []analyzer.FunctionInfo{{Name: "print"}}}}},
	}

	edges, err := BuildCallGraph(tmpDir, files)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	assert.Equal(t, "Main.main", edges[0].From.Name)
	assert.Equal(t, "Util.print", edges[0].To.Name)
}

func TestMermaidEscape(t *testing.T) {
	assert.Equal(t, "foo_bar", mermaidEscape("foo-bar"))
	assert.Equal(t, "a_b_c", mermaidEscape("a/b.c"))
	assert.Equal(t, "mod_1", mermaidEscape("mod:1"))
}

// helpers

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	err := os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)
}

func splitLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestBuildCallGraphPythonBasic(t *testing.T) {
	repoPath := filepath.Join("..", "..", "testdata", "repos", "python-basic")
	files := []*analyzer.FileResult{
		{Filename: "main.py", Functions: []analyzer.FunctionInfo{{Name: "main"}}},
		{
			Filename: "models/user.py",
			Classes: []analyzer.ClassInfo{
				{
					Name: "User",
					Methods: []analyzer.FunctionInfo{
						{Name: "create"},
						{Name: "authenticate"},
						{Name: "deactivate"},
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
						{Name: "register"},
						{Name: "authenticate"},
						{Name: "list_users"},
					},
				},
			},
		},
		{
			Filename: "repositories/user_repository.py",
			Classes: []analyzer.ClassInfo{
				{
					Name: "UserRepository",
					Methods: []analyzer.FunctionInfo{
						{Name: "save"},
						{Name: "find_by_id"},
						{Name: "find_by_username"},
						{Name: "find_all"},
					},
				},
			},
		},
		{Filename: "utils/crypto.py", Functions: []analyzer.FunctionInfo{{Name: "hash_password"}, {Name: "verify_password"}}},
		{Filename: "utils/logger.py", Functions: []analyzer.FunctionInfo{{Name: "get_logger"}}},
	}

	edges, err := BuildCallGraph(repoPath, files)
	require.NoError(t, err)
	require.NotEmpty(t, edges, "should discover inter-function calls in python-basic repo")

	seqs := FindSequences(edges, 2)
	require.NotEmpty(t, seqs, "should extract at least one sequence path")
	for _, s := range seqs {
		require.True(t, len(s.Messages) >= 2, "each sequence should have at least 2 messages")
		require.True(t, len(s.Participants) >= 2, "each sequence should cross multiple modules")
	}
}
