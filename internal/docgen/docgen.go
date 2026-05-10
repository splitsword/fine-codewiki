package docgen

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/splitsword/fine-codewiki/internal/analyzer"
	"github.com/splitsword/fine-codewiki/internal/grapher"
	"github.com/splitsword/fine-codewiki/internal/llm"
	"github.com/splitsword/fine-codewiki/internal/sequencer"
)

// Wiki holds all generated documentation artifacts.
type Wiki struct {
	ProjectName         string
	Overview            string
	WhatItDoes          string
	ProjectStructure    string
	KeyConcepts         string
	LearningPath        string
	APIReference        string
	Architecture        string
	ClassDiagram        string
	ArchitectureDiagram string
	SequenceDiagram     string
	SequenceDescription string
	ModuleDocs          map[string]string // module name -> markdown content
	ModuleThemes        map[string][]string // theme -> sorted module names
}

// wikiCheckpoint persists partial LLM-enhanced results for resume support.
type wikiCheckpoint struct {
	Overview      string            `json:"overview,omitempty"`
	WhatItDoes    string            `json:"what_it_does,omitempty"`
	KeyConcepts   string            `json:"key_concepts,omitempty"`
	LearningPath  string            `json:"learning_path,omitempty"`
	ArchNarrative string            `json:"arch_narrative,omitempty"`
	FuncDescMap   map[string]string `json:"func_desc_map,omitempty"`
	Timestamp     time.Time         `json:"timestamp"`
}

func loadWikiCheckpoint(path string) *wikiCheckpoint {
	b, err := os.ReadFile(path)
	if err != nil {
		return &wikiCheckpoint{FuncDescMap: make(map[string]string)}
	}
	var cp wikiCheckpoint
	if err := json.Unmarshal(b, &cp); err != nil {
		return &wikiCheckpoint{FuncDescMap: make(map[string]string)}
	}
	if cp.FuncDescMap == nil {
		cp.FuncDescMap = make(map[string]string)
	}
	return &cp
}

func saveWikiCheckpoint(path string, cp *wikiCheckpoint) error {
	cp.Timestamp = time.Now()
	b, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}

// ClearWikiCheckpoint removes the checkpoint so the next run starts fresh.
func ClearWikiCheckpoint(sourceDir string) {
	path := filepath.Join(sourceDir, ".codewiki", "checkpoint", "wiki.json")
	_ = os.Remove(path)
}

// GenerateWiki produces a complete Wiki from analysis results and diagrams.
func GenerateWiki(graph *grapher.Graph, projectName, archDSL, classDSL, seqDSL string) (*Wiki, error) {
	return generateWikiEnhanced(context.Background(), nil, graph, "", projectName, archDSL, classDSL, seqDSL, "", 0)
}

// GenerateWikiEnhanced produces a Wiki with optional LLM enhancement.
// If provider is nil, falls back to static generation.
func GenerateWikiEnhanced(ctx context.Context, provider llm.Provider, graph *grapher.Graph, sourceDir, projectName, archDSL, classDSL, seqDSL, language string) (*Wiki, error) {
	return generateWikiEnhanced(ctx, provider, graph, sourceDir, projectName, archDSL, classDSL, seqDSL, language, -1)
}

// GenerateWikiEnhancedWithMaxFunctions produces a Wiki with optional LLM enhancement.
// maxLLMFunctions controls the cap on functions sent for LLM semantic description:
//   - negative value (e.g. -1): auto-compute target (30% of total, floor 10)
//   - zero: skip LLM function-level enhancement entirely
//   - positive value: hard cap on number of functions
func GenerateWikiEnhancedWithMaxFunctions(ctx context.Context, provider llm.Provider, graph *grapher.Graph, sourceDir, projectName, archDSL, classDSL, seqDSL, language string, maxLLMFunctions int) (*Wiki, error) {
	return generateWikiEnhanced(ctx, provider, graph, sourceDir, projectName, archDSL, classDSL, seqDSL, language, maxLLMFunctions)
}

func generateWikiEnhanced(ctx context.Context, provider llm.Provider, graph *grapher.Graph, sourceDir, projectName, archDSL, classDSL, seqDSL, language string, maxLLMFunctions int) (*Wiki, error) {
	cpPath := ""
	if sourceDir != "" {
		cpPath = filepath.Join(sourceDir, ".codewiki", "checkpoint", "wiki.json")
	}
	cp := loadWikiCheckpoint(cpPath)

	readme := loadProjectReadme(sourceDir)

	overview, err := GenerateOverviewMarkdown(graph, projectName)
	if err != nil {
		return nil, fmt.Errorf("generate overview: %w", err)
	}

	// LLM enhancement for overview: replace static list with narrative when successful.
	staticOverview := overview
	if provider != nil {
		if cp.Overview != "" {
			fmt.Println("[Checkpoint] 恢复项目概述")
			overview = cp.Overview
		} else {
			fmt.Println("[LLM] 正在生成项目概述...")
			prompt := buildOverviewPrompt(graph, projectName, readme, language)
			enhanced, err := provider.Complete(ctx, prompt)
			if err != nil {
				fmt.Printf("警告：LLM 生成项目概述失败 (%v)\n", err)
				if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline exceeded") {
					fmt.Println("提示：请求超时，请在 ~/.codewiki/config.yaml 中增加 timeout 值（例如 timeout: 300）")
				}
			} else if enhanced == "" {
				fmt.Println("警告：LLM 返回了空的项目概述")
			} else if isChecklistLike(enhanced, graph) {
				fmt.Println("警告：LLM 返回的内容像是模块清单，已回退到静态描述")
			} else {
				if hallucinated, hasHallucination := detectHallucination(enhanced, graph); hasHallucination {
					fmt.Printf("提示：LLM 输出中发现 %d 处可能不存在的引用（%s），仍采用增强内容\n", len(hallucinated), strings.Join(hallucinated, ", "))
				}
				overview = fmt.Sprintf("# %s\n\n%s", projectName, enhanced)
			}
			cp.Overview = overview
			_ = saveWikiCheckpoint(cpPath, cp)
			fmt.Println("[LLM] 项目概述生成完成")
		}
	}
	_ = staticOverview // retain for potential future use (e.g. compilation appendix)

	// Generate "What It Does" narrative article
	staticWhatItDoes := GenerateWhatItDoesMarkdown(graph, projectName)
	whatItDoes := staticWhatItDoes
	if provider != nil {
		if cp.WhatItDoes != "" {
			fmt.Println("[Checkpoint] 恢复项目核心能力说明")
			whatItDoes = cp.WhatItDoes
		} else {
			fmt.Println("[LLM] 正在生成项目核心能力说明...")
			prompt := buildWhatItDoesPrompt(graph, projectName, readme, language)
			enhanced, err := provider.Complete(ctx, prompt)
			if err != nil {
				fmt.Printf("警告：LLM 生成核心能力说明失败 (%v)\n", err)
			} else if enhanced != "" && !isChecklistLike(enhanced, graph) {
				whatItDoes = fmt.Sprintf("# %s 能做什么\n\n%s", projectName, enhanced)
			}
			cp.WhatItDoes = whatItDoes
			_ = saveWikiCheckpoint(cpPath, cp)
			fmt.Println("[LLM] 核心能力说明生成完成")
		}
	}

	// Generate project structure guide
	projectStructure := GenerateProjectStructureMarkdown(graph, projectName)

	// Generate key concepts / design decisions
	keyConcepts := ""
	if provider != nil && cp.KeyConcepts != "" {
		fmt.Println("[Checkpoint] 恢复关键设计决策")
		keyConcepts = cp.KeyConcepts
	} else if provider != nil {
		fmt.Println("[LLM] 正在生成关键设计决策...")
		prompt := buildKeyConceptsPrompt(graph, projectName, language)
		batchCtx, batchCancel := context.WithTimeout(ctx, 5*time.Minute)
		enhanced, err := provider.Complete(batchCtx, prompt)
		batchCancel()
		if err != nil {
			fmt.Printf("警告：LLM 生成设计决策失败 (%v)\n", err)
			fmt.Println("[Fallback] 使用静态分析生成关键设计决策...")
			keyConcepts = GenerateKeyConceptsFallback(graph, projectName)
		} else if enhanced == "" || isChecklistLike(enhanced, graph) {
			fmt.Println("警告：LLM 返回的设计决策内容无效，使用静态分析回退")
			keyConcepts = GenerateKeyConceptsFallback(graph, projectName)
		} else {
			keyConcepts = enhanced
		}
		cp.KeyConcepts = keyConcepts
		_ = saveWikiCheckpoint(cpPath, cp)
		fmt.Println("[LLM] 设计决策生成完成")
	} else {
		keyConcepts = GenerateKeyConceptsFallback(graph, projectName)
	}

	// Generate learning path
	staticLearningPath := GenerateLearningPathMarkdown(graph, projectName, keyConcepts != "")
	learningPath := staticLearningPath
	if provider != nil {
		if cp.LearningPath != "" {
			fmt.Println("[Checkpoint] 恢复学习路径")
			learningPath = cp.LearningPath
		} else {
			fmt.Println("[LLM] 正在生成学习路径...")
			prompt := buildLearningPathPrompt(graph, projectName, language)
			batchCtx, batchCancel := context.WithTimeout(ctx, 5*time.Minute)
			enhanced, err := provider.Complete(batchCtx, prompt)
			batchCancel()
			if err != nil {
				fmt.Printf("警告：LLM 生成学习路径失败 (%v)\n", err)
				fmt.Println("[Fallback] 使用静态学习路径...")
			} else if enhanced == "" || isChecklistLike(enhanced, graph) {
				fmt.Println("警告：LLM 返回的学习路径内容无效，使用静态回退")
			} else {
				learningPath = fmt.Sprintf("# %s 学习路径\n\n%s", projectName, enhanced)
			}
			cp.LearningPath = learningPath
			_ = saveWikiCheckpoint(cpPath, cp)
			fmt.Println("[LLM] 学习路径生成完成")
		}
	}

	// Append source attribution to static-only articles (LLM-enhanced ones have inline per-paragraph attribution)
	projectStructure += buildSourcesFooter(graph, 10)
	if keyConcepts != "" {
		// Embed class diagram into key-concepts for thematic cohesion
		if classDSL != "" {
			keyConcepts += "\n## 类型关系图\n\n下图展示了项目中核心类与接口的继承和组合关系：\n\n```mermaid\n" + classDSL + "\n```\n"
		}
	}
	// Embed sequence diagram into learning-path for thematic cohesion
	if seqDSL != "" {
		learningPath += "\n## 关键调用流程\n\n下图展示了系统中一条典型调用链的交互顺序：\n\n```mermaid\n" + seqDSL + "\n```\n"
	}

	// Build call graph for richer function context
	var callEdges []sequencer.CallEdge
	if sourceDir != "" {
		files := nodesToFileResults(graph.Nodes)
		edges, err := sequencer.BuildCallGraph(sourceDir, files)
		if err == nil {
			callEdges = edges
		}
	}

	// LLM enhancement for top function descriptions
	var funcDescMap map[string]string
	if provider != nil && maxLLMFunctions != 0 && len(cp.FuncDescMap) > 0 {
		fmt.Printf("[Checkpoint] 恢复 %d 个函数语义描述\n", len(cp.FuncDescMap))
		funcDescMap = cp.FuncDescMap
	} else if provider != nil && maxLLMFunctions != 0 {
		topFuncs := selectTopFunctions(graph, callEdges, maxLLMFunctions)
		if len(topFuncs) > 0 {
			funcDescMap = make(map[string]string)
			// Batch in groups of 8 for deeper per-function analysis
			batchSize := 8
			totalBatches := (len(topFuncs) + batchSize - 1) / batchSize
			fmt.Printf("[LLM] 正在生成 %d 个函数的语义描述（共 %d 批，每批最多 %d 个）...\n", len(topFuncs), totalBatches, batchSize)
			for i := 0; i < len(topFuncs); i += batchSize {
				end := i + batchSize
				if end > len(topFuncs) {
					end = len(topFuncs)
				}
				batch := topFuncs[i:end]
				batchNum := i/batchSize + 1
				fmt.Printf("  批次 %d/%d（%d 个函数）...\n", batchNum, totalBatches, len(batch))
				prompt := buildFunctionDescriptionPrompt(batch)
				// 每批独立超时 5 分钟，防止单批 hung 住拖垮整个流程
				batchCtx, batchCancel := context.WithTimeout(ctx, 5*time.Minute)
				enhanced, err := provider.Complete(batchCtx, prompt)
				batchCancel()
				if err != nil {
					if isTimeoutErr(err) {
						fmt.Printf("  批次 %d/%d 超时（5分钟），跳过\n", batchNum, totalBatches)
					} else {
						fmt.Printf("  批次 %d/%d 失败 (%v)，跳过\n", batchNum, totalBatches, err)
					}
					continue
				}
				if enhanced != "" {
					batchDescs := parseFunctionDescriptions(enhanced, batch)
					for k, v := range batchDescs {
						funcDescMap[k] = v
					}
				}
			}
			totalFuncs := 0
			for _, n := range graph.Nodes {
				totalFuncs += len(n.Functions)
				for _, c := range n.Classes {
					totalFuncs += len(c.Methods)
				}
			}
			fmt.Printf("[LLM] 函数语义描述完成：%d/%d 个函数（覆盖率 %.0f%%）\n",
				len(funcDescMap), totalFuncs, float64(len(funcDescMap))*100/float64(totalFuncs))
			cp.FuncDescMap = funcDescMap
			_ = saveWikiCheckpoint(cpPath, cp)
		}
	}

	apiRef, err := GenerateAPIReferenceMarkdown(graph, funcDescMap)
	if err != nil {
		return nil, fmt.Errorf("generate api reference: %w", err)
	}

	// LLM enhancement for architecture
	archNarrative := ""
	if provider != nil {
		if cp.ArchNarrative != "" {
			fmt.Println("[Checkpoint] 恢复架构描述")
			archNarrative = cp.ArchNarrative
		} else {
			fmt.Println("[LLM] 正在生成架构描述...")
			prompt := buildArchitecturePrompt(graph, language)
			enhanced, err := provider.Complete(ctx, prompt)
			if err != nil {
				fmt.Printf("警告：LLM 生成架构描述失败 (%v)\n", err)
			} else if enhanced == "" {
				fmt.Println("警告：LLM 返回了空的架构描述")
			} else if isChecklistLike(enhanced, graph) {
				fmt.Println("警告：LLM 返回的架构描述像是模块清单，已回退到静态描述")
			} else {
				if hallucinated, hasHallucination := detectHallucination(enhanced, graph); hasHallucination {
					fmt.Printf("提示：LLM 架构描述中发现 %d 处可能不存在的引用（%s），仍采用增强内容\n", len(hallucinated), strings.Join(hallucinated, ", "))
				}
				archNarrative = enhanced
			}
			cp.ArchNarrative = archNarrative
			_ = saveWikiCheckpoint(cpPath, cp)
			fmt.Println("[LLM] 架构描述生成完成")
		}
	}

	arch, err := GenerateArchitectureMarkdown(graph, archDSL, archNarrative)
	if err != nil {
		return nil, fmt.Errorf("generate architecture doc: %w", err)
	}

	moduleDocs := GenerateModuleDocs(graph)

	// Build module theme grouping for navigation and indexing
	moduleThemes := make(map[string][]string)
	if graph != nil {
		themeGroups := groupModulesByTheme(graph)
		for theme, nodes := range themeGroups {
			for _, n := range nodes {
				moduleThemes[theme] = append(moduleThemes[theme], n.Name)
			}
		}
	}

	hasConcepts := keyConcepts != ""
	overview += buildWhereToGoNext("00-overview.md", hasConcepts)
	whatItDoes += buildWhereToGoNext("01-what-it-does.md", hasConcepts)
	arch += buildWhereToGoNext("02-architecture.md", hasConcepts)
	projectStructure += buildWhereToGoNext("03-project-structure.md", hasConcepts)
	if keyConcepts != "" {
		keyConcepts += buildWhereToGoNext("04-key-concepts.md", hasConcepts)
	}
	learningPath += buildWhereToGoNext("05-learning-path.md", hasConcepts)
	apiRef += buildWhereToGoNext("api-reference.md", hasConcepts)

	// 成功完成后清除 checkpoint，下次从头开始
	if cpPath != "" {
		_ = os.Remove(cpPath)
	}

	return &Wiki{
		ProjectName:         projectName,
		Overview:            overview,
		WhatItDoes:          whatItDoes,
		ProjectStructure:    projectStructure,
		KeyConcepts:         keyConcepts,
		LearningPath:        learningPath,
		APIReference:        apiRef,
		Architecture:        arch,
		ClassDiagram:        classDSL,
		ArchitectureDiagram: archDSL,
		SequenceDiagram:     seqDSL,
		ModuleDocs:          moduleDocs,
		ModuleThemes:        moduleThemes,
	}, nil
}

// loadProjectReadme attempts to read README.md or similar from the project root.
func loadProjectReadme(dir string) string {
	if dir == "" {
		return ""
	}
	names := []string{"README.md", "readme.md", "Readme.md", "README_zh.md", "README_CN.md", "README.txt"}
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err == nil {
			// Truncate to avoid overly long prompts
			text := string(data)
			if len(text) > 4000 {
				text = text[:4000] + "\n...（README 内容已截断）"
			}
			return text
		}
	}
	return ""
}

// isBuildArtifact returns true for paths that are typically build outputs.
func isBuildArtifact(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "/dist/") ||
		strings.Contains(lower, "/build/") ||
		strings.Contains(lower, "/.git/") ||
		strings.Contains(lower, "/node_modules/") ||
		strings.Contains(lower, "/vendor/") ||
		strings.HasSuffix(lower, ".d.ts")
}

// sortNodesByImportance orders modules by relevance for the LLM prompt:
// core modules (most depended upon) > entry points > others.
func sortNodesByImportance(nodes []*grapher.Node, graph *grapher.Graph, entries []*grapher.Node) []*grapher.Node {
	entrySet := make(map[string]bool)
	for _, e := range entries {
		entrySet[e.Name] = true
	}

	type scoredNode struct {
		node  *grapher.Node
		score int
	}
	scored := make([]scoredNode, 0, len(nodes))
	for _, n := range nodes {
		if isBuildArtifact(n.Name) {
			continue
		}
		score := len(graph.DependentsOf(n.Name)) * 10
		if entrySet[n.Name] {
			score += 50
		}
		if len(n.Classes) > 0 || len(n.Functions) > 0 {
			score += 5
		}
		scored = append(scored, scoredNode{n, score})
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	result := make([]*grapher.Node, 0, len(scored))
	for _, s := range scored {
		result = append(result, s.node)
	}
	return result
}

func buildOverviewPrompt(graph *grapher.Graph, projectName, readme, language string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "你是一个资深软件架构师。请基于以下代码库信息，撰写一段项目概述（2-3 段）。\n\n")
	fmt.Fprintf(&b, "要求：\n")
	fmt.Fprintf(&b, "1. 描述这个项目的核心目标、主要功能和适用场景\n")
	fmt.Fprintf(&b, "2. 概括整体架构风格（如 MVC、微服务、单体、工具库等）\n")
	fmt.Fprintf(&b, "3. 说明关键模块的职责分工和协作方式\n")
	fmt.Fprintf(&b, "4. 不要只是罗列模块名称和文件清单，要体现对代码逻辑的理解\n")
	fmt.Fprintf(&b, "5. 每个段落末尾使用 `*来源：` 标注该段落涉及的核心文件，例如：\n")
	fmt.Fprintf(&b, "   本项目采用分层架构，将业务逻辑与数据访问解耦。\n")
	fmt.Fprintf(&b, "   *来源：`services/user_service.py`、`repositories/user_repository.py`*\n")
	fmt.Fprintf(&b, "6. 使用简体中文\n\n")

	if readme != "" {
		fmt.Fprintf(&b, "【项目 README】\n%s\n\n", readme)
	}

	fmt.Fprintf(&b, "项目：%s\n", projectName)
	fmt.Fprintf(&b, "模块数：%d\n", len(graph.Nodes))
	fmt.Fprintf(&b, "依赖数：%d\n", len(graph.Edges))
	if language != "" {
		fmt.Fprintf(&b, "编程语言：%s\n", language)
	}
	fmt.Fprintln(&b)

	if hint := languagePromptHint(language); hint != "" {
		fmt.Fprintf(&b, "【语言特性提示】\n%s\n\n", hint)
	}

	entries := graph.EntryPoints()
	important := sortNodesByImportance(graph.Nodes, graph, entries)
	maxModulesInPrompt := 20
	fmt.Fprintf(&b, "核心模块列表（按重要性排序，供参考，不要原样复述）：\n")
	for i, n := range important {
		if i >= maxModulesInPrompt {
			fmt.Fprintf(&b, "... 还有 %d 个模块未列出\n", len(important)-maxModulesInPrompt)
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

// collectRealIdentifiers gathers all real module/class/function/method names from the graph.
// It also registers path variants without file extensions so that LLM references like
// `commands/chat` are recognised when the real module is `src/commands/chat.ts`.
func collectRealIdentifiers(graph *grapher.Graph) map[string]bool {
	ids := make(map[string]bool)
	for _, n := range graph.Nodes {
		ids[n.Name] = true
		// Register extension-less path (e.g. src/commands/chat.ts → src/commands/chat)
		base := stripExtension(n.Name)
		if base != "" && base != n.Name {
			ids[base] = true
		}
		// Register every path segment so that LLM references like `cli`, `src`, `run`
		// (which are real directories or file basenames) are not flagged as hallucinations.
		for _, seg := range pathSegments(n.Name) {
			if seg != "" {
				ids[seg] = true
			}
		}
		for _, c := range n.Classes {
			ids[c.Name] = true
			for _, m := range c.Methods {
				ids[m.Name] = true
			}
		}
		for _, f := range n.Functions {
			ids[f.Name] = true
		}
	}
	return ids
}

// pathSegments splits a path into its slash-separated components.
func pathSegments(p string) []string {
	p = strings.ReplaceAll(p, "\\", "/")
	return strings.Split(p, "/")
}

// stripExtension returns the file path without its recognised source extension.
func stripExtension(path string) string {
	exts := []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".py", ".go", ".java", ".rs", ".cpp", ".c", ".h", ".hpp", ".cs", ".rb", ".php", ".swift", ".kt", ".scala", ".clj", ".erl", ".ex", ".exs"}
	lower := strings.ToLower(path)
	for _, ext := range exts {
		if strings.HasSuffix(lower, ext) {
			return path[:len(path)-len(ext)]
		}
	}
	return ""
}

// extractQuotedIdentifiers finds identifiers wrapped in backticks.
// We intentionally ignore bold markdown (**text**) because bold is typically
// used for natural-language emphasis (e.g. design pattern names), not code
// identifiers, and flagging those as hallucinations produces false positives.
//
// We also skip backtick phrases that look like natural-language concepts
// (e.g. Chinese architecture terms like `命令模式`) because LLMs legitimately
// use backticks for emphasis on technical terms, not just literal code ids.
func extractQuotedIdentifiers(text string) []string {
	seen := make(map[string]bool)
	var results []string

	// Backtick quotes: `Identifier`
	for i := 0; i < len(text); i++ {
		if text[i] == '`' {
			j := i + 1
			for j < len(text) && text[j] != '`' {
				j++
			}
			if j < len(text) {
				word := text[i+1 : j]
				word = strings.TrimSpace(word)
				if word != "" && !seen[word] && looksLikeCodeIdentifier(word) {
					seen[word] = true
					results = append(results, word)
				}
			}
			i = j
		}
	}

	return results
}

// looksLikeCodeIdentifier returns true for strings that resemble code
// identifiers or module paths. It rejects pure Chinese phrases which are
// almost always natural-language concept names, not literal code symbols.
func looksLikeCodeIdentifier(s string) bool {
	// If it contains a path separator or dot, it's likely a module reference.
	if strings.Contains(s, "/") || strings.Contains(s, ".") || strings.Contains(s, "\\") {
		return true
	}
	// Allow pure ASCII identifiers (camelCase, snake_case, PascalCase).
	// Reject strings that contain CJK or other non-ASCII characters — these
	// are typically architecture concept names the LLM puts in backticks.
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return len(s) > 0
}

// detectHallucination checks whether LLM-generated text references identifiers
// that do not exist in the codebase. Returns hallucinated names and a bool flag.
func detectHallucination(text string, graph *grapher.Graph) ([]string, bool) {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil, false
	}
	realIDs := collectRealIdentifiers(graph)
	quoted := extractQuotedIdentifiers(text)

	var hallucinated []string
	for _, id := range quoted {
		if isRealIdentifier(id, realIDs) {
			continue
		}
		hallucinated = append(hallucinated, id)
	}

	// Threshold: at least 8 hallucinated identifiers, or more than 60% of quoted ids.
	// We keep the bar high because LLMs legitimately use directory names, file
	// basenames, README concepts, and architecture terms in backticks that are
	// not literal code identifiers. Small samples (≤3 quoted ids) are ignored to
	// avoid one-off false positives (e.g. a single project acronym like `cmc`).
	if len(hallucinated) >= 8 {
		return hallucinated, true
	}
	if len(quoted) > 3 && float64(len(hallucinated))/float64(len(quoted)) > 0.6 {
		return hallucinated, true
	}
	return hallucinated, false
}

// isRealIdentifier returns true if id is a known code symbol or a recognisable
// path fragment of one (e.g. `commands/chat` matches `src/commands/chat.ts`).
func isRealIdentifier(id string, realIDs map[string]bool) bool {
	if realIDs[id] {
		return true
	}
	// For path-like ids, allow substring matching so that LLM shorthand paths
	// (e.g. `cli/src`, `examples/*`) don't trigger false positives.
	if strings.Contains(id, "/") || strings.Contains(id, "\\") {
		for real := range realIDs {
			if strings.HasSuffix(real, id) || strings.HasSuffix(id, real) || strings.Contains(real, id) {
				return true
			}
		}
		// Allow wildcard patterns like `examples/*` to match any path containing
		// the prefix (e.g. `examples/` inside a full path).
		if idx := strings.Index(id, "/*"); idx > 0 {
			prefix := id[:idx+1]
			for real := range realIDs {
				if strings.Contains(real, prefix) {
					return true
				}
			}
		}
	}
	return false
}

// buildAutoDescription generates a static project description based on graph analysis.
// languagePromptHint returns a language-specific hint for LLM prompts.
func languagePromptHint(language string) string {
	switch strings.ToLower(language) {
	case "python":
		return "该项目使用 Python。分析时可关注：装饰器模式、鸭子类型、FastAPI/Django/Flask 等 Web 框架惯例、PEP8 风格、以及 `__init__.py` 定义的包结构。"
	case "go", "golang":
		return "该项目使用 Go。分析时可关注：接口组合（implicit interface）、goroutine 与 channel 并发模式、error 值显式处理、标准库风格、以及 `cmd/` 和 `internal/` 包布局惯例。"
	case "javascript", "js", "typescript", "ts":
		return "该项目使用 JavaScript/TypeScript。分析时可关注：异步编程模式（Promise/async-await）、模块系统（CommonJS/ESM）、npm 生态、以及前端框架（React/Vue/Angular）或 Node.js 服务端架构。"
	case "java":
		return "该项目使用 Java。分析时可关注：面向对象设计、接口与抽象类、Spring 框架惯例、Maven/Gradle 模块结构、以及异常处理层次。"
	case "rust":
		return "该项目使用 Rust。分析时可关注：所有权与借用模型、trait 系统、Cargo 工作区结构、错误处理（Result/Option）、以及零成本抽象设计。"
	case "c++", "cpp":
		return "该项目使用 C++。分析时可关注：RAII 资源管理、模板元编程、STL 使用模式、头文件/源文件分离、以及内存安全策略。"
	default:
		return ""
	}
}

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

	// Use PageRank-based role inference for richer module description
	roles := graph.InferModuleRoles()
	if len(roles) > 0 {
		// Group by role
		roleGroups := make(map[string][]string)
		for _, r := range roles {
			roleGroups[r.Role] = append(roleGroups[r.Role], r.Name)
		}

		// Describe core domain first
		if coreList := roleGroups["核心领域"]; len(coreList) > 0 {
			b.WriteString("核心领域模块包括 ")
			for i, m := range coreList {
				if i > 0 {
					b.WriteString("、")
				}
				fmt.Fprintf(&b, "`%s`", m)
			}
			fmt.Fprintf(&b, "，被项目中多个其他模块所依赖，构成系统的业务核心。")
		}

		// Describe entry points
		if entryList := roleGroups["入口层"]; len(entryList) > 0 {
			b.WriteString("项目入口点为 ")
			for i, m := range entryList {
				if i > 0 {
					b.WriteString("、")
				}
				fmt.Fprintf(&b, "`%s`", m)
			}
			b.WriteString("。")
		}

		// Describe utilities
		if utilList := roleGroups["工具库"]; len(utilList) > 0 {
			b.WriteString("通用工具模块包括 ")
			for i, m := range utilList {
				if i > 0 {
					b.WriteString("、")
				}
				fmt.Fprintf(&b, "`%s`", m)
			}
			b.WriteString("，为各业务模块提供基础能力支撑。")
		}
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

func buildArchitecturePrompt(graph *grapher.Graph, language string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "分析以下模块依赖结构，用 2-3 段文字描述系统架构、设计模式及层级关系。\n\n")
	fmt.Fprintf(&b, "要求：\n")
	fmt.Fprintf(&b, "1. 描述整体架构分层、模块职责划分及关键设计模式\n")
	fmt.Fprintf(&b, "2. 每个段落末尾使用 `*来源：` 标注该段落涉及的核心文件，例如：\n")
	fmt.Fprintf(&b, "   系统采用分层架构，将控制器、服务和数据访问层解耦。\n")
	fmt.Fprintf(&b, "   *来源：`controllers/user.go`、`services/user.go`、`repositories/user.go`*\n")
	fmt.Fprintf(&b, "3. 使用简体中文\n\n")
	if language != "" {
		fmt.Fprintf(&b, "编程语言：%s\n", language)
	}
	if hint := languagePromptHint(language); hint != "" {
		fmt.Fprintf(&b, "语言特性提示：%s\n\n", hint)
	}
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

func buildWhatItDoesPrompt(graph *grapher.Graph, projectName, readme, language string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "你是一位资深技术作家，正在为一位新加入的开发者撰写项目介绍。\n\n")
	fmt.Fprintf(&b, "请基于以下代码库信息，撰写一篇\"%s 能做什么\"的介绍文章。\n\n", projectName)
	fmt.Fprintf(&b, "要求：\n")
	fmt.Fprintf(&b, "1. 用第一人称\"本项目\"或\"该系统\"的口吻\n")
	fmt.Fprintf(&b, "2. 描述项目解决的核心问题、目标用户、主要使用场景\n")
	fmt.Fprintf(&b, "3. 用表格总结核心能力（至少3列：能力、说明、对应模块）\n")
	fmt.Fprintf(&b, "4. 不要只是罗列模块名称，要体现\"用这些模块能完成什么任务\"\n")
	fmt.Fprintf(&b, "5. 如果可能，提及设计哲学或独特之处\n")
	fmt.Fprintf(&b, "6. 每个段落末尾使用 `*来源：` 标注该段落涉及的核心文件，例如：\n")
	fmt.Fprintf(&b, "   本项目提供用户认证与授权能力，支持多种登录方式。\n")
	fmt.Fprintf(&b, "   *来源：`auth/service.py`、`oauth/providers.py`*\n")
	fmt.Fprintf(&b, "7. 使用简体中文\n\n")

	if readme != "" {
		fmt.Fprintf(&b, "【项目 README】\n%s\n\n", readme)
	}

	entries := graph.EntryPoints()
	if len(entries) > 0 {
		fmt.Fprintf(&b, "【入口模块】\n")
		for _, e := range entries {
			fmt.Fprintf(&b, "- %s\n", e.Name)
		}
		fmt.Fprintln(&b)
	}

	roles := graph.InferModuleRoles()
	fmt.Fprintf(&b, "【模块角色推断】\n")
	for _, r := range roles {
		if r.Role == "核心领域" || r.Role == "入口层" {
			fmt.Fprintf(&b, "- %s（%s，得分 %.3f）\n", r.Name, r.Role, r.Score)
		}
	}
	fmt.Fprintln(&b)

	if hint := languagePromptHint(language); hint != "" {
		fmt.Fprintf(&b, "【语言特性提示】\n%s\n\n", hint)
	}

	return b.String()
}

func buildKeyConceptsPrompt(graph *grapher.Graph, projectName, language string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "你是一位资深软件架构师。请基于以下代码库信息，提炼该项目的\"关键设计决策与核心概念\"。\n\n")
	fmt.Fprintf(&b, "要求：\n")
	fmt.Fprintf(&b, "1. 找出 3-5 个最关键的设计决策或架构概念\n")
	fmt.Fprintf(&b, "2. 每个概念用一个小节说明：\"是什么\"、\"为什么这样设计\"、\"带来了什么好处/代价\"\n")
	fmt.Fprintf(&b, "3. 不要罗列所有模块，只聚焦真正有设计深度的决策点\n")
	fmt.Fprintf(&b, "4. 每个段落末尾使用 `*来源：` 标注该段落涉及的核心文件，例如：\n")
	fmt.Fprintf(&b, "   本项目采用命令模式将请求封装为对象，从而支持撤销和队列化操作。\n")
	fmt.Fprintf(&b, "   *来源：`commands/base.py`、`invoker/remote.py`*\n")
	fmt.Fprintf(&b, "5. 使用简体中文\n\n")
	fmt.Fprintf(&b, "例如好的输出：\n")
	fmt.Fprintf(&b, "## 命令行即 API 的设计哲学\n")
	fmt.Fprintf(&b, "gog 将每个 Google API 映射到统一的命令树... 这种设计使得...\n\n")
	fmt.Fprintf(&b, "## Safety Profile 的权限隔离机制\n")
	fmt.Fprintf(&b, "通过编译期和运行时的双重限制...\n\n")

	fmt.Fprintf(&b, "项目：%s\n", projectName)
	fmt.Fprintf(&b, "模块数：%d\n", len(graph.Nodes))
	fmt.Fprintf(&b, "依赖边数：%d\n\n", len(graph.Edges))

	// Provide architectural signals
	entries := graph.EntryPoints()
	if len(entries) > 0 {
		fmt.Fprintf(&b, "【入口模块】\n")
		for _, e := range entries {
			fmt.Fprintf(&b, "- %s\n", e.Name)
		}
		fmt.Fprintln(&b)
	}

	cycles := graph.DetectCycles()
	if len(cycles) > 0 {
		fmt.Fprintf(&b, "【循环依赖】\n")
		for _, c := range cycles {
			fmt.Fprintf(&b, "- %s\n", strings.Join(c.Nodes, " → "))
		}
		fmt.Fprintln(&b)
	}

	roles := graph.InferModuleRoles()
	fmt.Fprintf(&b, "【模块角色】\n")
	for _, r := range roles {
		if r.Role == "核心领域" || r.Role == "入口层" || r.Role == "工具库" {
			deps := graph.DependenciesOf(r.Name)
			dependents := graph.DependentsOf(r.Name)
			fmt.Fprintf(&b, "- %s（%s，依赖 %d 个模块，被 %d 个模块依赖）\n", r.Name, r.Role, len(deps), len(dependents))
		}
	}
	fmt.Fprintln(&b)

	if hint := languagePromptHint(language); hint != "" {
		fmt.Fprintf(&b, "【语言特性提示】\n%s\n\n", hint)
	}

	return b.String()
}

func buildLearningPathPrompt(graph *grapher.Graph, projectName, language string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "你是一位资深技术导师，正在为新加入的开发者设计一套个性化的学习路径。\n\n")
	fmt.Fprintf(&b, "请基于以下代码库信息，为 `%s` 撰写一份学习路径文档。\n\n", projectName)
	fmt.Fprintf(&b, "要求：\n")
	fmt.Fprintf(&b, "1. 用第二人称你/您的口吻，像导师一样给出建议\n")
	fmt.Fprintf(&b, "2. 设计至少 3 条不同目标的学习路径，例如：\n")
	fmt.Fprintf(&b, "   - 快速上手路径（30 分钟了解全貌）\n")
	fmt.Fprintf(&b, "   - 功能开发路径（如何添加一个新功能）\n")
	fmt.Fprintf(&b, "   - 深度理解路径（理解核心设计思想）\n")
	fmt.Fprintf(&b, "3. 每条路径包含具体的步骤、预计时间、难度标签（入门 / 进阶 / 深入）\n")
	fmt.Fprintf(&b, "4. 每个段落末尾使用 `*来源：` 标注该段落涉及的核心文件，例如：\n")
	fmt.Fprintf(&b, "   首先阅读入口模块，了解系统如何接收和分发请求。\n")
	fmt.Fprintf(&b, "   *来源：`cmd/main.go`、`server/router.go`*\n")
	fmt.Fprintf(&b, "5. 不要只是罗列文档链接，要体现为什么先读这个、再读那个的学习逻辑\n")
	fmt.Fprintf(&b, "6. 使用简体中文\n\n")

	fmt.Fprintf(&b, "项目：%s\n", projectName)
	fmt.Fprintf(&b, "模块数：%d\n", len(graph.Nodes))
	fmt.Fprintf(&b, "依赖边数：%d\n\n", len(graph.Edges))

	entries := graph.EntryPoints()
	if len(entries) > 0 {
		fmt.Fprintf(&b, "【入口模块】\n")
		for _, e := range entries {
			fmt.Fprintf(&b, "- %s\n", e.Name)
		}
		fmt.Fprintln(&b)
	}

	roles := graph.InferModuleRoles()
	fmt.Fprintf(&b, "【模块角色】\n")
	for _, r := range roles {
		if r.Role == "核心领域" || r.Role == "入口层" {
			fmt.Fprintf(&b, "- %s（%s）\n", r.Name, r.Role)
		}
	}
	fmt.Fprintln(&b)

	cycles := graph.DetectCycles()
	if len(cycles) > 0 {
		fmt.Fprintf(&b, "【循环依赖】\n")
		for _, c := range cycles {
			fmt.Fprintf(&b, "- %s\n", strings.Join(c.Nodes, " → "))
		}
		fmt.Fprintln(&b)
	}

	if hint := languagePromptHint(language); hint != "" {
		fmt.Fprintf(&b, "【语言特性提示】\n%s\n\n", hint)
	}

	return b.String()
}

// inferArchitecturePattern infers an architectural pattern and design rationale
// from the dependency graph.
func inferArchitecturePattern(graph *grapher.Graph) (pattern, rationale string) {
	roles := graph.InferModuleRoles()
	roleGroups := make(map[string][]string)
	for _, r := range roles {
		roleGroups[r.Role] = append(roleGroups[r.Role], r.Name)
	}

	pr := graph.PageRank()
	var maxPR float64
	for _, score := range pr {
		if score > maxPR {
			maxPR = score
		}
	}

	cycles := graph.DetectCycles()

	// Layered: distinct roles and no cycles
	if len(roleGroups) > 1 && len(cycles) == 0 {
		return "分层架构", "项目采用清晰的分层设计，将入口、核心业务与通用工具解耦。这种架构的优势在于：修改表层逻辑（如路由或 CLI）不会波及核心领域，工具函数可被多模块复用而不引入循环依赖。"
	}

	// Hub-and-spoke: one dominant module
	if maxPR > 0.4 && len(graph.Nodes) > 2 {
		return "中心辐射式", "项目的依赖结构呈中心辐射形态，存在一个高度中心化的核心模块。这种设计意味着系统的核心抽象集中在一处，便于统一维护，但也意味着该中心模块的变更会影响全局，需要特别注意向后兼容。"
	}

	// Pipeline: mostly linear with few branches
	if len(cycles) == 0 && len(graph.Nodes) > 2 {
		return "管道/流水线", "模块之间形成较为线性的依赖链，数据或控制流依次穿过多个处理阶段。这种设计适合具有明确阶段划定的业务（如解析→处理→输出），但中间任一环节的瓶颈都会影响整体吞吐。"
	}

	if len(cycles) > 0 {
		return "紧密耦合型", "模块之间存在循环依赖，说明当前架构中某些业务概念跨越了模块边界。这种结构在短期内可能加快开发速度，但长期会导致难以独立测试和重构。建议通过合并模块或引入抽象接口来打破循环。"
	}

	return "简洁模块化", "项目由少量模块组成，依赖关系简单直接。这种设计在小型项目中非常高效，但随着规模增长，建议关注职责分离，避免模块膨胀。"
}

// GenerateKeyConceptsFallback creates a statically-analysed "design decisions"
// document when the LLM is unavailable or fails.  It turns graph signals
// (roles, cycles, PageRank, entry points) into narrative-style concepts
// rather than a flat module list.
func GenerateKeyConceptsFallback(graph *grapher.Graph, projectName string) string {
	if len(graph.Nodes) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s 关键设计决策\n\n", projectName)
	b.WriteString("> 以下分析基于代码依赖关系和模块结构自动推断，从现有代码反推设计意图与架构决策。\n\n")

	pattern, rationale := inferArchitecturePattern(graph)
	b.WriteString("## 架构总览\n\n")
	fmt.Fprintf(&b, "从依赖结构来看，项目呈现出 **%s** 特征。%s\n\n", pattern, rationale)

	roles := graph.InferModuleRoles()
	roleGroups := make(map[string][]string)
	for _, r := range roles {
		roleGroups[r.Role] = append(roleGroups[r.Role], r.Name)
	}

	// Concept 1: Layered responsibility separation
	if len(roleGroups) > 1 {
		b.WriteString("## 模块职责分层\n\n")
		b.WriteString("项目通过目录和依赖关系形成了自然的职责分层，不同模块承担不同角色：\n\n")
		if coreList := roleGroups["核心领域"]; len(coreList) > 0 {
			b.WriteString("**核心领域模块** 构成业务逻辑主干，被多个上层模块依赖。包括 ")
			for i, m := range coreList {
				if i > 0 {
					b.WriteString("、")
				}
				fmt.Fprintf(&b, "`%s`", m)
			}
			fmt.Fprintf(&b, "，共 %d 个。\n\n", len(coreList))
		}
		if entryList := roleGroups["入口层"]; len(entryList) > 0 {
			b.WriteString("**入口层模块** 负责对接外部请求或命令，是系统的对外门面。包括 ")
			for i, m := range entryList {
				if i > 0 {
					b.WriteString("、")
				}
				fmt.Fprintf(&b, "`%s`", m)
			}
			fmt.Fprintf(&b, "，共 %d 个。\n\n", len(entryList))
		}
		if utilList := roleGroups["工具库"]; len(utilList) > 0 {
			b.WriteString("**工具库模块** 提供通用能力，通常被大量业务模块引用。包括 ")
			for i, m := range utilList {
				if i > 0 {
					b.WriteString("、")
				}
				fmt.Fprintf(&b, "`%s`", m)
			}
			fmt.Fprintf(&b, "，共 %d 个。\n\n", len(utilList))
		}
		b.WriteString("这种分层使得修改核心逻辑时影响范围可控，新增入口时无需改动底层实现。\n\n")
	}

	// Concept 2: Dependency flow — PageRank top modules as key abstractions
	pr := graph.PageRank()
	type prPair struct {
		name  string
		score float64
	}
	var prList []prPair
	for name, score := range pr {
		prList = append(prList, prPair{name, score})
	}
	sort.Slice(prList, func(i, j int) bool {
		return prList[i].score > prList[j].score
	})
	if len(prList) > 0 {
		b.WriteString("## 关键抽象与依赖流向\n\n")
		b.WriteString("PageRank 分析显示，以下模块在依赖网络中处于中心位置，是系统中最关键的抽象：\n\n")
		limit := 5
		if len(prList) < limit {
			limit = len(prList)
		}
		for i := 0; i < limit; i++ {
			p := prList[i]
			deps := graph.DependenciesOf(p.name)
			dependents := graph.DependentsOf(p.name)
			fmt.Fprintf(&b, "- **`%s`** — 中心度 %.3f，依赖 %d 个模块，被 %d 个模块依赖\n", p.name, p.score, len(deps), len(dependents))
		}
		b.WriteString("\n")
		b.WriteString("这些模块的稳定性直接影响整个系统的可维护性。修改前应评估对上游模块的波及范围。\n\n")
	}

	// Concept 3: Cycles as design tension
	cycles := graph.DetectCycles()
	if len(cycles) > 0 {
		b.WriteString("## 耦合张力点：循环依赖\n\n")
		b.WriteString("系统中存在循环依赖，说明以下模块之间存在紧密的双向耦合：\n\n")
		for _, c := range cycles {
			fmt.Fprintf(&b, "- %s\n", strings.Join(c.Nodes, " → "))
		}
		b.WriteString("\n")
		b.WriteString("循环依赖通常意味着这些模块在概念上应该合并，或者需要通过引入抽象接口来解耦。\n\n")
	}

	// Concept 4: Entry point pattern
	entries := graph.EntryPoints()
	if len(entries) > 0 {
		b.WriteString("## 入口与交互模式\n\n")
		b.WriteString("系统通过以下入口与外界交互：\n\n")
		for _, e := range entries {
			deps := graph.DependenciesOf(e.Name)
			fmt.Fprintf(&b, "- `%s` — 直接或间接驱动了 %d 个下游模块\n", e.Name, len(deps))
		}
		b.WriteString("\n")
		b.WriteString("理解这些入口的调用链路，是快速定位功能实现位置的最佳路径。\n\n")
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

	return b.String(), nil
}

// capabilityRow represents a themed capability for the WhatItDoes table.
type capabilityRow struct {
	name    string
	desc    string
	modules []string
}

// inferCapabilityFromName guesses a capability description from a module name and role.
func inferCapabilityFromName(name, role string) (string, string) {
	lower := strings.ToLower(name)

	// Entry-type inference
	if role == "入口层" || strings.Contains(lower, "main") || strings.Contains(lower, "app") {
		switch {
		case strings.Contains(lower, "api") || strings.Contains(lower, "route") || strings.Contains(lower, "router") || strings.Contains(lower, "endpoint"):
			return "API 服务", "接收并分发外部请求，定义系统对外接口"
		case strings.Contains(lower, "cli") || strings.Contains(lower, "cmd") || strings.Contains(lower, "command"):
			return "命令行交互", "提供终端命令接口，支持脚本化调用"
		case strings.Contains(lower, "web") || strings.Contains(lower, "server") || strings.Contains(lower, "http"):
			return "Web 服务", "运行 HTTP 服务，处理浏览器/客户端请求"
		case strings.Contains(lower, "gui") || strings.Contains(lower, "ui") || strings.Contains(lower, "view") || strings.Contains(lower, "frontend"):
			return "界面交互", "提供图形或 Web 界面，供用户直观操作"
		case strings.Contains(lower, "main") || strings.Contains(lower, "entry") || strings.Contains(lower, "start"):
			return "程序入口", "系统启动入口，负责初始化和流程编排"
		}
	}

	// Domain inference
	if role == "核心领域" {
		switch {
		case strings.Contains(lower, "user") || strings.Contains(lower, "auth") || strings.Contains(lower, "account") || strings.Contains(lower, "login") || strings.Contains(lower, "session") || strings.Contains(lower, "permission") || strings.Contains(lower, "role"):
			return "用户与认证", "管理用户身份、权限验证与会话状态"
		case strings.Contains(lower, "order") || strings.Contains(lower, "cart") || strings.Contains(lower, "purchase") || strings.Contains(lower, "payment") || strings.Contains(lower, "checkout") || strings.Contains(lower, "trade"):
			return "交易与订单", "处理订单生命周期、支付流程与交易状态"
		case strings.Contains(lower, "product") || strings.Contains(lower, "item") || strings.Contains(lower, "catalog") || strings.Contains(lower, "goods") || strings.Contains(lower, "sku"):
			return "商品与目录", "维护商品信息、分类体系与库存状态"
		case strings.Contains(lower, "message") || strings.Contains(lower, "chat") || strings.Contains(lower, "notification") || strings.Contains(lower, "email") || strings.Contains(lower, "sms") || strings.Contains(lower, "push"):
			return "消息通知", "负责消息发送、通知推送与通信管理"
		case strings.Contains(lower, "file") || strings.Contains(lower, "storage") || strings.Contains(lower, "upload") || strings.Contains(lower, "download") || strings.Contains(lower, "media") || strings.Contains(lower, "asset"):
			return "文件与存储", "管理文件上传、存储、下载与媒体资源"
		case strings.Contains(lower, "data") || strings.Contains(lower, "model") || strings.Contains(lower, "entity") || strings.Contains(lower, "schema") || strings.Contains(lower, "database") || strings.Contains(lower, "db") || strings.Contains(lower, "repository") || strings.Contains(lower, "dao"):
			return "数据模型", "定义数据实体、访问模式与持久化策略"
		case strings.Contains(lower, "config") || strings.Contains(lower, "setting") || strings.Contains(lower, "env") || strings.Contains(lower, "option"):
			return "配置管理", "管理系统配置、环境变量与运行时参数"
		case strings.Contains(lower, "search") || strings.Contains(lower, "index") || strings.Contains(lower, "query") || strings.Contains(lower, "filter") || strings.Contains(lower, "find"):
			return "搜索查询", "提供全文检索、索引构建与数据筛选能力"
		case strings.Contains(lower, "job") || strings.Contains(lower, "task") || strings.Contains(lower, "queue") || strings.Contains(lower, "worker") || strings.Contains(lower, "schedule") || strings.Contains(lower, "cron") || strings.Contains(lower, "background"):
			return "异步任务", "处理后台任务、定时调度与队列消费"
		case strings.Contains(lower, "report") || strings.Contains(lower, "analytics") || strings.Contains(lower, "stat") || strings.Contains(lower, "metric") || strings.Contains(lower, "dashboard"):
			return "报表分析", "生成统计报表、数据分析与指标监控"
		case strings.Contains(lower, "cache") || strings.Contains(lower, "redis") || strings.Contains(lower, "memo") || strings.Contains(lower, "buffer"):
			return "缓存加速", "提供数据缓存、热点加速与访问优化"
		case strings.Contains(lower, "log") || strings.Contains(lower, "trace") || strings.Contains(lower, "monitor") || strings.Contains(lower, "audit"):
			return "日志监控", "记录运行日志、链路追踪与异常告警"
		}
	}

	// Utility inference
	if role == "工具库" {
		switch {
		case strings.Contains(lower, "util") || strings.Contains(lower, "helper") || strings.Contains(lower, "common") || strings.Contains(lower, "shared") || strings.Contains(lower, "lib"):
			return "通用工具", "提供跨模块复用的工具函数与公共逻辑"
		case strings.Contains(lower, "test") || strings.Contains(lower, "mock") || strings.Contains(lower, "fixture") || strings.Contains(lower, "stub"):
			return "测试辅助", "提供测试固件、Mock 数据与断言工具"
		case strings.Contains(lower, "format") || strings.Contains(lower, "parse") || strings.Contains(lower, "serialize") || strings.Contains(lower, "encode") || strings.Contains(lower, "transform") || strings.Contains(lower, "convert"):
			return "数据转换", "负责格式解析、序列化与数据变换"
		case strings.Contains(lower, "valid") || strings.Contains(lower, "check") || strings.Contains(lower, "verify"):
			return "校验验证", "提供输入校验、规则验证与数据清洗"
		case strings.Contains(lower, "net") || strings.Contains(lower, "http") || strings.Contains(lower, "client") || strings.Contains(lower, "transport") || strings.Contains(lower, "conn"):
			return "网络通信", "封装网络请求、连接管理与协议处理"
		case strings.Contains(lower, "crypto") || strings.Contains(lower, "hash") || strings.Contains(lower, "sign") || strings.Contains(lower, "encrypt") || strings.Contains(lower, "security") || strings.Contains(lower, "secur"):
			return "安全加密", "提供加密、签名、哈希等安全机制"
		case strings.Contains(lower, "i18n") || strings.Contains(lower, "locale") || strings.Contains(lower, "lang") || strings.Contains(lower, "translate"):
			return "国际化", "支持多语言、本地化与区域适配"
		case strings.Contains(lower, "cache") || strings.Contains(lower, "redis") || strings.Contains(lower, "memo") || strings.Contains(lower, "buffer"):
			return "缓存加速", "提供数据缓存、热点加速与访问优化"
		case strings.Contains(lower, "log") || strings.Contains(lower, "trace") || strings.Contains(lower, "monitor") || strings.Contains(lower, "audit"):
			return "日志监控", "记录运行日志、链路追踪与异常告警"
		}
		return "通用支撑", "为业务模块提供基础技术能力"
	}

	// Fallback based on naming patterns
	if role == "核心领域" {
		return "业务处理", "承载项目核心业务逻辑与状态管理"
	}
	if role == "入口层" {
		return "请求处理", "接收外部输入并转发到业务层"
	}
	return "功能模块", "提供特定领域的功能实现"
}

// buildCapabilityTable aggregates modules into themed capability rows.
func buildCapabilityTable(entries []*grapher.Node, roles []grapher.ModuleRole) []capabilityRow {
	roleMap := make(map[string]string)
	for _, r := range roles {
		roleMap[r.Name] = r.Role
	}

	// Group by inferred capability name
	groups := make(map[string]*capabilityRow)

	// Process entries first
	for _, e := range entries {
		capName, capDesc := inferCapabilityFromName(e.Name, roleMap[e.Name])
		if g, ok := groups[capName]; ok {
			g.modules = append(g.modules, e.Name)
		} else {
			groups[capName] = &capabilityRow{name: capName, desc: capDesc, modules: []string{e.Name}}
		}
	}

	// Process core modules
	for _, r := range roles {
		if r.Role != "核心领域" {
			continue
		}
		capName, capDesc := inferCapabilityFromName(r.Name, r.Role)
		if g, ok := groups[capName]; ok {
			// Only add if not already from entry point (avoid duplicates)
			found := false
			for _, m := range g.modules {
				if m == r.Name {
					found = true
					break
				}
			}
			if !found {
				g.modules = append(g.modules, r.Name)
			}
		} else {
			groups[capName] = &capabilityRow{name: capName, desc: capDesc, modules: []string{r.Name}}
		}
	}

	// Aggregate utilities into a single row if there are many
	var utilModules []string
	for _, r := range roles {
		if r.Role == "工具库" {
			utilModules = append(utilModules, r.Name)
		}
	}
	if len(utilModules) > 0 {
		groups["通用技术支撑"] = &capabilityRow{
			name:    "通用技术支撑",
			desc:    "提供日志、配置、工具函数等跨模块基础设施",
			modules: utilModules,
		}
	}

	// Sort rows: entries first, then core, then utility
	var result []capabilityRow
	seen := make(map[string]bool)
	for _, e := range entries {
		capName, _ := inferCapabilityFromName(e.Name, roleMap[e.Name])
		if !seen[capName] {
			seen[capName] = true
			if g, ok := groups[capName]; ok {
				result = append(result, *g)
			}
		}
	}
	for _, r := range roles {
		if r.Role != "核心领域" {
			continue
		}
		capName, _ := inferCapabilityFromName(r.Name, r.Role)
		if !seen[capName] {
			seen[capName] = true
			if g, ok := groups[capName]; ok {
				result = append(result, *g)
			}
		}
	}
	if g, ok := groups["通用技术支撑"]; ok && !seen["通用技术支撑"] {
		result = append(result, *g)
	}

	return result
}

// GenerateWhatItDoesMarkdown creates a narrative article describing what the project does.
// When LLM fails, this static fallback produces theme-oriented content with a capability
// table rather than a flat module list.
func GenerateWhatItDoesMarkdown(graph *grapher.Graph, projectName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s 能做什么\n\n", projectName)

	entries := graph.EntryPoints()
	roles := graph.InferModuleRoles()
	rows := buildCapabilityTable(entries, roles)

	if len(rows) == 0 {
		b.WriteString("该项目暂无足够信息生成能力说明。\n")
		return b.String()
	}

	// Project positioning narrative
	b.WriteString("## 项目定位\n\n")
	var scale string
	if len(graph.Nodes) <= 5 {
		scale = "小型"
	} else if len(graph.Nodes) <= 15 {
		scale = "中型"
	} else {
		scale = "大型"
	}

	if len(entries) > 0 {
		var entryCaps []string
		seenCap := make(map[string]bool)
		for _, e := range entries {
			capName, _ := inferCapabilityFromName(e.Name, "")
			if !seenCap[capName] {
				seenCap[capName] = true
				entryCaps = append(entryCaps, capName)
			}
		}
		fmt.Fprintf(&b, "%s 是一个 %s 项目，通过 %s 对外提供服务。",
			projectName, scale, strings.Join(entryCaps, "、"))
	} else {
		fmt.Fprintf(&b, "%s 是一个 %s 项目。", projectName, scale)
	}

	// Count core capabilities
	var coreModules []string
	for _, r := range roles {
		if r.Role == "核心领域" {
			coreModules = append(coreModules, r.Name)
		}
	}
	if len(coreModules) > 0 {
		fmt.Fprintf(&b, "核心业务逻辑集中在 %s 等模块。", strings.Join(takeFirstN(coreModules, 3), "、"))
	}
	b.WriteString("\n\n")

	// Capability table
	b.WriteString("## 核心能力\n\n")
	b.WriteString("| 能力 | 说明 | 涉及模块 |\n")
	b.WriteString("|------|------|----------|\n")
	for _, row := range rows {
		moduleStr := ""
		for i, m := range row.modules {
			if i > 0 {
				moduleStr += "、"
			}
			moduleStr += fmt.Sprintf("`%s`", m)
		}
		fmt.Fprintf(&b, "| %s | %s | %s |\n", row.name, row.desc, moduleStr)
	}
	b.WriteString("\n")

	// Architecture notes
	cycles := graph.DetectCycles()
	if len(cycles) > 0 {
		b.WriteString("## 架构注意事项\n\n")
		b.WriteString("项目中存在以下模块间的循环依赖，阅读时需特别关注：\n\n")
		for _, c := range cycles {
			fmt.Fprintf(&b, "- `%s`\n", strings.Join(c.Nodes, "` → `"))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// takeFirstN returns the first n elements of a string slice.
func takeFirstN(ss []string, n int) []string {
	if len(ss) <= n {
		return ss
	}
	return ss[:n]
}

// GenerateProjectStructureMarkdown creates a project structure guide with responsibilities.
func GenerateProjectStructureMarkdown(graph *grapher.Graph, projectName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s 项目结构\n\n", projectName)

	b.WriteString("## 目录概览\n\n")
	b.WriteString("```\n")
	b.WriteString(buildProjectTree(graph))
	b.WriteString("```\n\n")

	// Module responsibilities table
	b.WriteString("## 模块职责\n\n")
	b.WriteString("| 模块 | 角色 | 职责说明 |\n")
	b.WriteString("|------|------|----------|\n")

	roles := graph.InferModuleRoles()
	for _, r := range roles {
		var desc string
		switch r.Role {
		case "核心领域":
			desc = "承载核心业务逻辑，被多个模块依赖"
		case "入口层":
			desc = "程序入口，负责请求接收和初步分发"
		case "工具库":
			desc = "提供通用技术能力，无业务耦合"
		case "独立模块":
			desc = "独立运行的功能单元"
		case "支撑模块":
			desc = "为特定模块提供辅助能力"
		default:
			desc = "业务功能模块"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s |\n", r.Name, r.Role, desc)
	}
	b.WriteString("\n")

	// Key dependencies explanation
	b.WriteString("## 关键依赖关系\n\n")
	b.WriteString("```mermaid\ngraph TD\n")
	// Include top-level dependencies only for clarity
	shown := make(map[string]bool)
	for _, e := range graph.Edges {
		if !shown[e.From+e.To] {
			fmt.Fprintf(&b, "    %s --> %s\n", mermaidEscape(e.From), mermaidEscape(e.To))
			shown[e.From+e.To] = true
		}
	}
	b.WriteString("```\n\n")

	return b.String()
}

func buildProjectTree(graph *grapher.Graph) string {
	var b strings.Builder
	groups := graph.GroupByDirectory()
	var dirs []string
	for d := range groups {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	for _, dir := range dirs {
		if dir == "." || dir == "" {
			continue
		}
		fmt.Fprintf(&b, "%s/\n", dir)
		for _, n := range groups[dir] {
			base := filepath.Base(n.Name)
			if base == "" {
				base = n.Name
			}
			fmt.Fprintf(&b, "  %s\n", base)
		}
	}
	return b.String()
}

func mermaidEscape(s string) string {
	// Simple escape for mermaid node IDs
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, ".", "_")
	s = strings.ReplaceAll(s, "/", "_")
	return s
}

// GenerateLearningPathMarkdown creates a learning path guide for new developers.
// It provides three difficulty-labelled branches tailored to different goals.
func GenerateLearningPathMarkdown(graph *grapher.Graph, projectName string, hasConcepts bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s 学习路径\n\n", projectName)

	b.WriteString("> 根据你的目标，选择最适合的阅读路径。\n\n")

	// Estimate reading time based on project scale
	var quickTime, deepTime, contribTime string
	nodeCount := len(graph.Nodes)
	switch {
	case nodeCount <= 5:
		quickTime, deepTime, contribTime = "10 分钟", "30 分钟", "1-2 小时"
	case nodeCount <= 15:
		quickTime, deepTime, contribTime = "20 分钟", "1 小时", "半天"
	default:
		quickTime, deepTime, contribTime = "30 分钟", "2 小时", "1 天以上"
	}

	b.WriteString("## 快速上手\n\n")
	b.WriteString("如果你刚加入这个项目，建议按以下顺序阅读：\n\n")
	b.WriteString("1. **[项目能做什么](what-it-does.md)** — 了解项目解决什么问题和核心能力\n")
	b.WriteString("2. **[项目结构](project-structure.md)** — 熟悉代码组织方式和模块职责\n")
	b.WriteString("3. **[架构说明](architecture.md)** — 理解系统分层和关键设计决策\n")
	if hasConcepts {
		b.WriteString("4. **[核心概念](key-concepts.md)** — 深入理解关键设计思想\n")
	}
	b.WriteString("\n")

	// Three goal-based branches with difficulty labels
	b.WriteString("## 按目标选择\n\n")
	b.WriteString("### 分支一：快速熟悉 ⭐ 入门\n\n")
	fmt.Fprintf(&b, "适合新团队成员、代码评审者或想快速了解项目定位的读者。预计耗时 **%s**。\n\n", quickTime)
	b.WriteString("1. [项目概述](overview.md) — 先建立全局认知\n")
	b.WriteString("2. [项目能做什么](what-it-does.md) — 理解核心能力边界\n")
	b.WriteString("3. [架构说明](architecture.md) — 可视化模块关系\n")
	b.WriteString("\n")

	b.WriteString("### 分支二：深度理解 ⭐⭐ 进阶\n\n")
	fmt.Fprintf(&b, "适合需要维护代码、排查问题或进行二次开发的开发者。预计耗时 **%s**。\n\n", deepTime)
	b.WriteString("1. [项目结构](project-structure.md) — 掌握目录与模块职责\n")
	if hasConcepts {
		b.WriteString("2. [核心概念](key-concepts.md) — 理解关键设计决策（含类图）\n")
	} else {
		b.WriteString("2. [架构说明](architecture.md) — 理解分层与设计思想\n")
	}
	b.WriteString("3. [学习路径](learning-path.md) — 追踪调用流程（含时序图）\n")
	b.WriteString("4. [API 参考](api-reference.md) — 掌握公开接口\n")
	b.WriteString("\n")

	b.WriteString("### 分支三：贡献代码 ⭐⭐⭐ 专家\n\n")
	fmt.Fprintf(&b, "适合准备提交 PR、重构架构或深入优化性能的开发者。预计耗时 **%s**。\n\n", contribTime)
	b.WriteString("1. [代码入口](#代码入口) — 从启动点开始追踪执行链路\n")
	b.WriteString("2. [关键抽象与依赖流向](key-concepts.md) — 识别稳定性核心\n")
	pr := graph.PageRank()
	if len(pr) > 0 {
		b.WriteString("3. 精读 PageRank 最高的中心模块源码 — 这些模块的变更影响面最大\n")
	}
	cycles := graph.DetectCycles()
	if len(cycles) > 0 {
		b.WriteString("4. 分析循环依赖模块 — 评估解耦或合并的可行性\n")
	}
	b.WriteString("\n")

	// Entry points as starting points
	entries := graph.EntryPoints()
	if len(entries) > 0 {
		b.WriteString("## 代码入口\n\n")
		b.WriteString("从以下入口文件开始阅读代码：\n\n")
		for _, e := range entries {
			fmt.Fprintf(&b, "- `%s`\n", e.Name)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// inferModuleDifficulty assigns a difficulty label based on PageRank, dependents,
// code size, and dependency depth using percentile thresholds.
func inferModuleDifficulty(n *grapher.Node, graph *grapher.Graph) string {
	pr := graph.PageRank()
	type scored struct {
		name  string
		score float64
	}
	var all []scored
	for _, node := range graph.Nodes {
		score := pr[node.Name] * 50
		score += float64(len(graph.DependentsOf(node.Name))) * 8
		codeSize := len(node.Functions)
		for _, c := range node.Classes {
			codeSize += len(c.Methods)
		}
		score += float64(codeSize) * 2
		score += float64(len(graph.DependenciesOf(node.Name))) * 1
		all = append(all, scored{name: node.Name, score: score})
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].score > all[j].score
	})

	var rank int
	for i, s := range all {
		if s.name == n.Name {
			rank = i
			break
		}
	}
	percentile := float64(rank) / float64(len(all))
	switch {
	case percentile < 0.25:
		return "⭐⭐⭐ 深入"
	case percentile < 0.75:
		return "⭐⭐ 进阶"
	default:
		return "⭐ 入门"
	}
}

// GenerateModuleDocs creates per-module documentation mapping module name to markdown content.
// Each document describes the module's responsibility, dependencies, classes, and functions.
func GenerateModuleDocs(graph *grapher.Graph) map[string]string {
	if len(graph.Nodes) == 0 {
		return nil
	}

	roles := graph.InferModuleRoles()
	roleMap := make(map[string]string, len(roles))
	for _, r := range roles {
		roleMap[r.Name] = r.Role
	}

	docs := make(map[string]string, len(graph.Nodes))
	for _, n := range graph.Nodes {
		var b strings.Builder
		b.WriteString(fmt.Sprintf("# %s\n\n", n.Name))

		// Difficulty
		b.WriteString(fmt.Sprintf("**难度级别**：%s\n\n", inferModuleDifficulty(n, graph)))

		// Role
		if role := roleMap[n.Name]; role != "" {
			b.WriteString(fmt.Sprintf("**架构角色**：%s\n\n", role))
		}

		// Responsibility (static inference from filename)
		b.WriteString("## 职责说明\n\n")
		b.WriteString(inferModuleResponsibility(n))
		b.WriteString("\n\n")

		// Dependencies
		deps := graph.DependenciesOf(n.Name)
		if len(deps) > 0 {
			sort.Strings(deps)
			b.WriteString("## 依赖模块\n\n")
			for _, d := range deps {
				b.WriteString(fmt.Sprintf("- `%s`\n", d))
			}
			b.WriteString("\n")
		}

		dependents := graph.DependentsOf(n.Name)
		if len(dependents) > 0 {
			sort.Strings(dependents)
			b.WriteString("## 被依赖模块\n\n")
			for _, d := range dependents {
				b.WriteString(fmt.Sprintf("- `%s`\n", d))
			}
			b.WriteString("\n")
		}

		// Classes
		if len(n.Classes) > 0 {
			b.WriteString("## 类定义\n\n")
			for _, c := range n.Classes {
				b.WriteString(fmt.Sprintf("### %s\n\n", c.Name))
				if len(c.Bases) > 0 {
					b.WriteString(fmt.Sprintf("继承自：%s\n\n", strings.Join(c.Bases, ", ")))
				}
				if len(c.Methods) > 0 {
					b.WriteString("| 方法 | 参数 | 返回值 |\n")
					b.WriteString("|------|------|--------|\n")
					for _, m := range c.Methods {
						params := strings.Join(m.Params, ", ")
						params = stripSelfParamStr(params)
						b.WriteString(fmt.Sprintf("| `%s` | %s | %s |\n", m.Name, params, m.ReturnType))
					}
					b.WriteString("\n")
				}
			}
		}

		// Functions
		if len(n.Functions) > 0 {
			b.WriteString("## 函数列表\n\n")
			b.WriteString("| 函数 | 参数 | 返回值 | 职责 |\n")
			b.WriteString("|------|------|--------|------|\n")
			for _, f := range n.Functions {
				params := strings.Join(f.Params, ", ")
				desc := describeFunction(f.Name, f.Params, f.ReturnType)
				b.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s |\n", f.Name, params, f.ReturnType, desc))
			}
			b.WriteString("\n")
		}

		docs[n.Name] = b.String()
	}

	return docs
}

// groupModulesByTheme groups modules into thematic categories based on filename heuristics.
func groupModulesByTheme(graph *grapher.Graph) map[string][]*grapher.Node {
	groups := make(map[string][]*grapher.Node)
	for _, n := range graph.Nodes {
		name := strings.ToLower(n.Name)
		theme := "其他"
		switch {
		case strings.Contains(name, "cmd") || strings.Contains(name, "main") || strings.Contains(name, "entry") || strings.Contains(name, "bootstrap"):
			theme = "入口与命令行"
		case strings.Contains(name, "api") || strings.Contains(name, "handler") || strings.Contains(name, "route") || strings.Contains(name, "router") || strings.Contains(name, "controller") || strings.Contains(name, "gateway"):
			theme = "接口与路由"
		case strings.Contains(name, "service") || strings.Contains(name, "usecase") || strings.Contains(name, "biz") || strings.Contains(name, "logic") || strings.Contains(name, "workflow") || strings.Contains(name, "processor"):
			theme = "业务逻辑与服务"
		case strings.Contains(name, "model") || strings.Contains(name, "entity") || strings.Contains(name, "domain") || strings.Contains(name, "schema") || strings.Contains(name, "types") || strings.Contains(name, "dto") || strings.Contains(name, "vo"):
			theme = "数据模型与实体"
		case strings.Contains(name, "repo") || strings.Contains(name, "dao") || strings.Contains(name, "db") || strings.Contains(name, "database") || strings.Contains(name, "store") || strings.Contains(name, "storage") || strings.Contains(name, "persist"):
			theme = "数据访问与存储"
		case strings.Contains(name, "config") || strings.Contains(name, "middleware") || strings.Contains(name, "client") || strings.Contains(name, "server") || strings.Contains(name, "transport") || strings.Contains(name, "infra") || strings.Contains(name, "platform") || strings.Contains(name, "core"):
			theme = "配置与基础设施"
		case strings.Contains(name, "util") || strings.Contains(name, "helper") || strings.Contains(name, "common") || strings.Contains(name, "shared") || strings.Contains(name, "lib") || strings.Contains(name, "toolkit") || strings.Contains(name, "extension") || strings.Contains(name, "plugin"):
			theme = "工具与辅助函数"
		case strings.Contains(name, "test") || strings.Contains(name, "spec") || strings.Contains(name, "mock") || strings.Contains(name, "fixture") || strings.Contains(name, "benchmark"):
			theme = "测试与验证"
		case strings.Contains(name, "view") || strings.Contains(name, "ui") || strings.Contains(name, "component") || strings.Contains(name, "page") || strings.Contains(name, "template") || strings.Contains(name, "front"):
			theme = "视图与 UI"
		}
		groups[theme] = append(groups[theme], n)
	}
	// Sort nodes within each group by name for stable output
	for _, nodes := range groups {
		sort.Slice(nodes, func(i, j int) bool {
			return nodes[i].Name < nodes[j].Name
		})
	}
	return groups
}

func inferModuleResponsibility(n *grapher.Node) string {
	// Simple heuristic based on directory and filename
	name := strings.ToLower(n.Name)
	switch {
	case strings.Contains(name, "test"):
		return "该模块包含测试代码，用于验证项目功能的正确性。"
	case strings.Contains(name, "cmd") || strings.Contains(name, "main"):
		return "该模块是项目的入口或命令行接口，负责解析参数并启动核心流程。"
	case strings.Contains(name, "api") || strings.Contains(name, "handler") || strings.Contains(name, "route"):
		return "该模块负责对外提供接口或路由处理，是系统与外部交互的边界。"
	case strings.Contains(name, "model") || strings.Contains(name, "entity") || strings.Contains(name, "domain"):
		return "该模块定义核心业务实体与领域模型，承载系统的核心数据结构。"
	case strings.Contains(name, "service") || strings.Contains(name, "usecase") || strings.Contains(name, "biz"):
		return "该模块包含业务逻辑与服务编排，协调各组件完成具体业务功能。"
	case strings.Contains(name, "repo") || strings.Contains(name, "dao") || strings.Contains(name, "store"):
		return "该模块负责数据持久化与存储访问，封装对数据库或文件系统的操作。"
	case strings.Contains(name, "util") || strings.Contains(name, "helper") || strings.Contains(name, "common"):
		return "该模块提供通用工具函数与辅助逻辑，供其他模块复用。"
	case strings.Contains(name, "config") || strings.Contains(name, "setting"):
		return "该模块管理配置读取与环境适配，为系统运行提供参数支持。"
	case strings.Contains(name, "middleware") || strings.Contains(name, "intercept"):
		return "该模块实现横切关注点（如日志、认证、限流），在请求处理链中生效。"
	case strings.Contains(name, "client"):
		return "该模块封装对外部服务或资源的客户端访问逻辑。"
	case strings.Contains(name, "server"):
		return "该模块实现服务端监听与请求处理，是系统的网络入口。"
	case strings.Contains(name, "view") || strings.Contains(name, "ui") || strings.Contains(name, "component"):
		return "该模块负责界面展示或视图组件渲染，面向用户交互层。"
	case strings.Contains(name, "db") || strings.Contains(name, "database") || strings.Contains(name, "migration"):
		return "该模块处理数据库连接、迁移或 schema 管理。"
	case strings.Contains(name, "cache") || strings.Contains(name, "redis") || strings.Contains(name, "memo"):
		return "该模块实现缓存策略与高速数据存取，提升系统响应性能。"
	case strings.Contains(name, "queue") || strings.Contains(name, "worker") || strings.Contains(name, "job"):
		return "该模块负责任务队列管理与异步工作流调度。"
	case strings.Contains(name, "grpc") || strings.Contains(name, "proto") || strings.Contains(name, "rpc"):
		return "该模块定义或实现远程过程调用（RPC）接口与协议序列化。"
	case strings.Contains(name, "schema") || strings.Contains(name, "types") || strings.Contains(name, "dto"):
		return "该模块声明数据结构、类型定义或数据传输对象（DTO）。"
	case strings.Contains(name, "logger") || strings.Contains(name, "log") || strings.Contains(name, "monitor"):
		return "该模块提供日志记录、监控埋点或可观测性支持。"
	default:
		return "该模块是项目的组成部分，承担特定的业务或技术职责。"
	}
}

// stripSelfParamStr removes 'self' and 'cls' from parameter lists for cleaner docs.
func stripSelfParamStr(params string) string {
	var parts []string
	for _, p := range strings.Split(params, ", ") {
		if p != "self" && p != "cls" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ", ")
}

// GenerateAPIReferenceMarkdown creates an API reference from classes and functions.
func GenerateAPIReferenceMarkdown(graph *grapher.Graph, llmDesc map[string]string) (string, error) {
	var b strings.Builder
	b.WriteString("# API 参考\n\n")

	if len(graph.Nodes) == 0 {
		b.WriteString("未找到 API 符号。\n")
		return b.String(), nil
	}

	resolveDesc := func(module, name, params, returnType string) string {
		key := module + "#" + name
		if d, ok := llmDesc[key]; ok && d != "" {
			return d
		}
		return describeFunction(name, strings.Split(params, ", "), returnType)
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
		// Deduplicate classes by name, keeping the one with the most methods.
		type classInfo struct {
			info   analyzer.ClassInfo
			source string
		}
		classMap := make(map[string]classInfo)
		for _, n := range graph.Nodes {
			for _, c := range n.Classes {
				if existing, ok := classMap[c.Name]; !ok || len(c.Methods) > len(existing.info.Methods) {
					classMap[c.Name] = classInfo{info: c, source: n.Name}
				}
			}
		}
		var classNames []string
		for name := range classMap {
			classNames = append(classNames, name)
		}
		sort.Strings(classNames)
		for _, name := range classNames {
			c := classMap[name]
			b.WriteString(fmt.Sprintf("### %s\n\n", c.info.Name))
			if len(c.info.Bases) > 0 {
				b.WriteString(fmt.Sprintf("**继承**：%s\n\n", strings.Join(c.info.Bases, ", ")))
			}
			b.WriteString(fmt.Sprintf("*来源：`%s`*\n\n", c.source))
			if len(c.info.Methods) > 0 {
				b.WriteString("#### 方法\n\n")
				for _, m := range c.info.Methods {
					sig := formatSignature(m.Name, m.Params, m.ReturnType)
					desc := resolveDesc(name, m.Name, strings.Join(m.Params, ", "), m.ReturnType)
					b.WriteString(fmt.Sprintf("- `%s` — %s\n", sig, desc))
				}
				b.WriteString("\n")
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
				desc := resolveDesc(n.Name, f.Name, strings.Join(f.Params, ", "), f.ReturnType)
				b.WriteString(fmt.Sprintf("```\n%s\n```\n\n", sig))
				b.WriteString(fmt.Sprintf("%s\n\n", desc))
				b.WriteString(fmt.Sprintf("*来源：`%s`*\n\n", n.Name))
			}
		}
	}

	return b.String(), nil
}

// GenerateArchitectureMarkdown creates an architecture document with embedded diagrams.
// If narrative is non-empty, it is placed before the module overview table.
func GenerateArchitectureMarkdown(graph *grapher.Graph, archDSL, narrative string) (string, error) {
	var b strings.Builder
	b.WriteString("# 架构\n\n")

	if narrative != "" {
		b.WriteString(narrative)
		b.WriteString("\n\n")
	}

	// Module overview table
	b.WriteString("## 模块概览\n\n")
	// Module overview table with roles
	roles := graph.InferModuleRoles()
	roleMap := make(map[string]string)
	for _, r := range roles {
		roleMap[r.Name] = r.Role
	}

	b.WriteString("| 模块 | 角色 | 类型 | 依赖 | 被依赖 |\n")
	b.WriteString("|------|------|------|------|--------|\n")

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

		role := roleMap[n.Name]
		if role == "" {
			role = "—"
		}

		b.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s | %s |\n", n.Name, role, nodeType, depsStr, depStr))
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

// fileTitleMap maps known wiki filenames to human-readable titles.
var fileTitleMap = map[string]string{
	"00-overview.md":          "项目概述",
	"01-what-it-does.md":      "项目能做什么",
	"02-architecture.md":      "架构说明",
	"03-project-structure.md": "项目结构",
	"04-key-concepts.md":      "核心概念与设计决策",
	"05-learning-path.md":     "学习路径",
	"api-reference.md":        "API 参考",
	"compilation.md":          "Wiki 合辑",
}

// addFrontmatter prepends YAML frontmatter to Markdown content.
func addFrontmatter(filename, content, projectName string, moduleCount int) string {
	title := fileTitleMap[filename]
	if title == "" {
		// Try to extract from first heading
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "# ") {
				title = strings.TrimPrefix(trimmed, "# ")
				break
			}
		}
	}
	if title == "" {
		title = filename
	}

	fm := fmt.Sprintf("---\ntitle: %q\nproject: %q\ngenerated_at: %q\nsource_modules: %d\n---\n\n",
		title, projectName, time.Now().Format(time.RFC3339), moduleCount)
	return fm + content
}

// backupWikiFiles archives existing wiki files before overwriting.
func backupWikiFiles(outputDir string) error {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	hasFiles := false
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "modules" {
			hasFiles = true
			break
		}
	}
	if !hasFiles {
		return nil
	}

	historyDir := filepath.Join(outputDir, "..", "history")
	timestamp := time.Now().Format("20060102-150405")
	backupDir := filepath.Join(historyDir, timestamp)
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("create backup dir: %w", err)
	}
	if err := copyDir(outputDir, backupDir); err != nil {
		return fmt.Errorf("backup files: %w", err)
	}
	if err := cleanupOldHistory(historyDir, 10); err != nil {
		fmt.Printf("警告：清理旧版本历史失败 (%v)\n", err)
	}
	return nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dstPath, data, info.Mode())
	})
}

func cleanupOldHistory(historyDir string, keep int) error {
	entries, err := os.ReadDir(historyDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var versions []os.DirEntry
	for _, e := range entries {
		if e.IsDir() {
			versions = append(versions, e)
		}
	}
	if len(versions) <= keep {
		return nil
	}
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Name() < versions[j].Name()
	})
	for i := 0; i < len(versions)-keep; i++ {
		oldDir := filepath.Join(historyDir, versions[i].Name())
		if err := os.RemoveAll(oldDir); err != nil {
			fmt.Printf("警告：删除旧版本 %s 失败 (%v)\n", oldDir, err)
		}
	}
	return nil
}

// WriteWikiFiles writes all Wiki artifacts to the output directory.
func WriteWikiFiles(outputDir string, wiki *Wiki, graph *grapher.Graph) error {
	if err := backupWikiFiles(outputDir); err != nil {
		fmt.Printf("警告：备份旧版本失败 (%v)\n", err)
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	// Learning guide files (narrative, read in order)
	files := map[string]string{
		"00-overview.md":          wiki.Overview,
		"01-what-it-does.md":      wiki.WhatItDoes,
		"02-architecture.md":      wiki.Architecture,
		"03-project-structure.md": wiki.ProjectStructure,
	}
	if wiki.KeyConcepts != "" {
		files["04-key-concepts.md"] = "# 核心概念与设计决策\n\n" + wiki.KeyConcepts
	}
	files["05-learning-path.md"] = wiki.LearningPath

	// Reference files (lookup when needed)
	files["api-reference.md"] = wiki.APIReference

	// Diagrams are now embedded inside their thematic articles
	// (architecture.md, key-concepts.md, learning-path.md) via inline
	// mermaid code blocks. No standalone .mmd files are written.

	// Compilation and HTML
	files["compilation.md"] = GenerateMarkdownCompilation(wiki)
	files["index.html"] = GenerateStaticHTML(wiki)

	moduleCount := len(wiki.ModuleDocs)

	for name, content := range files {
		path := filepath.Join(outputDir, name)
		out := content
		if strings.HasSuffix(name, ".md") {
			out = addFrontmatter(name, content, wiki.ProjectName, moduleCount)
		}
		if err := os.WriteFile(path, []byte(out), 0644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}

	// Module docs
	if len(wiki.ModuleDocs) > 0 {
		modulesDir := filepath.Join(outputDir, "modules")
		if err := os.MkdirAll(modulesDir, 0755); err != nil {
			return fmt.Errorf("create modules dir: %w", err)
		}

		// Generate modules/README.md theme index
		if graph != nil && len(wiki.ModuleThemes) > 0 {
			var idx strings.Builder
			idx.WriteString("# 模块索引\n\n")
			idx.WriteString("> 按功能主题聚合的模块导航。点击模块名查看详细文档。\n\n")
			nodeMap := make(map[string]*grapher.Node)
			for _, n := range graph.Nodes {
				nodeMap[n.Name] = n
			}
			for _, theme := range sortedThemeKeys(wiki.ModuleThemes) {
				idx.WriteString(fmt.Sprintf("## %s\n\n", theme))
				idx.WriteString("| 模块 | 难度 | 职责 |\n")
				idx.WriteString("|------|------|------|\n")
				for _, name := range wiki.ModuleThemes[theme] {
					if _, ok := wiki.ModuleDocs[name]; !ok {
						continue
					}
					diff := "—"
					resp := "—"
					if n, ok := nodeMap[name]; ok {
						diff = inferModuleDifficulty(n, graph)
						resp = inferModuleResponsibility(n)
					}
					safe := strings.ReplaceAll(name, "/", "_")
					safe = strings.ReplaceAll(safe, "\\", "_")
					safe = strings.ReplaceAll(safe, ":", "_")
					idx.WriteString(fmt.Sprintf("| [%s](%s.md) | %s | %s |\n", name, safe, diff, resp))
				}
				idx.WriteString("\n")
			}
			idxPath := filepath.Join(modulesDir, "README.md")
			out := addFrontmatter("README.md", idx.String(), wiki.ProjectName, len(wiki.ModuleDocs))
			if err := os.WriteFile(idxPath, []byte(out), 0644); err != nil {
				return fmt.Errorf("write modules README: %w", err)
			}
		}

		for modName, content := range wiki.ModuleDocs {
			// Sanitize module name for filename
			fname := strings.ReplaceAll(modName, "/", "_")
			fname = strings.ReplaceAll(fname, "\\", "_")
			fname = strings.ReplaceAll(fname, ":", "_")
			modPath := filepath.Join(modulesDir, fname+".md")
			out := addFrontmatter(fname+".md", content, wiki.ProjectName, moduleCount)
			if err := os.WriteFile(modPath, []byte(out), 0644); err != nil {
				return fmt.Errorf("write module doc %s: %w", modName, err)
			}
		}
	}

	// PDF export (best-effort: don't block other exports on font missing)
	if pdfBytes, err := GeneratePDF(wiki); err == nil {
		pdfPath := filepath.Join(outputDir, "wiki.pdf")
		if err := os.WriteFile(pdfPath, pdfBytes, 0644); err != nil {
			fmt.Printf("警告：写入 PDF 失败: %v\n", err)
		}
	} else {
		fmt.Printf("警告：生成 PDF 失败: %v\n", err)
	}

	return nil
}

// GenerateStaticHTML renders the entire Wiki as a single standalone HTML file
// suitable for offline viewing. It includes sidebar navigation, all Markdown
// content converted to HTML, and embedded Mermaid diagrams.
func GenerateStaticHTML(wiki *Wiki) string {
	var body strings.Builder

	sections := []struct {
		id      string
		title   string
		content string
	}{
		{"overview", "项目概述", wiki.Overview},
		{"what-it-does", "项目能做什么", wiki.WhatItDoes},
		{"architecture", "架构说明", wiki.Architecture},
		{"project-structure", "项目结构", wiki.ProjectStructure},
	}
	if wiki.KeyConcepts != "" {
		sections = append(sections, struct {
			id      string
			title   string
			content string
		}{"key-concepts", "核心概念", "# 核心概念与设计决策\n\n" + wiki.KeyConcepts})
	}
	sections = append(sections, struct {
		id      string
			title   string
			content string
	}{"learning-path", "学习路径", wiki.LearningPath})
	sections = append(sections, struct {
		id      string
			title   string
			content string
	}{"api-reference", "API 参考", wiki.APIReference})

	for _, sec := range sections {
		body.WriteString(fmt.Sprintf("<section id=\"%s\">\n", sec.id))
		body.WriteString(RenderMarkdownBody([]byte(sec.content)))
		body.WriteString("</section>\n")
	}

	// Diagrams are now embedded inside their thematic articles via Markdown
	// mermaid code blocks, so RenderMarkdownBody already renders them inline.
	// No standalone diagram sections needed.

	// Module docs
	if len(wiki.ModuleDocs) > 0 {
		var modNames []string
		for name := range wiki.ModuleDocs {
			modNames = append(modNames, name)
		}
		sort.Strings(modNames)
		for _, name := range modNames {
			secID := "module-" + mermaidEscape(name)
			body.WriteString(fmt.Sprintf(`<section id="%s">` + "\n", secID))
			body.WriteString(RenderMarkdownBody([]byte(wiki.ModuleDocs[name])))
			body.WriteString("\n</section>\n")
		}
	}

	var nav strings.Builder
	nav.WriteString(`<nav class="sidebar">
<div class="sidebar-header">`)
	nav.WriteString(HTMLEscape(wiki.ProjectName))
	nav.WriteString(` Wiki</div>
<ul>
`)
	for _, sec := range sections {
		nav.WriteString(fmt.Sprintf(`<li><a href="#%s">%s</a></li>
`, sec.id, sec.title))
	}
	// Diagrams are now embedded inside their thematic sections, so no standalone nav links needed
	if len(wiki.ModuleDocs) > 0 {
		if len(wiki.ModuleThemes) > 0 {
			for _, theme := range sortedThemeKeys(wiki.ModuleThemes) {
				nav.WriteString(fmt.Sprintf(`<li class="sidebar-section">%s</li>`+"\n", HTMLEscape(theme)))
				for _, name := range wiki.ModuleThemes[theme] {
					if _, ok := wiki.ModuleDocs[name]; !ok {
						continue
					}
					secID := "module-" + mermaidEscape(name)
					nav.WriteString(fmt.Sprintf(`<li><a href="#%s">%s</a></li>`+"\n", secID, HTMLEscape(name)))
				}
			}
		} else {
			var modNames []string
			for name := range wiki.ModuleDocs {
				modNames = append(modNames, name)
			}
			sort.Strings(modNames)
			nav.WriteString(`<li class="sidebar-section">模块文档</li>` + "\n")
			for _, name := range modNames {
				secID := "module-" + mermaidEscape(name)
				nav.WriteString(fmt.Sprintf(`<li><a href="#%s">%s</a></li>`+"\n", secID, HTMLEscape(name)))
			}
		}
	}
	nav.WriteString(`</ul>
</nav>
`)

	var out strings.Builder
	out.WriteString(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>`)
	out.WriteString(HTMLEscape(wiki.ProjectName))
	out.WriteString(` Wiki</title>
<style>
* { box-sizing: border-box; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; margin: 0; line-height: 1.6; color: #24292f; background: #ffffff; display: flex; }
.sidebar { width: 260px; min-width: 260px; background: #f6f8fa; border-right: 1px solid #d0d7de; height: 100vh; position: fixed; overflow-y: auto; }
.sidebar-header { padding: 16px; font-weight: 600; font-size: 16px; border-bottom: 1px solid #d0d7de; background: #ffffff; }
.sidebar ul { list-style: none; padding: 0; margin: 0; }
.sidebar li a { display: block; padding: 8px 16px; color: #24292f; text-decoration: none; font-size: 14px; border-bottom: 1px solid #eaeef2; }
.sidebar li a:hover { background: #eaeef2; }
.sidebar-section { padding: 12px 16px 4px; font-size: 12px; font-weight: 600; color: #57606a; text-transform: uppercase; border-bottom: 1px solid #eaeef2; }
.content { margin-left: 260px; padding: 24px 32px; max-width: 960px; width: 100%; }
h1, h2, h3, h4, h5, h6 { margin-top: 24px; margin-bottom: 16px; font-weight: 600; line-height: 1.25; color: #1f2328; }
h1 { font-size: 2em; border-bottom: 1px solid #d0d7de; padding-bottom: .3em; }
h2 { font-size: 1.5em; border-bottom: 1px solid #d0d7de; padding-bottom: .3em; }
h3 { font-size: 1.25em; }
a { color: #0969da; text-decoration: none; }
a:hover { text-decoration: underline; }
code { font-family: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace; background: rgba(175,184,193,0.2); padding: .2em .4em; border-radius: 6px; font-size: 85%; }
pre { background: #1e1e1e; color: #d4d4d4; padding: 16px; overflow: auto; border-radius: 6px; }
pre code { background: transparent; padding: 0; color: inherit; }
blockquote { margin: 0; padding: 0 1em; color: #57606a; border-left: .25em solid #d0d7de; }
ul, ol { padding-left: 2em; }
li+li { margin-top: .25em; }
table { border-collapse: collapse; width: 100%; margin: 16px 0; }
th, td { border: 1px solid #d0d7de; padding: 6px 13px; }
tr:nth-child(even) { background: #f6f8fa; }
th { background: #f6f8fa; font-weight: 600; }
hr { height: .25em; padding: 0; margin: 24px 0; background: #d0d7de; border-0; }
.mermaid { background: #f6f8fa; padding: 16px; border-radius: 6px; overflow: hidden; min-height: 200px; }
</style>
<script src="https://cdn.jsdelivr.net/npm/svg-pan-zoom@3.6.1/dist/svg-pan-zoom.min.js"></script>
<script type="module">
  import mermaid from 'https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.esm.min.mjs';
  mermaid.initialize({ startOnLoad: true, securityLevel: 'loose' });
  window.navigateToModule = function(name) {
    var safe = name.replace(/[\/\\:]/g, '_');
    window.location.href = 'modules/' + safe + '.md';
  };
  window.addEventListener('load', function() {
    if (typeof svgPanZoom === 'undefined') return;
    document.querySelectorAll('.mermaid svg').forEach(function(svg) {
      svg.style.maxWidth = 'none';
      svgPanZoom(svg, { zoomEnabled: true, panEnabled: true, controlIconsEnabled: true, fit: true, center: true });
    });
  });
</script>
</head>
<body>
`)
	out.WriteString(nav.String())
	out.WriteString(`<div class="content">
`)
	out.WriteString(body.String())
	out.WriteString(`</div>
</body>
</html>
`)
	return out.String()
}

// buildSourcesFooter creates a Markdown footer listing the top source files.
func buildSourcesFooter(graph *grapher.Graph, maxSources int) string {
	if len(graph.Nodes) == 0 {
		return ""
	}
	pr := graph.PageRank()
	type scored struct {
		node  *grapher.Node
		score float64
	}
	scores := make([]scored, 0, len(graph.Nodes))
	for _, n := range graph.Nodes {
		scores = append(scores, scored{node: n, score: pr[n.Name]})
	}
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})
	if maxSources > 0 && len(scores) > maxSources {
		scores = scores[:maxSources]
	}
	var refs []string
	for _, s := range scores {
		ref := "`" + s.node.Filename
		// Append line number from the first function or class definition
		if len(s.node.Functions) > 0 {
			ref += fmt.Sprintf(":%d", s.node.Functions[0].StartLine)
		} else if len(s.node.Classes) > 0 {
			ref += fmt.Sprintf(":%d", s.node.Classes[0].StartLine)
		}
		ref += "`（" + s.node.Name + "）"
		refs = append(refs, ref)
	}
	return "\n\n**参考来源**：" + strings.Join(refs, "、") + "\n"
}

// buildWhereToGoNext creates a "next reading" navigation footer for narrative articles.
func buildWhereToGoNext(currentFile string, hasKeyConcepts bool) string {
	var nextFile, nextTitle, nextDesc string
	switch currentFile {
	case "00-overview.md":
		nextFile, nextTitle, nextDesc = "01-what-it-does.md", "项目能做什么", "理解核心能力、使用场景和目标用户"
	case "01-what-it-does.md":
		nextFile, nextTitle, nextDesc = "02-architecture.md", "架构说明", "掌握系统分层、设计模式和模块关系"
	case "02-architecture.md":
		nextFile, nextTitle, nextDesc = "03-project-structure.md", "项目结构", "熟悉目录组织方式和模块职责"
	case "03-project-structure.md":
		if hasKeyConcepts {
			nextFile, nextTitle, nextDesc = "04-key-concepts.md", "核心概念", "深入理解关键设计决策与架构思想"
		} else {
			nextFile, nextTitle, nextDesc = "05-learning-path.md", "学习路径", "按目标选择最适合的阅读路径"
		}
	case "04-key-concepts.md":
		nextFile, nextTitle, nextDesc = "05-learning-path.md", "学习路径", "按目标选择最适合的阅读路径"
	case "05-learning-path.md":
		nextFile, nextTitle, nextDesc = "api-reference.md", "API 参考", "查阅全部类、函数、方法的详细说明"
	case "api-reference.md":
		return "\n\n---\n\n**阅读完成** 🎉 你已浏览完全部学习指南。如需深入特定模块，请查看左侧导航中的 **模块文档** 或使用 **问答终端** 提出具体问题。\n"
	default:
		return ""
	}
	return fmt.Sprintf("\n\n---\n\n**下一步阅读** → [%s](%s) — %s\n", nextTitle, nextFile, nextDesc)
}

// GenerateMarkdownCompilation merges all Markdown documents into a single compilation
// with table of contents for easy offline reading.
func GenerateMarkdownCompilation(wiki *Wiki) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("# %s Wiki 合辑\n\n", wiki.ProjectName))
	b.WriteString("> 本文档由 CodeWiki 自动生成，整合项目概述、架构说明与 API 参考。\n\n")
	b.WriteString("---\n\n")

	// Table of contents
	b.WriteString("## 目录\n\n")
	b.WriteString("1. [项目概述](#项目概述)\n")
	b.WriteString("2. [项目能做什么](#项目能做什么)\n")
	b.WriteString("3. [架构说明](#架构说明)\n")
	b.WriteString("4. [项目结构](#项目结构)\n")
	if wiki.KeyConcepts != "" {
		b.WriteString("5. [核心概念](#核心概念)\n")
	}
	b.WriteString("6. [学习路径](#学习路径)\n")
	b.WriteString("7. [API 参考](#api-参考)\n")
	b.WriteString("\n---\n\n")

	// Section 1: Overview
	b.WriteString(adjustHeadingLevel(wiki.Overview, 1))
	b.WriteString("\n\n---\n\n")

	// Section 2: What It Does
	b.WriteString(adjustHeadingLevel(wiki.WhatItDoes, 1))
	b.WriteString("\n\n---\n\n")

	// Section 3: Architecture
	b.WriteString(adjustHeadingLevel(wiki.Architecture, 1))
	b.WriteString("\n\n---\n\n")

	// Section 4: Project Structure
	b.WriteString(adjustHeadingLevel(wiki.ProjectStructure, 1))
	b.WriteString("\n\n---\n\n")

	// Section 5: Key Concepts
	if wiki.KeyConcepts != "" {
		b.WriteString("## 核心概念与设计决策\n\n")
		b.WriteString(adjustHeadingLevel(wiki.KeyConcepts, 2))
		b.WriteString("\n\n---\n\n")
	}

	// Section 6: Learning Path
	b.WriteString(adjustHeadingLevel(wiki.LearningPath, 1))
	b.WriteString("\n\n---\n\n")

	// Section 7: API Reference
	b.WriteString(adjustHeadingLevel(wiki.APIReference, 1))
	b.WriteString("\n\n---\n\n")

	// Diagrams are now embedded inside their thematic articles via Markdown
	// mermaid code blocks, so they automatically appear in the compilation
	// under Architecture / Key Concepts / Learning Path sections.

	// Module theme index
	if len(wiki.ModuleDocs) > 0 && len(wiki.ModuleThemes) > 0 {
		b.WriteString("## 模块主题索引\n\n")
		for _, theme := range sortedThemeKeys(wiki.ModuleThemes) {
			b.WriteString(fmt.Sprintf("### %s\n\n", theme))
			for _, name := range wiki.ModuleThemes[theme] {
				if _, ok := wiki.ModuleDocs[name]; !ok {
					continue
				}
				b.WriteString(fmt.Sprintf("- `%s`\n", name))
			}
			b.WriteString("\n")
		}
		b.WriteString("---\n\n")
	}

	// Module docs
	if len(wiki.ModuleDocs) > 0 {
		var modNames []string
		for name := range wiki.ModuleDocs {
			modNames = append(modNames, name)
		}
		sort.Strings(modNames)
		b.WriteString("## 模块文档\n\n")
		for _, name := range modNames {
			b.WriteString(adjustHeadingLevel(wiki.ModuleDocs[name], 2))
			b.WriteString("\n\n---\n\n")
		}
	}

	return b.String()
}

// adjustHeadingLevel shifts all Markdown headings down by shift levels
// so they fit under the compilation title.
func adjustHeadingLevel(content string, shift int) string {
	if shift <= 0 {
		return content
	}
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "#") {
			// Count heading level
			level := 0
			for i := 0; i < len(trimmed) && trimmed[i] == '#'; i++ {
				level++
			}
			if level+shift <= 6 {
				// Replace the leading #s with deeper ones
				line = strings.Repeat("#", level+shift) + line[level:]
			}
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// formatSignature builds a human-readable function signature.
// describeFunction generates a static semantic description for a function
// based on its name, parameters, and return type.
func describeFunction(name string, params []string, returnType string) string {
	// Extract noun from name (after verb prefix)
	noun := name
	verb := ""

	switch {
	case name == "__init__":
		return "构造函数，初始化对象属性"
	case name == "__str__":
		return "返回对象的字符串表示"
	case name == "__repr__":
		return "返回对象的正式字符串表示"
	case name == "authenticate" || name == "login":
		return "用户认证，验证身份凭据"
	case name == "register" || name == "signup":
		return "用户注册，创建新账户"
	case name == "logout":
		return "用户登出，终止当前会话"
	case name == "run" || name == "main" || name == "start" || name == "execute":
		return "程序入口，执行主逻辑"
	case name == "init" || name == "setup" || name == "configure":
		return "初始化系统或配置环境"
	case strings.HasPrefix(name, "get_"):
		verb = "获取"
		noun = name[4:]
	case strings.HasPrefix(name, "set_"):
		verb = "设置"
		noun = name[4:]
	case strings.HasPrefix(name, "create_") || strings.HasPrefix(name, "add_") || strings.HasPrefix(name, "new_"):
		verb = "创建"
		if strings.HasPrefix(name, "create_") {
			noun = name[7:]
		} else if strings.HasPrefix(name, "add_") {
			noun = name[4:]
		} else {
			noun = name[4:]
		}
	case strings.HasPrefix(name, "update_") || strings.HasPrefix(name, "modify_"):
		verb = "更新"
		if strings.HasPrefix(name, "update_") {
			noun = name[7:]
		} else {
			noun = name[7:]
		}
	case strings.HasPrefix(name, "delete_") || strings.HasPrefix(name, "remove_") || strings.HasPrefix(name, "drop_"):
		verb = "删除"
		if strings.HasPrefix(name, "delete_") {
			noun = name[7:]
		} else if strings.HasPrefix(name, "remove_") {
			noun = name[7:]
		} else {
			noun = name[5:]
		}
	case strings.HasPrefix(name, "validate_") || strings.HasPrefix(name, "check_") || strings.HasPrefix(name, "verify_"):
		verb = "验证"
		if strings.HasPrefix(name, "validate_") {
			noun = name[9:]
		} else if strings.HasPrefix(name, "check_") {
			noun = name[6:]
		} else {
			noun = name[7:]
		}
	case strings.HasPrefix(name, "parse_") || strings.HasPrefix(name, "extract_"):
		verb = "解析"
		if strings.HasPrefix(name, "parse_") {
			noun = name[6:]
		} else {
			noun = name[8:]
		}
	case strings.HasPrefix(name, "format_"):
		verb = "格式化"
		noun = name[7:]
	case strings.HasPrefix(name, "send_"):
		verb = "发送"
		noun = name[5:]
	case strings.HasPrefix(name, "receive_") || strings.HasPrefix(name, "recv_"):
		verb = "接收"
		if strings.HasPrefix(name, "receive_") {
			noun = name[8:]
		} else {
			noun = name[5:]
		}
	case strings.HasPrefix(name, "process_") || strings.HasPrefix(name, "handle_"):
		verb = "处理"
		if strings.HasPrefix(name, "process_") {
			noun = name[8:]
		} else {
			noun = name[7:]
		}
	case strings.HasPrefix(name, "load_") || strings.HasPrefix(name, "read_"):
		verb = "读取"
		if strings.HasPrefix(name, "load_") {
			noun = name[5:]
		} else {
			noun = name[5:]
		}
	case strings.HasPrefix(name, "save_") || strings.HasPrefix(name, "write_") || strings.HasPrefix(name, "store_"):
		verb = "保存"
		if strings.HasPrefix(name, "save_") {
			noun = name[5:]
		} else if strings.HasPrefix(name, "write_") {
			noun = name[6:]
		} else {
			noun = name[6:]
		}
	case strings.HasPrefix(name, "find_") || strings.HasPrefix(name, "search_") || strings.HasPrefix(name, "query_") || strings.HasPrefix(name, "lookup_"):
		verb = "查找"
		if strings.HasPrefix(name, "find_") {
			noun = name[5:]
		} else if strings.HasPrefix(name, "search_") {
			noun = name[7:]
		} else if strings.HasPrefix(name, "query_") {
			noun = name[6:]
		} else {
			noun = name[7:]
		}
	case strings.HasPrefix(name, "build_") || strings.HasPrefix(name, "make_") || strings.HasPrefix(name, "generate_"):
		verb = "生成"
		if strings.HasPrefix(name, "build_") {
			noun = name[6:]
		} else if strings.HasPrefix(name, "make_") {
			noun = name[5:]
		} else {
			noun = name[9:]
		}
	case strings.HasPrefix(name, "convert_") || strings.HasPrefix(name, "transform_"):
		verb = "转换"
		if strings.HasPrefix(name, "convert_") {
			noun = name[8:]
		} else {
			noun = name[10:]
		}
	case strings.HasPrefix(name, "serialize") || strings.HasPrefix(name, "to_dict") || strings.HasPrefix(name, "to_json"):
		return "序列化对象为结构化数据"
	case strings.HasPrefix(name, "deserialize") || strings.HasPrefix(name, "from_dict") || strings.HasPrefix(name, "from_json"):
		return "从结构化数据反序列化对象"
	case strings.HasPrefix(name, "to_"):
		verb = "转换为"
		noun = name[3:]
	case strings.HasPrefix(name, "from_"):
		verb = "从"
		noun = name[5:] + " 解析/构建"
	case strings.HasPrefix(name, "is_") || strings.HasPrefix(name, "has_") || strings.HasPrefix(name, "can_"):
		verb = "判断"
		if strings.HasPrefix(name, "is_") {
			noun = name[3:] + " 状态"
		} else if strings.HasPrefix(name, "has_") {
			noun = "是否具备 " + name[4:]
		} else {
			noun = "是否可 " + name[4:]
		}
	case strings.HasPrefix(name, "render_") || strings.HasPrefix(name, "draw_") || strings.HasPrefix(name, "display_"):
		verb = "渲染"
		if strings.HasPrefix(name, "render_") {
			noun = name[7:]
		} else if strings.HasPrefix(name, "draw_") {
			noun = name[5:]
		} else {
			noun = name[8:]
		}
	case strings.HasPrefix(name, "encode_") || strings.HasPrefix(name, "decode_"):
		verb = "编解码"
		if strings.HasPrefix(name, "encode_") {
			noun = name[7:]
		} else {
			noun = name[7:]
		}
	default:
		// Empty / abstract function fallback
		if len(params) == 0 && (returnType == "" || returnType == "None" || returnType == "void") {
			if strings.HasPrefix(name, "abstract") || strings.HasPrefix(name, "Abstract") {
				return "抽象方法，需子类实现"
			}
			return "占位函数，待实现具体逻辑"
		}
		// Try to infer from suffix patterns
		if strings.HasSuffix(name, "_service") || strings.HasSuffix(name, "_handler") {
			return "处理 " + noun + " 相关逻辑"
		}
		return "执行 " + name + " 操作"
	}

	// Build description with noun
	desc := verb
	if noun != "" {
		// Convert snake_case to spaces for readability
		noun = strings.ReplaceAll(noun, "_", " ")
		desc += noun
	}

	// Add parameter context
	if len(params) > 0 {
		var paramNames []string
		for _, p := range params {
			if p != "self" && p != "cls" {
				paramNames = append(paramNames, p)
			}
		}
		if len(paramNames) > 0 {
			desc += fmt.Sprintf("，参数：%s", strings.Join(paramNames, ", "))
		}
	}

	// Add return type hint
	if returnType != "" && returnType != "None" && returnType != "void" {
		desc += fmt.Sprintf("，返回 %s", returnType)
	}

	return desc
}

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

// ---------- LLM Function Description Helpers ----------

type funcRef struct {
	Module     string
	Name       string
	Params     []string
	ReturnType string
	IsMethod   bool
	Callers    []string // functions that call this one
	Callees    []string // functions this one calls
}

// nodesToFileResults converts grapher nodes to analyzer file results for call graph extraction.
func nodesToFileResults(nodes []*grapher.Node) []*analyzer.FileResult {
	files := make([]*analyzer.FileResult, 0, len(nodes))
	for _, n := range nodes {
		files = append(files, &analyzer.FileResult{
			Filename:  n.Filename,
			Classes:   n.Classes,
			Functions: n.Functions,
		})
	}
	return files
}

func selectTopFunctions(graph *grapher.Graph, callEdges []sequencer.CallEdge, maxN int) []funcRef {
	// 1. Collect all functions/methods and group by module
	var all []funcRef
	moduleFuncs := make(map[string][]funcRef)
	for _, n := range graph.Nodes {
		for _, f := range n.Functions {
			ref := funcRef{Module: n.Name, Name: f.Name, Params: f.Params, ReturnType: f.ReturnType, IsMethod: false}
			all = append(all, ref)
			moduleFuncs[n.Name] = append(moduleFuncs[n.Name], ref)
		}
		for _, c := range n.Classes {
			for _, m := range c.Methods {
				fullName := c.Name + "." + m.Name
				ref := funcRef{Module: n.Name, Name: fullName, Params: m.Params, ReturnType: m.ReturnType, IsMethod: true}
				all = append(all, ref)
				moduleFuncs[n.Name] = append(moduleFuncs[n.Name], ref)
			}
		}
	}

	total := len(all)
	if total == 0 || maxN == 0 {
		return nil
	}

	// Auto-compute target: at least 30% of total functions, with a floor of 10
	target := int(float64(total) * 0.3)
	if target < 10 {
		target = min(10, total)
	}
	if maxN > 0 && target > maxN {
		target = maxN
	}
	// maxN < 0 means auto-compute without cap

	// 2. Compute in-degree (called by) and out-degree (calls to) from callEdges
	inDegree := make(map[string]int)
	outDegree := make(map[string]int)
	callersMap := make(map[string][]string)
	calleesMap := make(map[string][]string)
	for _, e := range callEdges {
		fromKey := e.From.Module + "#" + e.From.Name
		toKey := e.To.Module + "#" + e.To.Name
		outDegree[fromKey]++
		inDegree[toKey]++
		callersMap[toKey] = append(callersMap[toKey], e.From.Name)
		calleesMap[fromKey] = append(calleesMap[fromKey], e.To.Name)
	}

	// 3. Compute module importance scores
	roles := graph.InferModuleRoles()
	roleScore := make(map[string]int)
	for _, r := range roles {
		switch r.Role {
		case "核心领域", "入口层":
			roleScore[r.Name] = 100
		case "业务模块":
			roleScore[r.Name] = 50
		default:
			roleScore[r.Name] = 10
		}
	}

	entries := graph.EntryPoints()
	entrySet := make(map[string]bool)
	for _, e := range entries {
		entrySet[e.Name] = true
	}

	// 4. Score each function
	type scoredFunc struct {
		ref   funcRef
		score int
	}
	scored := make([]scoredFunc, len(all))
	for i, ref := range all {
		key := ref.Module + "#" + ref.Name
		score := inDegree[key]*5 + outDegree[key]*3
		score += roleScore[ref.Module]
		score += len(graph.DependentsOf(ref.Module)) * 2
		if entrySet[ref.Module] {
			score += 15
		}
		scored[i] = scoredFunc{ref: ref, score: score}
	}

	// Sort by score descending for tiered selection
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].ref.Name < scored[j].ref.Name
	})

	selected := make(map[string]bool)
	var result []funcRef

	addIfNotSelected := func(ref funcRef) {
		key := ref.Module + "#" + ref.Name
		if !selected[key] {
			selected[key] = true
			ref.Callers = callersMap[key]
			ref.Callees = calleesMap[key]
			result = append(result, ref)
		}
	}

	// Layer 1: Hub functions (top in-degree) — ~35% of target
	layer1Count := target * 35 / 100
	if layer1Count < 3 {
		layer1Count = min(3, target)
	}
	for i := 0; i < len(scored) && i < layer1Count; i++ {
		addIfNotSelected(scored[i].ref)
	}

	// Layer 2: Module representatives — every module with functions gets at least 1-2
	// Sort modules by importance
	var moduleNames []string
	for name := range moduleFuncs {
		moduleNames = append(moduleNames, name)
	}
	sort.Slice(moduleNames, func(i, j int) bool {
		if roleScore[moduleNames[i]] != roleScore[moduleNames[j]] {
			return roleScore[moduleNames[i]] > roleScore[moduleNames[j]]
		}
		return moduleNames[i] < moduleNames[j]
	})

	for _, mod := range moduleNames {
		if len(result) >= target {
			break
		}
		added := 0
		maxPerModule := 2
		if roleScore[mod] >= 100 {
			maxPerModule = 4 // core modules get more coverage
		} else if roleScore[mod] >= 50 {
			maxPerModule = 3 // business modules get moderate coverage
		}
		for _, s := range scored {
			if s.ref.Module != mod {
				continue
			}
			key := s.ref.Module + "#" + s.ref.Name
			if !selected[key] {
				addIfNotSelected(s.ref)
				added++
				if added >= maxPerModule {
					break
				}
			}
		}
	}

	// Layer 3: Fill remaining slots with highest-scored uncovered functions
	for _, s := range scored {
		if len(result) >= target {
			break
		}
		addIfNotSelected(s.ref)
	}

	// Sort result by module importance then by name for deterministic output
	sort.Slice(result, func(i, j int) bool {
		si := roleScore[result[i].Module]
		sj := roleScore[result[j].Module]
		if si != sj {
			return si > sj
		}
		return result[i].Name < result[j].Name
	})

	return result
}

func buildFunctionDescriptionPrompt(funcs []funcRef) string {
	var b strings.Builder
	fmt.Fprintf(&b, "你是一个资深软件架构师。请为以下 %d 个关键函数/方法撰写深度语义分析。\n\n", len(funcs))
	fmt.Fprintf(&b, "对每个函数，用一段话（50-120 字）完成以下三个层面的分析，用分号隔开：\n")
	fmt.Fprintf(&b, "1. 职责：该函数的核心业务/技术意图（不要只复述函数名）\n")
	fmt.Fprintf(&b, "2. 执行逻辑：关键执行步骤、条件分支、数据流转或核心算法思路\n")
	fmt.Fprintf(&b, "3. 调用关联：在调用链中的位置（被谁调用、调用了谁、扮演什么角色）\n\n")
	fmt.Fprintf(&b, "要求：\n")
	fmt.Fprintf(&b, "- 使用简体中文\n")
	fmt.Fprintf(&b, "- 按以下格式输出（每行一个）：函数完整名: 职责是...；执行逻辑为...；调用关联是...\n")
	fmt.Fprintf(&b, "- 函数完整名不要带括号，如 HookRegistry.clear、Api.get，不要省略类名前缀\n\n")
	for _, f := range funcs {
		sig := formatSignature(f.Name, f.Params, f.ReturnType)
		fmt.Fprintf(&b, "- %s（来自模块 %s）\n", sig, f.Module)
		fmt.Fprintf(&b, "  输出名称请严格使用: %s\n", f.Name)
		if len(f.Callers) > 0 {
			fmt.Fprintf(&b, "  被以下函数调用: %s\n", strings.Join(f.Callers, ", "))
		}
		if len(f.Callees) > 0 {
			fmt.Fprintf(&b, "  调用以下函数: %s\n", strings.Join(f.Callees, ", "))
		}
	}
	return b.String()
}

func parseFunctionDescriptions(response string, funcs []funcRef) map[string]string {
	result := make(map[string]string)
	// Build lookup from short name to module#name key
	lookup := make(map[string]string)
	for _, f := range funcs {
		lookup[f.Name] = f.Module + "#" + f.Name
	}

	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Parse "Name: description" or "- Name: description"
		line = strings.TrimPrefix(line, "-")
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSpace(line)
		// Remove leading numbering like "1. "
		for len(line) > 0 && line[0] >= '0' && line[0] <= '9' {
			if len(line) > 1 && line[1] == '.' {
				line = strings.TrimSpace(line[2:])
				break
			}
			break
		}
		// Support both English colon and Chinese colon
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			parts = strings.SplitN(line, "：", 2)
		}
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		// Strip common markdown decorations (bold, code backticks)
		name = strings.TrimPrefix(name, "**")
		name = strings.TrimSuffix(name, "**")
		name = strings.TrimPrefix(name, "`")
		name = strings.TrimSuffix(name, "`")
		// Strip trailing parentheses that LLM often adds, e.g. "HookRegistry.clear()"
		if idx := strings.Index(name, "("); idx > 0 {
			name = name[:idx]
		}
		desc := strings.TrimSpace(parts[1])
		if key, ok := lookup[name]; ok {
			result[key] = desc
			continue
		}
		matched := false
		// Fallback 1: if name is a short method name like "clear", try to match
		// any full name that ends with ".clear".
		for fullName, key := range lookup {
			if strings.HasSuffix(fullName, "."+name) || fullName == name {
				result[key] = desc
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		// Fallback 2: LLM may prefix the name with module path (e.g. "src/tools/types.call").
		// Extract candidate short names by splitting on "/" and ".".
		var candidates []string
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			candidates = append(candidates, name[idx+1:])
		}
		if idx := strings.LastIndex(name, "."); idx >= 0 {
			candidates = append(candidates, name[idx+1:])
		}
		for _, cand := range candidates {
			if key, ok := lookup[cand]; ok {
				result[key] = desc
				matched = true
				break
			}
			for fullName, key := range lookup {
				if strings.HasSuffix(fullName, "."+cand) || fullName == cand {
					result[key] = desc
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
	}
	if len(result) == 0 && strings.TrimSpace(response) != "" {
		fmt.Printf("警告：LLM 函数描述返回格式无法解析，原始内容前 200 字：%q\n", response[:min(len(response), 200)])
	}
	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// isTimeoutErr checks whether an error is caused by a timeout or deadline exceeded.
func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "context canceled")
}

// markdownEscape escapes Markdown special characters in text.
func markdownEscape(s string) string {
	s = strings.ReplaceAll(s, "*", "\\*")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}

func sortedThemeKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
