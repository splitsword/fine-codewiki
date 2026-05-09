package diagram

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/splitsword/fine-codewiki/internal/analyzer"
	"github.com/splitsword/fine-codewiki/internal/grapher"
	"github.com/stretchr/testify/require"
)

// TestMmdcValidateSnapshots runs the real mmdc CLI against stored diagram
// snapshots to ensure they are syntactically valid Mermaid.
// If mmdc is not installed, the test is skipped.
func TestMmdcValidateSnapshots(t *testing.T) {
	mmdc, err := exec.LookPath("mmdc")
	if err != nil {
		t.Skip("mmdc not found in PATH, skipping mmdc validation")
	}

	snapshotDir := filepath.Join("..", "..", "testdata", "expected", "diagrams", "python-basic")
	entries, err := os.ReadDir(snapshotDir)
	require.NoError(t, err)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".mmd") {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			inputPath := filepath.Join(snapshotDir, name)
			outputPath := filepath.Join(t.TempDir(), strings.TrimSuffix(name, ".mmd")+".svg")

			cmd := exec.Command(mmdc, "-i", inputPath, "-o", outputPath)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("mmdc failed for %s: %v\nOutput: %s", name, err, string(out))
			}
		})
	}
}

// TestMmdcValidateGenerated runs mmdc against freshly generated diagrams
// for a synthetic project to catch syntax regressions immediately.
func TestMmdcValidateGenerated(t *testing.T) {
	mmdc, err := exec.LookPath("mmdc")
	if err != nil {
		t.Skip("mmdc not found in PATH, skipping mmdc validation")
	}

	files := []*analyzer.FileResult{
		{
			Filename: "models/user.py",
			Classes:  []analyzer.ClassInfo{{Name: "User", Bases: []string{"BaseModel"}}},
			Imports:  []analyzer.ImportInfo{{Module: ".base", Name: "BaseModel", IsRelative: true}},
		},
		{Filename: "models/base.py", Classes: []analyzer.ClassInfo{{Name: "BaseModel"}}},
		{
			Filename: "services/api.py",
			Classes:  []analyzer.ClassInfo{{Name: "Api"}},
			Imports: []analyzer.ImportInfo{
				{Module: "..models.user", Name: "User", IsRelative: true},
			},
		},
	}
	graph := grapher.BuildGraph(files)

	// Architecture diagram
	archDSL, err := GenerateArchitectureDiagram(graph)
	require.NoError(t, err)
	runMmdc(t, mmdc, "architecture", archDSL)

	// Class diagram
	classDSL, err := GenerateClassDiagram(graph)
	require.NoError(t, err)
	runMmdc(t, mmdc, "class", classDSL)

	// Dependency diagram
	depDSL, err := GenerateDependencyDiagram(graph)
	require.NoError(t, err)
	runMmdc(t, mmdc, "dependency", depDSL)
}

func runMmdc(t *testing.T, mmdc, label, dsl string) {
	t.Helper()
	inputPath := filepath.Join(t.TempDir(), label+".mmd")
	outputPath := filepath.Join(t.TempDir(), label+".svg")
	require.NoError(t, os.WriteFile(inputPath, []byte(dsl), 0644))

	cmd := exec.Command(mmdc, "-i", inputPath, "-o", outputPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mmdc failed for %s: %v\nDSL:\n%s\nOutput:\n%s", label, err, dsl, string(out))
	}
}
