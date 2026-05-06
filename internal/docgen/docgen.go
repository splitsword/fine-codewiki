package docgen

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/splitsword/fine-codewiki/internal/grapher"
	"github.com/splitsword/fine-codewiki/internal/llm"
)

// Wiki holds all generated documentation artifacts.
type Wiki struct {
	ProjectName         string
	Overview            string
	APIReference        string
	Architecture        string
	ClassDiagram        string
	ArchitectureDiagram string
}

// GenerateWiki produces a complete Wiki from analysis results and diagrams.
func GenerateWiki(graph *grapher.Graph, projectName, archDSL, classDSL string) (*Wiki, error) {
	return GenerateWikiEnhanced(context.Background(), nil, graph, projectName, archDSL, classDSL)
}

// GenerateWikiEnhanced produces a Wiki with optional LLM enhancement.
// If provider is nil, falls back to static generation.
func GenerateWikiEnhanced(ctx context.Context, provider llm.Provider, graph *grapher.Graph, projectName, archDSL, classDSL string) (*Wiki, error) {
	overview, err := GenerateOverviewMarkdown(graph, projectName)
	if err != nil {
		return nil, fmt.Errorf("generate overview: %w", err)
	}

	// LLM enhancement for overview
	if provider != nil {
		prompt := buildOverviewPrompt(graph, projectName)
		enhanced, err := provider.Complete(ctx, prompt)
		if err == nil && enhanced != "" {
			overview = fmt.Sprintf("# %s\n\n%s\n\n---\n\n%s", projectName, enhanced, overview)
		}
	}

	apiRef, err := GenerateAPIReferenceMarkdown(graph)
	if err != nil {
		return nil, fmt.Errorf("generate api reference: %w", err)
	}

	arch, err := GenerateArchitectureMarkdown(graph, archDSL)
	if err != nil {
		return nil, fmt.Errorf("generate architecture doc: %w", err)
	}

	// LLM enhancement for architecture
	if provider != nil {
		prompt := buildArchitecturePrompt(graph)
		enhanced, err := provider.Complete(ctx, prompt)
		if err == nil && enhanced != "" {
			arch = fmt.Sprintf("# Architecture\n\n%s\n\n---\n\n%s", enhanced, arch)
		}
	}

	return &Wiki{
		ProjectName:         projectName,
		Overview:            overview,
		APIReference:        apiRef,
		Architecture:        arch,
		ClassDiagram:        classDSL,
		ArchitectureDiagram: archDSL,
	}, nil
}

func buildOverviewPrompt(graph *grapher.Graph, projectName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Analyze the following codebase and write a concise project overview in 2-3 paragraphs.\n\n")
	fmt.Fprintf(&b, "Project: %s\n", projectName)
	fmt.Fprintf(&b, "Modules: %d\n", len(graph.Nodes))
	fmt.Fprintf(&b, "Dependencies: %d\n\n", len(graph.Edges))
	fmt.Fprintf(&b, "Module list:\n")
	for _, n := range graph.Nodes {
		line := "- " + n.Name
		if len(n.Classes) > 0 {
			line += fmt.Sprintf(" (%d classes)", len(n.Classes))
		}
		if len(n.Functions) > 0 {
			line += fmt.Sprintf(" (%d functions)", len(n.Functions))
		}
		fmt.Fprintln(&b, line)
	}
	return b.String()
}

func buildArchitecturePrompt(graph *grapher.Graph) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Analyze the following module dependency structure and describe the system architecture, design patterns, and layer relationships in 2-3 paragraphs.\n\n")
	fmt.Fprintf(&b, "Modules and their dependencies:\n")
	for _, n := range graph.Nodes {
		deps := graph.DependenciesOf(n.Name)
		if len(deps) > 0 {
			fmt.Fprintf(&b, "- %s depends on: %s\n", n.Name, strings.Join(deps, ", "))
		} else {
			fmt.Fprintf(&b, "- %s (no internal dependencies)\n", n.Name)
		}
	}
	return b.String()
}

// GenerateOverviewMarkdown creates a project overview Markdown document.
func GenerateOverviewMarkdown(graph *grapher.Graph, projectName string) (string, error) {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# %s\n\n", projectName))

	if len(graph.Nodes) == 0 {
		b.WriteString("No modules found in this project.\n")
		return b.String(), nil
	}

	// Stats
	var classCount, funcCount int
	for _, n := range graph.Nodes {
		classCount += len(n.Classes)
		funcCount += len(n.Functions)
		for _, c := range n.Classes {
			funcCount += len(c.Methods)
		}
	}

	b.WriteString("## Project Stats\n\n")
	b.WriteString(fmt.Sprintf("- **Modules**: %d\n", len(graph.Nodes)))
	b.WriteString(fmt.Sprintf("- **Classes**: %d\n", classCount))
	b.WriteString(fmt.Sprintf("- **Functions**: %d\n", funcCount))
	b.WriteString(fmt.Sprintf("- **Dependencies**: %d\n\n", len(graph.Edges)))

	// Entry points
	entries := graph.EntryPoints()
	if len(entries) > 0 {
		b.WriteString("## Entry Points\n\n")
		for _, e := range entries {
			b.WriteString(fmt.Sprintf("- `%s`\n", e.Name))
		}
		b.WriteString("\n")
	}

	// Module list grouped by directory
	groups := graph.GroupByDirectory()
	b.WriteString("## Modules\n\n")
	for _, dir := range sortedKeys(groups) {
		nodes := groups[dir]
		if dir == "." || dir == "" {
			dir = "(root)"
		}
		b.WriteString(fmt.Sprintf("### %s\n\n", dir))
		for _, n := range nodes {
			b.WriteString(fmt.Sprintf("- `%s`", n.Name))
			if len(n.Classes) > 0 {
				b.WriteString(fmt.Sprintf(" — %d classes", len(n.Classes)))
			}
			if len(n.Functions) > 0 {
				b.WriteString(fmt.Sprintf(" — %d functions", len(n.Functions)))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	return b.String(), nil
}

// GenerateAPIReferenceMarkdown creates an API reference from classes and functions.
func GenerateAPIReferenceMarkdown(graph *grapher.Graph) (string, error) {
	var b strings.Builder
	b.WriteString("# API Reference\n\n")

	if len(graph.Nodes) == 0 {
		b.WriteString("No API symbols found.\n")
		return b.String(), nil
	}

	// Classes
	var hasClasses bool
	for _, n := range graph.Nodes {
		if len(n.Classes) > 0 {
			hasClasses = true
			break
		}
	}

	if hasClasses {
		b.WriteString("## Classes\n\n")
		for _, n := range graph.Nodes {
			for _, c := range n.Classes {
				b.WriteString(fmt.Sprintf("### %s\n\n", c.Name))
				if len(c.Bases) > 0 {
					b.WriteString(fmt.Sprintf("**Inherits**: %s\n\n", strings.Join(c.Bases, ", ")))
				}
				if len(c.Methods) > 0 {
					b.WriteString("#### Methods\n\n")
					for _, m := range c.Methods {
						sig := formatSignature(m.Name, m.Params, m.ReturnType)
						b.WriteString(fmt.Sprintf("- `%s`\n", sig))
					}
					b.WriteString("\n")
				}
			}
		}
	}

	// Functions
	var hasFunctions bool
	for _, n := range graph.Nodes {
		if len(n.Functions) > 0 {
			hasFunctions = true
			break
		}
	}

	if hasFunctions {
		b.WriteString("## Functions\n\n")
		for _, n := range graph.Nodes {
			for _, f := range n.Functions {
				b.WriteString(fmt.Sprintf("### %s\n\n", f.Name))
				sig := formatSignature(f.Name, f.Params, f.ReturnType)
				b.WriteString(fmt.Sprintf("```\n%s\n```\n\n", sig))
			}
		}
	}

	return b.String(), nil
}

// GenerateArchitectureMarkdown creates an architecture document with embedded diagrams.
func GenerateArchitectureMarkdown(graph *grapher.Graph, archDSL string) (string, error) {
	var b strings.Builder
	b.WriteString("# Architecture\n\n")

	// Module overview table
	b.WriteString("## Module Overview\n\n")
	b.WriteString("| Module | Type | Dependencies | Dependents |\n")
	b.WriteString("|--------|------|--------------|------------|\n")

	for _, n := range graph.Nodes {
		nodeType := "module"
		if len(n.Classes) > 0 {
			nodeType = "classes"
		} else if len(n.Functions) > 0 {
			nodeType = "functions"
		}

		deps := graph.DependenciesOf(n.Name)
		depsStr := "—"
		if len(deps) > 0 {
			depsStr = "`" + strings.Join(deps, "`, `") + "`"
		}

		dependents := graph.DependentsOf(n.Name)
		depStr := "—"
		if len(dependents) > 0 {
			depStr = "`" + strings.Join(dependents, "`, `") + "`"
		}

		b.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s |\n", n.Name, nodeType, depsStr, depStr))
	}
	b.WriteString("\n")

	// Embedded architecture diagram
	if archDSL != "" {
		b.WriteString("## Dependency Graph\n\n")
		b.WriteString("```mermaid\n")
		b.WriteString(archDSL)
		b.WriteString("```\n\n")
	}

	return b.String(), nil
}

// WriteWikiFiles writes all Wiki artifacts to the output directory.
func WriteWikiFiles(outputDir string, wiki *Wiki) error {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	files := map[string]string{
		"overview.md":      wiki.Overview,
		"api-reference.md": wiki.APIReference,
		"architecture.md":  wiki.Architecture,
		"class-diagram.mmd":   wiki.ClassDiagram,
		"architecture.mmd":    wiki.ArchitectureDiagram,
	}

	for name, content := range files {
		path := filepath.Join(outputDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}

	return nil
}

// formatSignature builds a human-readable function signature.
func formatSignature(name string, params []string, returnType string) string {
	// Filter out self/cls
	var displayParams []string
	for _, p := range params {
		if p != "self" && p != "cls" {
			displayParams = append(displayParams, p)
		}
	}

	sig := fmt.Sprintf("%s(%s)", name, strings.Join(displayParams, ", "))
	if returnType != "" {
		sig += " -> " + returnType
	}
	return sig
}

// markdownEscape escapes Markdown special characters in text.
func markdownEscape(s string) string {
	s = strings.ReplaceAll(s, "*", "\\*")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}

func sortedKeys(m map[string][]*grapher.Node) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Ensure root entries come first
	sort.Slice(keys, func(i, j int) bool {
		ri := keys[i] == "." || keys[i] == ""
		rj := keys[j] == "." || keys[j] == ""
		if ri != rj {
			return ri // root comes first
		}
		return keys[i] < keys[j]
	})
	return keys
}
