package benchmark

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/splitsword/fine-codewiki/internal/analyzer"
	"github.com/splitsword/fine-codewiki/internal/diagram"
	"github.com/splitsword/fine-codewiki/internal/docgen"
	"github.com/splitsword/fine-codewiki/internal/grapher"
	"github.com/splitsword/fine-codewiki/internal/sequencer"
)

// generateSyntheticPythonProject creates a temporary project with the specified
// number of Python files, each containing classes and functions with imports
// forming a layered dependency structure.
func generateSyntheticPythonProject(b *testing.B, numFiles int) string {
	b.Helper()
	tmpDir := b.TempDir()

	// Create modules in layers: models -> services -> controllers -> main
	layers := 4
	filesPerLayer := numFiles / layers
	if filesPerLayer < 1 {
		filesPerLayer = 1
	}

	layerNames := []string{"models", "services", "controllers", "main"}
	for layerIdx, layer := range layerNames {
		dir := filepath.Join(tmpDir, layer)
		if err := os.MkdirAll(dir, 0755); err != nil {
			b.Fatal(err)
		}
		for i := 0; i < filesPerLayer && layerIdx*filesPerLayer+i < numFiles; i++ {
			fileIdx := layerIdx*filesPerLayer + i
			filename := filepath.Join(dir, fmt.Sprintf("module_%d.py", fileIdx))
			content := generatePythonFile(layer, fileIdx, layerIdx, filesPerLayer)
			if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
				b.Fatal(err)
			}
		}
	}
	return tmpDir
}

func generatePythonFile(layer string, fileIdx, layerIdx, filesPerLayer int) string {
	className := fmt.Sprintf("%sClass%d", capitalize(layer), fileIdx)
	var imports []string
	if layerIdx > 0 {
		// Import from first file in previous layer
		prevLayer := []string{"models", "services", "controllers", "main"}[layerIdx-1]
		prevFileIdx := (layerIdx - 1) * filesPerLayer
		imports = append(imports, fmt.Sprintf("from %s.module_%d import %sClass%d",
			prevLayer, prevFileIdx, capitalize(prevLayer), prevFileIdx))
	}

	content := ""
	for _, imp := range imports {
		content += imp + "\n"
	}
	content += "\n"
	content += fmt.Sprintf("class %s:\n", className)
	content += fmt.Sprintf("    def __init__(self, id: int):\n")
	content += fmt.Sprintf("        self.id = id\n")
	content += fmt.Sprintf("        self.name = '%s_%d'\n", className, fileIdx)
	content += "\n"

	// Add methods
	for m := 0; m < 5; m++ {
		content += fmt.Sprintf("    def method_%d(self, x: int) -> int:\n", m)
		content += fmt.Sprintf("        result = x * %d + self.id\n", m+1)
		content += fmt.Sprintf("        return result\n")
		content += "\n"
	}

	// Add standalone functions
	for f := 0; f < 3; f++ {
		content += fmt.Sprintf("def helper_%d_%d(value: str) -> str:\n", fileIdx, f)
		content += fmt.Sprintf("    return value.upper() + '_%d'\n", f)
		content += "\n"
	}

	return content
}

func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	return string(s[0]-'a'+'A') + s[1:]
}

// BenchmarkEndToEndSmall benchmarks the full pipeline on a ~1k line project.
func BenchmarkEndToEndSmall(b *testing.B) {
	sourceDir := filepath.Join("..", "testdata", "repos", "python-basic")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		files, err := analyzer.ParseDirectory(sourceDir, "")
		if err != nil {
			b.Fatal(err)
		}
		graph := grapher.BuildGraph(files)
		archDSL, _ := diagram.GenerateArchitectureDiagram(graph)
		classDSL, _ := diagram.GenerateClassDiagram(graph)
		callEdges, _ := sequencer.BuildCallGraph(sourceDir, files)
		sequences := sequencer.FindSequences(callEdges, 2)
		var seqDSL string
		if len(sequences) > 0 {
			seqDSL = sequencer.GenerateSequenceDiagram(sequences[0])
		} else {
			seqDSL = "sequenceDiagram\n"
		}
		_, err = docgen.GenerateWikiEnhanced(context.Background(), nil, graph, sourceDir, "bench", archDSL, classDSL, seqDSL, "")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEndToEnd10K benchmarks the full pipeline on a ~10k line synthetic project.
func BenchmarkEndToEnd10K(b *testing.B) {
	sourceDir := generateSyntheticPythonProject(b, 40) // ~40 files * ~250 lines = ~10k lines
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		files, err := analyzer.ParseDirectory(sourceDir, "")
		if err != nil {
			b.Fatal(err)
		}
		graph := grapher.BuildGraph(files)
		archDSL, _ := diagram.GenerateArchitectureDiagram(graph)
		classDSL, _ := diagram.GenerateClassDiagram(graph)
		callEdges, _ := sequencer.BuildCallGraph(sourceDir, files)
		sequences := sequencer.FindSequences(callEdges, 2)
		var seqDSL string
		if len(sequences) > 0 {
			seqDSL = sequencer.GenerateSequenceDiagram(sequences[0])
		} else {
			seqDSL = "sequenceDiagram\n"
		}
		_, err = docgen.GenerateWikiEnhanced(context.Background(), nil, graph, sourceDir, "bench", archDSL, classDSL, seqDSL, "")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEndToEnd50K benchmarks the full pipeline on a ~50k line synthetic project.
func BenchmarkEndToEnd50K(b *testing.B) {
	sourceDir := generateSyntheticPythonProject(b, 200) // ~200 files * ~250 lines = ~50k lines
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		files, err := analyzer.ParseDirectory(sourceDir, "")
		if err != nil {
			b.Fatal(err)
		}
		graph := grapher.BuildGraph(files)
		archDSL, _ := diagram.GenerateArchitectureDiagram(graph)
		classDSL, _ := diagram.GenerateClassDiagram(graph)
		callEdges, _ := sequencer.BuildCallGraph(sourceDir, files)
		sequences := sequencer.FindSequences(callEdges, 2)
		var seqDSL string
		if len(sequences) > 0 {
			seqDSL = sequencer.GenerateSequenceDiagram(sequences[0])
		} else {
			seqDSL = "sequenceDiagram\n"
		}
		_, err = docgen.GenerateWikiEnhanced(context.Background(), nil, graph, sourceDir, "bench", archDSL, classDSL, seqDSL, "")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEndToEnd100K benchmarks the full pipeline on a ~100k line synthetic project.
func BenchmarkEndToEnd100K(b *testing.B) {
	sourceDir := generateSyntheticPythonProject(b, 400) // ~400 files * ~250 lines = ~100k lines
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		files, err := analyzer.ParseDirectory(sourceDir, "")
		if err != nil {
			b.Fatal(err)
		}
		graph := grapher.BuildGraph(files)
		archDSL, _ := diagram.GenerateArchitectureDiagram(graph)
		classDSL, _ := diagram.GenerateClassDiagram(graph)
		callEdges, _ := sequencer.BuildCallGraph(sourceDir, files)
		sequences := sequencer.FindSequences(callEdges, 2)
		var seqDSL string
		if len(sequences) > 0 {
			seqDSL = sequencer.GenerateSequenceDiagram(sequences[0])
		} else {
			seqDSL = "sequenceDiagram\n"
		}
		_, err = docgen.GenerateWikiEnhanced(context.Background(), nil, graph, sourceDir, "bench", archDSL, classDSL, seqDSL, "")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkParseDirectory10K benchmarks AST parsing for a 10k line project.
func BenchmarkParseDirectory10K(b *testing.B) {
	sourceDir := generateSyntheticPythonProject(b, 40)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := analyzer.ParseDirectory(sourceDir, "")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBuildGraph10K benchmarks graph construction for a 10k line project.
func BenchmarkBuildGraph10K(b *testing.B) {
	sourceDir := generateSyntheticPythonProject(b, 40)
	files, err := analyzer.ParseDirectory(sourceDir, "")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		grapher.BuildGraph(files)
	}
}

// BenchmarkGenerateWiki10K benchmarks wiki generation for a 10k line project.
func BenchmarkGenerateWiki10K(b *testing.B) {
	sourceDir := generateSyntheticPythonProject(b, 40)
	files, err := analyzer.ParseDirectory(sourceDir, "")
	if err != nil {
		b.Fatal(err)
	}
	graph := grapher.BuildGraph(files)
	archDSL, _ := diagram.GenerateArchitectureDiagram(graph)
	classDSL, _ := diagram.GenerateClassDiagram(graph)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := docgen.GenerateWikiEnhanced(context.Background(), nil, graph, sourceDir, "bench", archDSL, classDSL, "sequenceDiagram\n", "")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPageRank100Nodes benchmarks PageRank on a 100-node graph.
func BenchmarkPageRank100Nodes(b *testing.B) {
	files := generateSyntheticFileResults(100)
	graph := grapher.BuildGraph(files)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		graph.PageRank()
	}
}

// BenchmarkPageRank1000Nodes benchmarks PageRank on a 1000-node graph.
func BenchmarkPageRank1000Nodes(b *testing.B) {
	files := generateSyntheticFileResults(1000)
	graph := grapher.BuildGraph(files)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		graph.PageRank()
	}
}

func generateSyntheticFileResults(count int) []*analyzer.FileResult {
	var files []*analyzer.FileResult
	for i := 0; i < count; i++ {
		f := &analyzer.FileResult{
			Filename: fmt.Sprintf("modules/module_%d.py", i),
			Classes: []analyzer.ClassInfo{
				{Name: fmt.Sprintf("Class%d", i)},
			},
		}
		// Create a chain-like dependency structure
		if i < count-1 {
			f.Imports = []analyzer.ImportInfo{
				{Module: fmt.Sprintf("..modules.module_%d", i+1), IsRelative: true},
			}
		}
		files = append(files, f)
	}
	return files
}

// ReportMemoryUsage logs memory statistics (used by CI to track trends).
func ReportMemoryUsage(b *testing.B) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	b.Logf("Alloc = %d MB, TotalAlloc = %d MB, Sys = %d MB, NumGC = %d",
		m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)
}
