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
	SequenceDiagram     string
}

// GenerateWiki produces a complete Wiki from analysis results and diagrams.
func GenerateWiki(graph *grapher.Graph, projectName, archDSL, classDSL, seqDSL string) (*Wiki, error) {
	return GenerateWikiEnhanced(context.Background(), nil, graph, projectName, archDSL, classDSL, seqDSL)
}

// GenerateWikiEnhanced produces a Wiki with optional LLM enhancement.
// If provider is nil, falls back to static generation.
func GenerateWikiEnhanced(ctx context.Context, provider llm.Provider, graph *grapher.Graph, projectName, archDSL, classDSL, seqDSL string) (*Wiki, error) {
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
		SequenceDiagram:     seqDSL,
	}, nil
}

func buildOverviewPrompt(graph *grapher.Graph, projectName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "分析以下代码库，用 2-3 段简洁的文字撰写项目概述。\n\n")
	fmt.Fprintf(&b, "项目：%s\n", projectName)
	fmt.Fprintf(&b, "模块数：%d\n", len(graph.Nodes))
	fmt.Fprintf(&b, "依赖数：%d\n\n", len(graph.Edges))
	fmt.Fprintf(&b, "模块列表：\n")
	for _, n := range graph.Nodes {
		line := "- " + n.Name
		if len(n.Classes) > 0 {
			line += fmt.Sprintf("（%d 个类）", len(n.Classes))
		}
		if len(n.Functions) > 0 {
			line += fmt.Sprintf("（%d 个函数）", len(n.Functions))
		}
		fmt.Fprintln(&b, line)
	}
	return b.String()
}

func buildArchitecturePrompt(graph *grapher.Graph) string {
	var b strings.Builder
	fmt.Fprintf(&b, "分析以下模块依赖结构，用 2-3 段文字描述系统架构、设计模式及层级关系。\n\n")
	fmt.Fprintf(&b, "模块及其依赖：\n")
	for _, n := range graph.Nodes {
		deps := graph.DependenciesOf(n.Name)
		if len(deps) > 0 {
			fmt.Fprintf(&b, "- %s 依赖：%s\n", n.Name, strings.Join(deps, ", "))
		} else {
			fmt.Fprintf(&b, "- %s（无内部依赖）\n", n.Name)
		}
	}
	return b.String()
}

// GenerateOverviewMarkdown creates a project overview Markdown document.
func GenerateOverviewMarkdown(graph *grapher.Graph, projectName string) (string, error) {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# %s\n\n", projectName))

	if len(graph.Nodes) == 0 {
		b.WriteString("未在项目中找到模块。\n")
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

	b.WriteString("## 项目统计\n\n")
	b.WriteString(fmt.Sprintf("- **模块**：%d\n", len(graph.Nodes)))
	b.WriteString(fmt.Sprintf("- **类**：%d\n", classCount))
	b.WriteString(fmt.Sprintf("- **函数**：%d\n", funcCount))
	b.WriteString(fmt.Sprintf("- **依赖**：%d\n\n", len(graph.Edges)))

	// Entry points
	entries := graph.EntryPoints()
	if len(entries) > 0 {
		b.WriteString("## 入口点\n\n")
		for _, e := range entries {
			b.WriteString(fmt.Sprintf("- `%s`\n", e.Name))
		}
		b.WriteString("\n")
	}

	// Module list grouped by directory
	groups := graph.GroupByDirectory()
	b.WriteString("## 模块\n\n")
	for _, dir := range sortedKeys(groups) {
		nodes := groups[dir]
		if dir == "." || dir == "" {
			dir = "（根目录）"
		}
		b.WriteString(fmt.Sprintf("### %s\n\n", dir))
		for _, n := range nodes {
			b.WriteString(fmt.Sprintf("- `%s`", n.Name))
			if len(n.Classes) > 0 {
				b.WriteString(fmt.Sprintf(" — %d 个类", len(n.Classes)))
			}
			if len(n.Functions) > 0 {
				b.WriteString(fmt.Sprintf(" — %d 个函数", len(n.Functions)))
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
	b.WriteString("# API 参考\n\n")

	if len(graph.Nodes) == 0 {
		b.WriteString("未找到 API 符号。\n")
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
		b.WriteString("## 类\n\n")
		for _, n := range graph.Nodes {
			for _, c := range n.Classes {
				b.WriteString(fmt.Sprintf("### %s\n\n", c.Name))
				if len(c.Bases) > 0 {
					b.WriteString(fmt.Sprintf("**继承**：%s\n\n", strings.Join(c.Bases, ", ")))
				}
				if len(c.Methods) > 0 {
					b.WriteString("#### 方法\n\n")
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
		b.WriteString("## 函数\n\n")
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
	b.WriteString("# 架构\n\n")

	// Module overview table
	b.WriteString("## 模块概览\n\n")
	b.WriteString("| 模块 | 类型 | 依赖 | 被依赖 |\n")
	b.WriteString("|------|------|------|--------|\n")

	for _, n := range graph.Nodes {
		nodeType := "模块"
		if len(n.Classes) > 0 {
			nodeType = "类模块"
		} else if len(n.Functions) > 0 {
			nodeType = "函数模块"
		}

		deps := graph.DependenciesOf(n.Name)
		depsStr := "—"
		if len(deps) > 0 {
			depsStr = "`" + strings.Join(deps, "`，`") + "`"
		}

		dependents := graph.DependentsOf(n.Name)
		depStr := "—"
		if len(dependents) > 0 {
			depStr = "`" + strings.Join(dependents, "`，`") + "`"
		}

		b.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s |\n", n.Name, nodeType, depsStr, depStr))
	}
	b.WriteString("\n")

	// Embedded architecture diagram
	if archDSL != "" {
		b.WriteString("## 依赖图\n\n")
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
		"overview.md":         wiki.Overview,
		"api-reference.md":    wiki.APIReference,
		"architecture.md":     wiki.Architecture,
		"class-diagram.mmd":   wiki.ClassDiagram,
		"architecture.mmd":    wiki.ArchitectureDiagram,
		"sequence-diagram.mmd": wiki.SequenceDiagram,
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
