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
		if err != nil {
			fmt.Printf("警告：LLM 生成项目概述失败 (%v)\n", err)
		} else if enhanced == "" {
			fmt.Println("警告：LLM 返回了空的项目概述")
		} else if isChecklistLike(enhanced, graph) {
			fmt.Println("警告：LLM 返回的内容像是模块清单，已回退到静态描述")
		} else {
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
		if err != nil {
			fmt.Printf("警告：LLM 生成架构描述失败 (%v)\n", err)
		} else if enhanced != "" {
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
	fmt.Fprintf(&b, "你是一个资深软件架构师。请基于以下代码库信息，撰写一段项目概述（2-3 段）。\n\n")
	fmt.Fprintf(&b, "要求：\n")
	fmt.Fprintf(&b, "1. 描述这个项目的核心目标、主要功能和适用场景\n")
	fmt.Fprintf(&b, "2. 概括整体架构风格（如 MVC、微服务、单体、工具库等）\n")
	fmt.Fprintf(&b, "3. 说明关键模块的职责分工和协作方式\n")
	fmt.Fprintf(&b, "4. 不要只是罗列模块名称和文件清单，要体现对代码逻辑的理解\n")
	fmt.Fprintf(&b, "5. 使用简体中文\n\n")
	fmt.Fprintf(&b, "项目：%s\n", projectName)
	fmt.Fprintf(&b, "模块数：%d\n", len(graph.Nodes))
	fmt.Fprintf(&b, "依赖数：%d\n\n", len(graph.Edges))
	maxModulesInPrompt := 20
	fmt.Fprintf(&b, "模块列表（供参考，不要原样复述）：\n")
	for i, n := range graph.Nodes {
		if i >= maxModulesInPrompt {
			fmt.Fprintf(&b, "... 还有 %d 个模块未列出\n", len(graph.Nodes)-maxModulesInPrompt)
			break
		}
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

// isChecklistLike checks if the LLM output is just a repetition of module names
// rather than a real descriptive overview.
func isChecklistLike(text string, graph *grapher.Graph) bool {
	if len(text) < 20 {
		return true // too short to be a real description
	}

	// Count how many module names appear in the text
	moduleHits := 0
	for _, n := range graph.Nodes {
		if strings.Contains(text, n.Name) {
			moduleHits++
		}
	}

	// If more than 70% of module names are mentioned, it's likely a checklist
	if len(graph.Nodes) > 0 && float64(moduleHits)/float64(len(graph.Nodes)) > 0.7 {
		return true
	}

	// Count list markers (bullet points or numbered lists)
	listMarkerCount := strings.Count(text, "\n-") + strings.Count(text, "\n*") + strings.Count(text, "\n1.")
	lines := strings.Split(text, "\n")
	nonEmptyLines := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmptyLines++
		}
	}
	if nonEmptyLines > 0 && float64(listMarkerCount)/float64(nonEmptyLines) > 0.5 {
		return true
	}

	return false
}

// buildAutoDescription generates a static project description based on graph analysis.
func buildAutoDescription(graph *grapher.Graph, projectName string, classCount, funcCount int) string {
	var b strings.Builder

	// Basic scale description
	if len(graph.Nodes) == 1 {
		fmt.Fprintf(&b, "%s 是一个单模块项目", projectName)
	} else if len(graph.Nodes) <= 5 {
		fmt.Fprintf(&b, "%s 是一个小型项目，包含 %d 个模块", projectName, len(graph.Nodes))
	} else if len(graph.Nodes) <= 15 {
		fmt.Fprintf(&b, "%s 是一个中型项目，包含 %d 个模块", projectName, len(graph.Nodes))
	} else {
		fmt.Fprintf(&b, "%s 是一个大型项目，包含 %d 个模块", projectName, len(graph.Nodes))
	}

	if classCount > 0 {
		fmt.Fprintf(&b, "、%d 个类", classCount)
	}
	if funcCount > 0 {
		fmt.Fprintf(&b, "、%d 个函数/方法", funcCount)
	}
	b.WriteString("。")

	// Identify core modules (most depended upon)
	var coreModules []string
	maxDependents := 0
	for _, n := range graph.Nodes {
		dependents := len(graph.DependentsOf(n.Name))
		if dependents > maxDependents {
			maxDependents = dependents
			coreModules = []string{n.Name}
		} else if dependents == maxDependents && dependents > 0 {
			coreModules = append(coreModules, n.Name)
		}
	}
	if len(coreModules) > 0 && maxDependents >= 2 {
		b.WriteString("核心模块包括 ")
		for i, m := range coreModules {
			if i > 0 {
				b.WriteString("、")
			}
			fmt.Fprintf(&b, "`%s`", m)
		}
		fmt.Fprintf(&b, "，被项目中 %d 个其他模块所依赖。", maxDependents)
	}

	// Entry points hint
	entries := graph.EntryPoints()
	if len(entries) > 0 {
		b.WriteString("项目入口点为 ")
		for i, e := range entries {
			if i > 0 {
				b.WriteString("、")
			}
			fmt.Fprintf(&b, "`%s`", e.Name)
		}
		b.WriteString("。")
	}

	// Detect cycles
	cycles := graph.DetectCycles()
	if len(cycles) > 0 {
		b.WriteString("注意：项目中存在循环依赖，主要发生在 ")
		for i, c := range cycles {
			if i > 0 {
				b.WriteString("、")
			}
			fmt.Fprintf(&b, "`%s`", strings.Join(c.Nodes, "` → `"))
		}
		b.WriteString("。")
	}

	// Community detection for functional grouping
	communities := graph.DetectCommunities()
	// Only show communities if they actually group modules meaningfully
	var meaningfulCommunities []struct {
		name  string
		count int
	}
	for name, nodes := range communities {
		if len(nodes) >= 2 {
			meaningfulCommunities = append(meaningfulCommunities, struct {
				name  string
				count int
			}{name, len(nodes)})
		}
	}
	// Only output if we have at least 2 meaningful communities and they cover
	// a reasonable portion of the project (avoid 200 communities of 1 module)
	if len(meaningfulCommunities) >= 2 && len(meaningfulCommunities) <= len(graph.Nodes)/3 {
		fmt.Fprintf(&b, "按功能划分，项目大致可分为 %d 个模块组：", len(meaningfulCommunities))
		sort.Slice(meaningfulCommunities, func(i, j int) bool {
			return meaningfulCommunities[i].name < meaningfulCommunities[j].name
		})
		for i, c := range meaningfulCommunities {
			if i > 0 {
				b.WriteString("、")
			}
			fmt.Fprintf(&b, "%s（%d 个模块）", c.name, c.count)
		}
		b.WriteString("。")
	}

	return b.String()
}

func buildArchitecturePrompt(graph *grapher.Graph) string {
	var b strings.Builder
	fmt.Fprintf(&b, "分析以下模块依赖结构，用 2-3 段文字描述系统架构、设计模式及层级关系。\n\n")
	fmt.Fprintf(&b, "模块及其依赖：\n")
	maxModules := 20
	for i, n := range graph.Nodes {
		if i >= maxModules {
			fmt.Fprintf(&b, "... 还有 %d 个模块未列出\n", len(graph.Nodes)-maxModules)
			break
		}
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

	// Auto-generated project description based on graph analysis
	b.WriteString("## 项目简介\n\n")
	b.WriteString(buildAutoDescription(graph, projectName, classCount, funcCount))
	b.WriteString("\n\n")

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
