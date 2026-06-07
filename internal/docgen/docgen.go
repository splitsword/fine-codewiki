package docgen

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/splitsword/fine-codewiki/internal/analyzer"
	"github.com/splitsword/fine-codewiki/internal/diagram"
	"github.com/splitsword/fine-codewiki/internal/grapher"
	"github.com/splitsword/fine-codewiki/internal/llm"
	"github.com/splitsword/fine-codewiki/internal/sequencer"
)

// Wiki holds all generated documentation artifacts.
// ChapterTitle holds the LLM-generated book-like title for a theme chapter.
type ChapterTitle struct {
	Title         string   // 4-8 char Chinese, e.g. "用户认证"
	Subtitle      string   // ~15 char, e.g. "身份验证与会话管理"
	Difficulty    string   // "⭐", "⭐⭐", "⭐⭐⭐"
	LearningGoals []string // 2-3 learning objectives for this chapter
	Prerequisites []string // 0-2 prerequisite topics
}

type Wiki struct {
	ProjectName         string
	Language            string // 目标项目的编程语言，用于来源文件名扩展名显示
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
	ModuleChineseNames  map[string]string          // module name → LLM-generated Chinese name
	ModuleThemes        map[string][]string        // theme -> sorted module names
	ChapterTitles       map[string]ChapterTitle    // theme name → LLM-generated title
	ThemeIntros         map[string]string          // theme name → LLM-generated 2-3 sentence intro
	ChapterNarratives   map[string]string          // theme name → LLM-generated narrative article
	ChapterPages              map[string]string          // theme name → standalone chapter HTML
	Sequences           []sequencer.Sequence       // call sequences for per-chapter sequence diagrams
	CallEdges           []sequencer.CallEdge       // raw call edges for per-module sequence diagrams
}


// wikiCheckpoint persists partial LLM-enhanced results for resume support.
type wikiCheckpoint struct {
	Overview      string            `json:"overview,omitempty"`
	WhatItDoes    string            `json:"what_it_does,omitempty"`
	KeyConcepts   string            `json:"key_concepts,omitempty"`
	LearningPath  string            `json:"learning_path,omitempty"`
	ArchNarrative string            `json:"arch_narrative,omitempty"`
	FuncDescMap   map[string]string         `json:"func_desc_map,omitempty"`
	Timestamp     time.Time                 `json:"timestamp"`
	ChapterTitles        map[string]ChapterTitle   `json:"chapter_titles,omitempty"`
	ModuleChineseNames   map[string]string         `json:"module_chinese_names,omitempty"`
	ModuleThemes         map[string][]string       `json:"module_themes,omitempty"`
	ThemeIntros          map[string]string         `json:"theme_intros,omitempty"`
	ChapterNarratives  map[string]string         `json:"chapter_narratives,omitempty"`
	ProjectStructure   string                   `json:"project_structure,omitempty"`
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

// stripFrontmatter removes YAML frontmatter from markdown content.
func stripFrontmatter(data []byte) string {
	s := string(data)
	if !strings.HasPrefix(s, "---\n") {
		return s
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return s
	}
	body := s[4+end+5:]
	body = strings.TrimLeft(body, "\n")
	return body
}

// LoadWikiFromDir rebuilds a Wiki struct from previously-generated markdown files.
// It reads 00-*.md, api-reference.md, modules/*.md and modules/README.md to
// reconstruct enough state for PDF re-export without re-running LLM.
func LoadWikiFromDir(dir string) (*Wiki, error) {
	wiki := &Wiki{
		ModuleDocs:        make(map[string]string),
		ModuleThemes:      make(map[string][]string),
		ChapterTitles:     make(map[string]ChapterTitle),
		ThemeIntros:       make(map[string]string),
		ChapterNarratives: make(map[string]string),
	}

	// Try to load project name from compilation.md frontmatter
	if data, err := os.ReadFile(filepath.Join(dir, "compilation.md")); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "project: ") {
				wiki.ProjectName = strings.Trim(strings.TrimPrefix(line, "project: "), `"`)
				break
			}
		}
	}

	fileMap := map[string]*string{
		"00-overview.md":          &wiki.Overview,
		"01-what-it-does.md":      &wiki.WhatItDoes,
		"02-architecture.md":      &wiki.Architecture,
		"03-project-structure.md": &wiki.ProjectStructure,
		"05-learning-path.md":     &wiki.LearningPath,
		"api-reference.md":        &wiki.APIReference,
	}
	for fname, ptr := range fileMap {
		data, err := os.ReadFile(filepath.Join(dir, fname))
		if err != nil {
			continue
		}
		*ptr = stripFrontmatter(data)
	}

	// Key concepts has a prefixed heading when written
	if data, err := os.ReadFile(filepath.Join(dir, "04-key-concepts.md")); err == nil {
		content := stripFrontmatter(data)
		content = strings.TrimPrefix(content, "# 核心概念与设计决策\n\n")
		wiki.KeyConcepts = content
	}

	// Module docs
	modulesDir := filepath.Join(dir, "modules")
	entries, err := os.ReadDir(modulesDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || e.Name() == "README.md" || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(modulesDir, e.Name()))
			if err != nil {
				continue
			}
			content := stripFrontmatter(data)
			// Try to extract module name from frontmatter title
			modName := ""
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "title: ") {
					modName = strings.Trim(strings.TrimPrefix(line, "title: "), `"`)
					break
				}
			}
			if modName == "" {
				// Fallback: first heading
				for _, line := range strings.Split(content, "\n") {
					if strings.HasPrefix(line, "# ") {
						modName = strings.TrimPrefix(line, "# ")
						break
					}
				}
			}
			if modName == "" {
				modName = strings.TrimSuffix(e.Name(), ".md")
			}
			wiki.ModuleDocs[modName] = content
		}
	}

	// Module themes & chapter titles from modules/README.md
	readmePath := filepath.Join(modulesDir, "README.md")
	if data, err := os.ReadFile(readmePath); err == nil {
		content := stripFrontmatter(data)
		var currentTheme string
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "## ") {
				currentTheme = strings.TrimPrefix(line, "## ")
				wiki.ModuleThemes[currentTheme] = nil
				wiki.ChapterTitles[currentTheme] = ChapterTitle{Title: currentTheme}
				continue
			}
			if currentTheme == "" || !strings.HasPrefix(line, "|") {
				continue
			}
			// Parse table row like: | [module_name](file.md) | ⭐⭐⭐ 深入 | ... |
			parts := strings.Split(line, "|")
			if len(parts) < 3 {
				continue
			}
			cell := strings.TrimSpace(parts[1])
			// Extract module name from [name](file.md)
			start := strings.Index(cell, "[")
			end := strings.Index(cell, "](")
			if start >= 0 && end > start {
				modName := cell[start+1 : end]
				if modName != "" && modName != "模块" {
					wiki.ModuleThemes[currentTheme] = append(wiki.ModuleThemes[currentTheme], modName)
				}
			}
		}
	}

	// If no themes were found but module docs exist, group all into a single default theme
	if len(wiki.ModuleThemes) == 0 && len(wiki.ModuleDocs) > 0 {
		var mods []string
		for m := range wiki.ModuleDocs {
			mods = append(mods, m)
		}
		sort.Strings(mods)
		wiki.ModuleThemes["模块详情"] = mods
		wiki.ChapterTitles["模块详情"] = ChapterTitle{Title: "模块详情"}
	}

	return wiki, nil
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

	readme := loadProjectDocs(sourceDir)

	// 进度渲染器：TTY 下动画进度条 + 留痕，非 TTY 逐行打印。
	pr := newProgressRenderer()
	pr.start()
	defer pr.stop()

	activeRenderer = pr
	// 一次性设定全部任务总数（Phase1 7 + 设计决策 1 + Phase2 2 + Phase3 2），
	// 函数描述批次在运行时额外增量。避免分阶段 bump 导致百分比倒退。
	pr.bump(12)

	// Static generation — always runs first, instant
	staticOverview, err := GenerateOverviewMarkdown(graph, projectName)
	if err != nil {
		return nil, fmt.Errorf("generate overview: %w", err)
	}
	overview := staticOverview
	staticWhatItDoes := GenerateWhatItDoesMarkdown(graph, projectName)
	whatItDoes := staticWhatItDoes
	projectStructure := GenerateProjectStructureMarkdown(graph, projectName)
	moduleDocs := GenerateModuleDocs(graph, sourceDir)
	keyConcepts := ""
	learningPath := ""
	archNarrative := ""
	var funcDescMap map[string]string
	var arch string
	var apiRef string
	moduleThemes := make(map[string][]string)
	var themeIntros map[string]string
	var chapterTitles map[string]ChapterTitle
	var chapterNarratives map[string]string
	var moduleChineseNames map[string]string

	if provider != nil {
	var projectStructureNarrative string

		// ─── Phase 1: All independent LLM tasks run concurrently ───
		pr.log(fmt.Sprintf("[Phase 1] 启动 %d 个并行 LLM 任务", 7))
		phase1Start := time.Now()

		var wg sync.WaitGroup
		fatalCh := make(chan error, 6)
		var themeGroupsResult map[string][]*grapher.Node

		// Task 1: Overview
		if cp.Overview != "" {
			overview = cp.Overview
			pr.done("项目概述", "从 checkpoint 恢复")
		} else {
			wg.Add(1)
			go func() {
				defer wg.Done()
				pr.update("项目概述", "开始生成...")
				prompt := buildOverviewPrompt(graph, projectName, readme, language)
				enhanced, err := streamComplete(ctx, provider, prompt)
				if err != nil {
					pr.done("项目概述", fmt.Sprintf("LLM 失败 (%v)，回退静态", err))
				} else if enhanced == "" {
					pr.done("项目概述", "LLM 返回空，回退静态")
				} else if isChecklistLike(enhanced, graph) {
					pr.done("项目概述", "内容像模块清单，回退静态")
				} else {
					overview = fmt.Sprintf("# %s\n\n%s", projectName, enhanced)
					pr.done("项目概述", "生成完成")
				}
			}()
		}

		// Task 2: What It Does
		if cp.WhatItDoes != "" {
			whatItDoes = cp.WhatItDoes
			pr.done("核心能力", "从 checkpoint 恢复")
		} else {
			wg.Add(1)
			go func() {
				defer wg.Done()
				pr.update("核心能力", "开始生成...")
				prompt := buildWhatItDoesPrompt(graph, projectName, readme, language)
				enhanced, err := streamComplete(ctx, provider, prompt)
				if err != nil {
					pr.done("核心能力", fmt.Sprintf("LLM 失败 (%v)，回退静态", err))
				} else if enhanced != "" && !isChecklistLike(enhanced, graph) {
					whatItDoes = enhanced
					pr.done("核心能力", "生成完成")
				} else {
					pr.done("核心能力", "内容无效，回退静态")
				}
			}()
		}

		// Task 3: Architecture Narrative
		if cp.ArchNarrative != "" {
			archNarrative = cp.ArchNarrative
			pr.done("架构描述", "从 checkpoint 恢复")
		} else {
			wg.Add(1)
			go func() {
				defer wg.Done()
				pr.update("架构描述", "开始生成...")
				prompt := buildArchitecturePrompt(graph, projectName, readme, language)
				enhanced, err := streamComplete(ctx, provider, prompt)
				if err != nil {
					pr.done("架构描述", fmt.Sprintf("LLM 失败 (%v)，回退静态", err))
				} else if enhanced == "" {
					pr.done("架构描述", "LLM 返回空，回退静态")
				} else if isChecklistLike(enhanced, graph) {
					pr.done("架构描述", "内容像模块清单，回退静态")
				} else {
					archNarrative = enhanced
					pr.done("架构描述", "生成完成")
				}
			}()
		}

		// Task 4: Module Themes
		if cp.ModuleThemes != nil && len(cp.ModuleThemes) > 0 {
			pr.done("模块主题", "从 checkpoint 恢复")
		} else if graph != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				pr.update("模块主题", "开始语义分组...")
				themeGroupsResult = generateModuleThemes(ctx, provider, projectName, graph)
				pr.done("模块主题", fmt.Sprintf("分组完成 → %d 个主题", len(themeGroupsResult)))
			}()
		}


		// Task 6: Project Structure Narrative (LLM-based, replaces static table)
		if cp.ProjectStructure != "" {
			projectStructureNarrative = cp.ProjectStructure
			pr.done("项目结构", "从 checkpoint 恢复")
		} else {
			wg.Add(1)
			go func() {
				defer wg.Done()
				pr.update("项目结构", "开始生成叙事...")
				prompt := buildProjectStructurePrompt(graph, projectName)
				enhanced, err := streamComplete(ctx, provider, prompt)
				if err != nil {
					pr.done("项目结构", fmt.Sprintf("LLM 失败 (%v)，回退静态生成", err))
				} else if enhanced != "" {
					projectStructureNarrative = enhanced
					pr.done("项目结构", "叙事生成完成")
				} else {
					pr.done("项目结构", "LLM 返回空，回退静态生成")
				}
			}()
		}

		// Task 7: Module Chinese Names
		if cp.ModuleChineseNames != nil && len(cp.ModuleChineseNames) > 0 {
			moduleChineseNames = cp.ModuleChineseNames
			pr.done("模块中文名", fmt.Sprintf("从 checkpoint 恢复 %d 个", len(moduleChineseNames)))
		} else if graph != nil && len(graph.Nodes) > 0 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				pr.update("模块中文名", "开始生成...")
				names := generateModuleChineseNames(ctx, provider, projectName, graph.Nodes)
				if len(names) > 0 {
					moduleChineseNames = names
				pr.done("模块中文名", fmt.Sprintf("生成完成 → %d 个", len(names)))
				} else {
					pr.done("模块中文名", "生成失败，将使用原始英文名")
				}
			}()
		}

		wg.Wait()
		close(fatalCh)

		// Propagate fatal errors
		for err := range fatalCh {
			if err != nil {
				return nil, err
			}
		}
			// Task 5: Function Descriptions — after Phase 1 to avoid API quota contention.
			if maxLLMFunctions != 0 && len(cp.FuncDescMap) > 0 {
				funcDescMap = cp.FuncDescMap
				pr.done("函数描述", fmt.Sprintf("从 checkpoint 恢复 %d 个函数", len(funcDescMap)))
			} else if maxLLMFunctions != 0 {
				func() {
					var callEdges []sequencer.CallEdge
					if sourceDir != "" {
						files := nodesToFileResults(graph.Nodes)
						edges, err := sequencer.BuildCallGraph(sourceDir, files)
						if err == nil {
							callEdges = edges
						}
					}
					topFuncs := selectTopFunctions(graph, callEdges, maxLLMFunctions)
					if len(topFuncs) == 0 {
						pr.done("函数描述", "无需描述的函数")
						return
					}
					fdm := make(map[string]string)
					var fdmMu sync.Mutex
					funcsPerReq := 5
					concurrency := 10

					var reqs [][]funcRef
					for i := 0; i < len(topFuncs); i += funcsPerReq {
						end := i + funcsPerReq
						if end > len(topFuncs) {
							end = len(topFuncs)
						}
						reqs = append(reqs, topFuncs[i:end])
					}
					totalBatches := (len(reqs) + concurrency - 1) / concurrency
					pr.update("函数描述", fmt.Sprintf("开始生成 %d 个函数（每请求 %d 个，每批并发 %d，共 %d 批）...", len(topFuncs), funcsPerReq, concurrency, totalBatches))
					if totalBatches > 1 {
						pr.bump(totalBatches - 1)
					}

					for i := 0; i < len(reqs); i += concurrency {
						end := i + concurrency
						if end > len(reqs) {
							end = len(reqs)
						}
						batch := reqs[i:end]
						batchNum := i/concurrency + 1

						var batchWG sync.WaitGroup
						for _, r := range batch {
							batchWG.Add(1)
							go func(fns []funcRef) {
								defer batchWG.Done()
								prompt := buildFunctionDescriptionPrompt(fns)
								reqCtx, cancel := context.WithTimeout(ctx, 8*time.Minute)
								defer cancel()
								enhanced, err := streamComplete(reqCtx, provider, prompt)
								if err != nil {
									pr.tick("函数描述", fmt.Sprintf("批次 %d/%d 失败 (%v)", batchNum, totalBatches, err))
									return
								}
								if enhanced != "" {
									descs := parseFunctionDescriptions(enhanced, fns)
									fdmMu.Lock()
									for k, v := range descs {
										fdm[k] = v
									}
									fdmMu.Unlock()
								}
							}(r)
						}
						batchWG.Wait()
						pr.tick("函数描述", fmt.Sprintf("第 %d/%d 批完成（累计 %d/%d 个函数）", batchNum, totalBatches, len(fdm), len(topFuncs)))
					}

					funcDescMap = fdm
					totalFuncs := 0
					for _, n := range graph.Nodes {
						totalFuncs += len(n.Functions)
						for _, c := range n.Classes {
							totalFuncs += len(c.Methods)
						}
					}
					pr.log(fmt.Sprintf("[函数描述] 完成：%d/%d（%.0f%%）", len(fdm), len(topFuncs), float64(len(fdm))*100/float64(len(topFuncs))))
					pr.remove("函数描述")
				}()
			}


		pr.log(fmt.Sprintf("[Phase 1] 全部完成（耗时 %v）", time.Since(phase1Start).Round(time.Second)))

		// Key Concepts: recover from checkpoint or extract from architecture narrative
		if cp.KeyConcepts != "" {
			keyConcepts = cp.KeyConcepts
			pr.done("设计决策", "从 checkpoint 恢复")
		} else if archNarrative != "" {
			keyConcepts = extractKeyDesignDecisions(archNarrative)
			if keyConcepts != "" {
				pr.done("设计决策", "从架构叙事提取完成")
			}
		}
		if keyConcepts == "" {
			keyConcepts = GenerateKeyConceptsFallback(graph, projectName)
			pr.done("设计决策", "静态回退")
		}

		// Replace static project structure with LLM-generated narrative
		if projectStructureNarrative != "" {
			projectStructure = fmt.Sprintf("# %s 项目结构\n\n%s", projectName, projectStructureNarrative)
		}

		// Populate moduleThemes from LLM result or checkpoint
		if cp.ModuleThemes != nil && len(cp.ModuleThemes) > 0 {
			for theme, modNames := range cp.ModuleThemes {
				for _, mn := range modNames {
					moduleThemes[theme] = append(moduleThemes[theme], mn)
				}
			}
		} else if themeGroupsResult != nil && len(themeGroupsResult) > 0 {
			cp.ModuleThemes = make(map[string][]string)
			for theme, nodes := range themeGroupsResult {
				for _, n := range nodes {
					cp.ModuleThemes[theme] = append(cp.ModuleThemes[theme], n.Name)
					moduleThemes[theme] = append(moduleThemes[theme], n.Name)
				}
			}
		}

		// Save Phase 1 checkpoint
		if cpPath != "" {
			cp.Overview = overview
			cp.WhatItDoes = whatItDoes
			cp.KeyConcepts = keyConcepts
			cp.ArchNarrative = archNarrative
			cp.FuncDescMap = funcDescMap
			cp.ProjectStructure = projectStructureNarrative
			cp.ModuleChineseNames = moduleChineseNames
			_ = saveWikiCheckpoint(cpPath, cp)
		}
	} else {
		// No provider: all static
		keyConcepts = GenerateKeyConceptsFallback(graph, projectName)
	}

	// ─── Phase 2: Tasks depending on Phase 1 outputs ───
	pr.log("[Phase 2] 启动依赖任务...")
	phase2Start := time.Now()


	hasConcepts := keyConcepts != ""

	if provider != nil {
		var wg2 sync.WaitGroup

		// Learning Path
		if cp.LearningPath != "" {
			learningPath = cp.LearningPath
			pr.done("学习路径", "从 checkpoint 恢复")
		} else {
			wg2.Add(1)
			go func() {
				defer wg2.Done()
				staticLP := GenerateLearningPathMarkdown(graph, projectName, hasConcepts)
				pr.update("学习路径", "开始生成...")
				prompt := buildLearningPathPrompt(graph, projectName, language)
				batchCtx, cancel := context.WithTimeout(ctx, 8*time.Minute)
				defer cancel()
				enhanced, err := streamComplete(batchCtx, provider, prompt)
				if err != nil {
				pr.done("学习路径", fmt.Sprintf("LLM 失败 (%v)，静态回退", err))
					learningPath = staticLP
				} else if enhanced == "" {
					pr.done("学习路径", "LLM 返回空，静态回退")
					learningPath = staticLP
				} else {
					learningPath = fmt.Sprintf("# %s 学习路径\n\n%s", projectName, enhanced)
					pr.done("学习路径", "生成完成")
				}
			}()
		}

		// Chapter Titles (single LLM call for all themes)
		if len(moduleThemes) > 0 {
			if cp.ChapterTitles != nil && len(cp.ChapterTitles) > 0 {
				chapterTitles = cp.ChapterTitles
				pr.done("章节标题", "从 checkpoint 恢复")
			} else {
				wg2.Add(1)
				go func() {
					defer wg2.Done()
					pr.update("章节标题", "开始生成...")
					chapterTitles = generateChapterTitles(ctx, provider, projectName, moduleThemes, graph)
					pr.done("章节标题", "生成完成")
				}()
			}
		} else {
			chapterTitles = generateChapterTitlesFallback(moduleThemes, graph)
		}

		wg2.Wait()

		// Save Phase 2 checkpoint
		if cpPath != "" {
			cp.LearningPath = learningPath
			cp.ChapterTitles = chapterTitles
			_ = saveWikiCheckpoint(cpPath, cp)
		}
	} else {
		staticLearningPath := GenerateLearningPathMarkdown(graph, projectName, hasConcepts)
		learningPath = staticLearningPath
		chapterTitles = generateChapterTitlesFallback(moduleThemes, graph)
	}

	// Architecture doc (LLM-enhanced text + static diagram/table append)
	arch, err = GenerateArchitectureMarkdown(graph, archNarrative)
	if err != nil {
		return nil, fmt.Errorf("generate architecture doc: %w", err)
	}

	// API Reference
	apiRef, err = GenerateAPIReferenceMarkdown(graph, funcDescMap)
	if err != nil {
		return nil, fmt.Errorf("generate api reference: %w", err)
	}

	pr.log(fmt.Sprintf("[Phase 2] 全部完成（耗时 %v）", time.Since(phase2Start).Round(time.Second)))

	// ─── Phase 3: Theme-dependent tasks (chapter intros + narratives) ───
	if provider != nil && len(moduleThemes) > 0 && len(chapterTitles) > 0 {
		pr.log("[Phase 3] 启动主题相关任务...")
		phase3Start := time.Now()
		var wg3 sync.WaitGroup

		// Theme Intros
		if cp.ThemeIntros != nil && len(cp.ThemeIntros) > 0 {
			themeIntros = cp.ThemeIntros
			pr.done("主题简介", "从 checkpoint 恢复")
		} else {
			wg3.Add(1)
			go func() {
				defer wg3.Done()
				pr.update("主题简介", "开始生成...")
				themeIntros = generateThemeIntros(ctx, provider, projectName, moduleThemes, chapterTitles, graph)
				pr.done("主题简介", "生成完成")
			}()
		}

		// Chapter Narratives (per-theme parallel inside)
		if cp.ChapterNarratives != nil && len(cp.ChapterNarratives) > 0 {
			chapterNarratives = cp.ChapterNarratives
			pr.done("章节叙事", fmt.Sprintf("从 checkpoint 恢复 %d 个", len(chapterNarratives)))
		} else {
			wg3.Add(1)
			go func() {
				defer wg3.Done()
				pr.update("章节叙事", "开始生成...")
				chapterNarratives = generateChapterNarrativesParallel(ctx, provider, projectName, moduleThemes, chapterTitles, graph, pr)
				if len(chapterNarratives) > 0 {
				pr.done("章节叙事", fmt.Sprintf("生成完成 → %d/%d 个主题", len(chapterNarratives), len(moduleThemes)))
				} else {
					pr.done("章节叙事", "全部失败，将使用模块拼接模式")
				}
			}()
		}

		wg3.Wait()
		pr.log(fmt.Sprintf("[Phase 3] 全部完成（耗时 %v）", time.Since(phase3Start).Round(time.Second)))

		if cpPath != "" {
			cp.ThemeIntros = themeIntros
			cp.ChapterNarratives = chapterNarratives
			_ = saveWikiCheckpoint(cpPath, cp)
		}
	}

	// ─── Phase 4: Final assembly (sequential, fast) ───
	projectStructure += buildCoreModuleSourceSection(graph, sourceDir)
	projectStructure += buildSourcesFooter(graph, 10)
	if keyConcepts != "" && classDSL != "" {
		keyConcepts += "\n## 类型关系图\n\n下图展示了项目中核心类与接口的继承和组合关系：\n\n```mermaid\n" + classDSL + "\n```\n"
	}
	if seqDSL != "" {
		learningPath += "\n## 关键调用流程\n\n下图展示了系统中一条典型调用链的交互顺序：\n\n```mermaid\n" + seqDSL + "\n```\n"
	}

	overview += buildWhereToGoNext("00-overview.md", hasConcepts)
	whatItDoes += buildWhereToGoNext("01-what-it-does.md", hasConcepts)
	arch += buildWhereToGoNext("02-architecture.md", hasConcepts)
	projectStructure += buildWhereToGoNext("03-project-structure.md", hasConcepts)
	if keyConcepts != "" {
		keyConcepts += buildWhereToGoNext("04-key-concepts.md", hasConcepts)
	}
	learningPath += buildWhereToGoNext("05-learning-path.md", hasConcepts)
	apiRef += buildWhereToGoNext("api-reference.md", hasConcepts)

	// Clear checkpoint on success
	if cpPath != "" {
		_ = os.Remove(cpPath)
	}

	return &Wiki{
		ProjectName:         projectName,
		Language:            language,
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
		ChapterTitles:       chapterTitles,
		ModuleChineseNames:  moduleChineseNames,
		ThemeIntros:         themeIntros,
		ChapterNarratives:   chapterNarratives,
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

// projectDoc 表示一篇被探测到的项目文档。
type projectDoc struct {
	title   string // 相对项目根目录的路径，用于在 prompt 中标注来源
	content string
}

// docNameKeywords 匹配需求/设计/用户指导/PRD 等文档文件名（小写、中英文）。
// 顺序即优先级：靠前的文档对"项目是什么"更具说明价值。
var docNameKeywords = []string{
	"readme", "prd", "requirement", "需求",
	"design", "设计", "architecture", "架构",
	"spec", "specification", "规格",
	"overview", "概述", "introduction", "简介",
	"guide", "manual", "tutorial", "getting-started", "getting_started",
	"用户指南", "使用指南", "用户手册", "使用手册", "操作手册", "快速开始", "入门",
}

// docFileExts 限定可作为文档读取的文本扩展名。
var docFileExts = map[string]bool{
	".md": true, ".markdown": true, ".txt": true, ".rst": true, ".adoc": true,
}

// loadProjectDocs 在 README 之外，进一步探测需求/设计/用户指导/PRD 等中英文文档
// （项目根目录顶层 + 递归 doc/docs 目录），读取并合并关键内容，
// 为 LLM 提供真实的业务语义来源，避免在缺乏 README 时仅凭项目标题臆测。
func loadProjectDocs(dir string) string {
	if dir == "" {
		return ""
	}
	readme := loadProjectReadme(dir)
	docs := collectProjectDocs(dir)
	if readme == "" && len(docs) == 0 {
		return ""
	}

	var b strings.Builder
	const maxTotalRunes = 8000
	total := 0
	appendDoc := func(title, content string) {
		content = strings.TrimSpace(content)
		if content == "" || total >= maxTotalRunes {
			return
		}
		runes := []rune(content)
		if remain := maxTotalRunes - total; len(runes) > remain {
			runes = append(runes[:remain:remain], []rune("\n...（内容已截断）")...)
		}
		fmt.Fprintf(&b, "### %s\n%s\n\n", title, string(runes))
		total += len(runes)
	}
	if readme != "" {
		appendDoc("README", readme)
	}
	for _, d := range docs {
		appendDoc(d.title, d.content)
	}
	return strings.TrimSpace(b.String())
}

// collectProjectDocs 在根目录顶层及递归的 doc/docs 目录中，
// 按文件名关键词优先级查找文档，去重后返回（README 由 loadProjectReadme 单独处理）。
func collectProjectDocs(dir string) []projectDoc {
	// 1. 收集候选文件：根目录顶层 + doc/docs 递归（带遍历上限，避免大仓库开销）
	var candidates []string
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				candidates = append(candidates, filepath.Join(dir, e.Name()))
			}
		}
	}
	for _, sub := range []string{"doc", "docs"} {
		root := filepath.Join(dir, sub)
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			continue
		}
		walked := 0
		filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			walked++
			if walked > 500 {
				return filepath.SkipDir
			}
			if info != nil && !info.IsDir() {
				candidates = append(candidates, path)
			}
			return nil
		})
	}

	// 2. 按关键词优先级匹配并读取，去重，限制总篇数
	var result []projectDoc
	seen := map[string]bool{}
	const maxDocs = 12
	for _, kw := range docNameKeywords {
		for _, path := range candidates {
			if seen[path] {
				continue
			}
			base := strings.ToLower(filepath.Base(path))
			ext := strings.ToLower(filepath.Ext(path))
			if !docFileExts[ext] {
				continue
			}
			name := strings.TrimSuffix(base, ext)
			if !strings.Contains(name, kw) {
				continue
			}
			seen[path] = true
			// README 系列已由 loadProjectReadme 处理，跳过避免重复
			if strings.HasPrefix(name, "readme") {
				continue
			}
			content := readDocFile(path)
			if content == "" {
				continue
			}
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				rel = filepath.Base(path)
			}
			result = append(result, projectDoc{title: filepath.ToSlash(rel), content: content})
			if len(result) >= maxDocs {
				return result
			}
		}
	}
	return result
}

// readDocFile 读取单篇文档，去除 frontmatter 并按 rune 截断，避免 prompt 过长。
func readDocFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(stripFrontmatter(data))
	runes := []rune(text)
	if len(runes) > 3000 {
		text = string(runes[:3000]) + "\n...（已截断）"
	}
	return text
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

// extractArchitectureHints scans the README for paragraphs containing architecture-related
// keywords (架构/设计/分层/组件/模块/技术栈/architecture/layer/component/stack etc.)
// and returns a concatenated summary (max 800 chars) to give the LLM human-written design context.
func extractArchitectureHints(readme string) string {
	if readme == "" {
		return ""
	}
	keywords := []string{
		"架构", "设计", "分层", "组件", "模块", "技术栈", "依赖",
		"architecture", "layer", "component", "module", "stack",
		"design", "pattern", "framework", "infrastructure", "middleware",
		"plugin", "microservice", "monolith", "frontend", "backend",
	}
	paragraphs := strings.Split(readme, "\n\n")
	var hints []string
	totalChars := 0
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		hits := 0
		lower := strings.ToLower(p)
		for _, kw := range keywords {
			if strings.Contains(lower, strings.ToLower(kw)) {
				hits++
			}
		}
		if hits >= 2 {
			if totalChars+len(p) > 800 {
				// Truncate last paragraph if it would exceed 800
				remaining := 800 - totalChars
				if remaining > 50 {
					hints = append(hints, p[:remaining]+"...")
				}
				break
			}
			hints = append(hints, p)
			totalChars += len(p)
		}
	}
	if len(hints) == 0 {
		return ""
	}
	return "以下是从项目 README 中提取的架构相关信息，可作为设计意图的参考：\n\n" + strings.Join(hints, "\n\n")
}

func buildOverviewPrompt(graph *grapher.Graph, projectName, readme, language string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "你是一个资深软件架构师。请基于以下代码库信息，撰写一段项目概述（2-3 段）。\n\n")
	fmt.Fprintf(&b, "要求：\n")
	fmt.Fprintf(&b, "1. 描述这个项目的核心目标、主要功能和适用场景\n")
	fmt.Fprintf(&b, "2. 概括整体架构风格（如 MVC、微服务、单体、工具库等）\n")
	fmt.Fprintf(&b, "3. 只挑选 5-8 个最核心的模块深入说明其职责和协作，不要逐个列举所有模块\n")
	fmt.Fprintf(&b, "4. 禁止使用列表格式罗列模块名，用连续的叙事段落来描述架构逻辑\n")
	fmt.Fprintf(&b, "5. 每个段落末尾使用 `*来源：` 标注该段落涉及的核心文件，例如：\n")
	fmt.Fprintf(&b, "   本项目采用分层架构，将业务逻辑与数据访问解耦。\n")
	fmt.Fprintf(&b, "   *来源：`services/user_service.py`、`repositories/user_repository.py`*\n")
	fmt.Fprintf(&b, "6. 使用简体中文\n")
	fmt.Fprintf(&b, "7. 重要：项目名仅为生成时指定的展示标题，可能与真实业务无关，严禁据此推断项目的业务领域或用途；必须依据下方的项目文档与代码结构（模块名、类名、函数名）来判断项目实际做什么。若信息不足以确定业务领域，宁可如实概括代码结构，也不要臆造行业背景。\n\n")

	if readme != "" {
		fmt.Fprintf(&b, "【项目文档】\n%s\n\n", readme)
	}

	fmt.Fprintf(&b, "项目：%s\n", projectName)
	cleanNodes := filterNonNoiseModules(graph.Nodes)
	fmt.Fprintf(&b, "模块数：%d\n", len(cleanNodes))
	fmt.Fprintf(&b, "依赖数：%d\n", len(graph.Edges))
	if language != "" {
		fmt.Fprintf(&b, "编程语言：%s\n", language)
	}
	fmt.Fprintln(&b)

	if hint := languagePromptHint(language); hint != "" {
		fmt.Fprintf(&b, "【语言特性提示】\n%s\n\n", hint)
	}

	entries := graph.EntryPoints()
	important := sortNodesByImportance(cleanNodes, graph, entries)
	maxModulesInPrompt := 20
	fmt.Fprintf(&b, "核心模块列表（按重要性排序，供参考，不要原样复述）：\n")
	for i, n := range important {
		if i >= maxModulesInPrompt {
			fmt.Fprintf(&b, "... 还有 %d 个模块未列出\n", len(important)-maxModulesInPrompt)
			break
		}
		line := "- " + n.Filename
		if len(n.Classes) > 0 {
			line += fmt.Sprintf("（%d 个类）", len(n.Classes))
		}
		if len(n.Functions) > 0 {
			line += fmt.Sprintf("（%d 个函数）", len(n.Functions))
		}
		// 注入代表性类名/函数名，给 LLM 真实的代码语义而非仅模块名
		var ids []string
		for _, c := range n.Classes {
			if len(ids) >= 6 {
				break
			}
			ids = append(ids, c.Name)
		}
		for _, f := range n.Functions {
			if len(ids) >= 6 {
				break
			}
			ids = append(ids, f.Name)
		}
		if len(ids) > 0 {
			line += "：" + strings.Join(ids, ", ")
		}
		fmt.Fprintln(&b, line)
	}
	return b.String()
}

// isChecklistLike checks if the LLM output is just a repetition of module names
// rather than a real descriptive overview.
// reSourceAttr matches source attribution lines like `*来源：path/file.go*`
// which are required by the prompt and should not count toward checklist detection.
var reSourceAttr = regexp.MustCompile(`\*来源：.+?\*`)

func isChecklistLike(text string, graph *grapher.Graph) bool {
	if len(text) < 20 {
		return true // too short to be a real description
	}

	// Strip *来源：`...`* attribution lines — these are required by the prompt
	// and do not indicate checklist-like content.
	clean := reSourceAttr.ReplaceAllString(text, "")

	// Count how many module names appear in the cleaned text
	moduleHits := 0
	for _, n := range graph.Nodes {
		if strings.Contains(clean, n.Name) {
			moduleHits++
		}
	}

	// If more than 80% of module names are mentioned (and at least 12 modules), it's likely a checklist.
	// Require a minimum absolute count so small projects aren't penalised for mentioning most modules.
	if moduleHits >= 12 && float64(moduleHits)/float64(len(graph.Nodes)) > 0.8 {
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
// When the project contains multiple languages (language is "、" separated),
// it generates a balanced, non-biased hint that lists every language with its
// weight and instructs the LLM to treat each language fairly.
func languagePromptHint(language string) string {
	if language == "" {
		return ""
	}

	// Multi-language: the string contains "、" (e.g. "java（338 个文件）、python（112 个文件）")
	if strings.Contains(language, "、") {
		return fmt.Sprintf(
			"该项目使用多种编程语言：%s。\n"+
				"重要：这是一份客观的代码库文档，请不要偏向任何一种语言。分析时应依据各语言的代码结构与模块职责如实描述，不得将项目定性为「Java 项目」或「Python 项目」——它就是一个多语言混合项目。",
			language,
		)
	}

	// Single language — provide focused analysis hints.
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

func buildArchitecturePrompt(graph *grapher.Graph, projectName, readme, language string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "你是一位资深软件架构师，正在为 %s 项目的代码 Wiki 撰写架构说明。\n", projectName)
	fmt.Fprintf(&b, "目标读者是刚加入团队的开发者，他们需要在 5 分钟内理解系统的整体结构和设计逻辑。\n")
	fmt.Fprintf(&b, "注意：项目名仅为展示标题，可能与真实业务无关，请勿据此臆测业务领域，务必基于下方文档、目录结构与静态分析结果来判断。\n\n")

	// --- 项目文档架构信息提取 ---
	if readme != "" {
		hints := extractArchitectureHints(readme)
		if hints != "" {
			fmt.Fprintf(&b, "%s\n\n", hints)
		}
	}

	// --- 项目基本信息 ---
	cleanNodes := filterNonNoiseModules(graph.Nodes)
	fmt.Fprintf(&b, "项目：%s\n", projectName)
	fmt.Fprintf(&b, "有效模块数：%d（已过滤测试和调试模块）\n", len(cleanNodes))
	fmt.Fprintf(&b, "依赖边数：%d\n", len(graph.Edges))
	if language != "" {
		fmt.Fprintf(&b, "编程语言：%s\n", language)
	}
	if hint := languagePromptHint(language); hint != "" {
		fmt.Fprintf(&b, "语言特性提示：%s\n", hint)
	}
	fmt.Fprintln(&b)

	// --- 目录树 ---
	fmt.Fprintf(&b, "## 目录结构\n\n")
	fmt.Fprintf(&b, "```\n")
	fmt.Fprint(&b, buildProjectTree(graph))
	fmt.Fprintf(&b, "```\n\n")

	// --- 架构模式 + 入口点 + 循环依赖 ---
	pattern, rationale := inferArchitecturePattern(graph)
	fmt.Fprintf(&b, "【静态分析推断】\n")
	fmt.Fprintf(&b, "架构模式：%s — %s\n", pattern, rationale)

	entries := graph.EntryPoints()
	if len(entries) > 0 {
		fmt.Fprintf(&b, "入口模块：")
		for i, e := range entries {
			if i > 0 {
				fmt.Fprintf(&b, "、")
			}
			fmt.Fprintf(&b, "%s", e.Filename)
		}
		fmt.Fprintf(&b, "\n")
	}

	cycles := graph.DetectCycles()
	if len(cycles) > 0 {
		fmt.Fprintf(&b, "循环依赖：")
		for i, c := range cycles {
			if i > 0 {
				fmt.Fprintf(&b, "；")
			}
			fmt.Fprintf(&b, "%s", strings.Join(c.Nodes, " → "))
		}
		fmt.Fprintf(&b, "\n")
	}
	fmt.Fprintln(&b)

	// --- 模块详情（类、函数签名、依赖关系） ---
	roles := graph.InferModuleRoles()
	roleMap := make(map[string]string, len(roles))
	for _, r := range roles {
		roleMap[r.Name] = r.Role
	}

	const maxModules = 35
	fmt.Fprintf(&b, "## 模块详情\n\n")
	shown := 0
	for _, n := range cleanNodes {
		if shown >= maxModules {
			fmt.Fprintf(&b, "... 还有 %d 个模块未列出\n", len(cleanNodes)-maxModules)
			break
		}
		shown++

		role := roleMap[n.Name]
		if role == "" {
			role = "通用"
		}
		fmt.Fprintf(&b, "### %s（%s）\n\n", n.Filename, role)

		if len(n.Classes) > 0 {
			fmt.Fprintf(&b, "- 类：")
			for j, c := range n.Classes {
				if j > 0 {
					fmt.Fprintf(&b, "、")
				}
				methods := make([]string, len(c.Methods))
				for k, m := range c.Methods {
					methods[k] = m.Name
				}
				fmt.Fprintf(&b, "%s（方法：%s）", c.Name, strings.Join(methods, ", "))
			}
			fmt.Fprintf(&b, "\n")
		}

		if len(n.Functions) > 0 {
			fmt.Fprintf(&b, "- 函数：")
			funcs := n.Functions
			if len(funcs) > 5 {
				funcs = funcs[:5]
			}
			for j, f := range funcs {
				if j > 0 {
					fmt.Fprintf(&b, "、")
				}
				sig := f.Name + "("
				if len(f.Params) > 0 {
					sig += strings.Join(f.Params, ", ")
				}
				sig += ")"
				if f.ReturnType != "" {
					sig += " -> " + f.ReturnType
				}
				fmt.Fprintf(&b, "%s", sig)
			}
			fmt.Fprintf(&b, "\n")
		}

		deps := graph.DependenciesOf(n.Name)
		if len(deps) > 0 {
			fmt.Fprintf(&b, "- 依赖：%s\n", strings.Join(deps, ", "))
		}
		dependents := graph.DependentsOf(n.Name)
		if len(dependents) > 0 {
			fmt.Fprintf(&b, "- 被依赖：%s\n", strings.Join(dependents, ", "))
		}
		fmt.Fprintf(&b, "\n")
	}

	// --- 写作要求 ---
	fmt.Fprintf(&b, "## 写作要求\n\n")
	fmt.Fprintf(&b, "请撰写一篇 800-1500 字的架构说明（Markdown 格式），包含以下三个小节。\n\n")
	fmt.Fprintf(&b, "### 小节 1：## 功能架构\n")
	fmt.Fprintf(&b, "- 按\"系统能做什么\"的视角，将模块按功能域分组（如：认证、数据处理、API 网关、存储等）\n")
	fmt.Fprintf(&b, "- 1-2 段文字讲清每个功能域的职责和协作方式\n")
	fmt.Fprintf(&b, "- **画一张功能域依赖图**：```mermaid``` 格式，5-10 个节点，按功能域聚合模块\n")
	fmt.Fprintf(&b, "- 图中节点标注模块名而非功能域名，按功能域分组排列\n\n")
	fmt.Fprintf(&b, "### 小节 2：## 技术架构\n")
	fmt.Fprintf(&b, "- 按\"系统怎么构建\"的视角，分析技术分层和数据流（如：入口层→业务层→数据层）\n")
	fmt.Fprintf(&b, "- 1-2 段文字讲清技术分层逻辑和基础设施组件\n")
	fmt.Fprintf(&b, "- **画一张技术分层图**：```mermaid``` 格式，5-10 个节点，按层次排列\n")
	fmt.Fprintf(&b, "- 技术图侧重展示层次关系和基础设施依赖，与功能图不要重复\n\n")
	fmt.Fprintf(&b, "### 小节 3：## 关键设计决策\n")
	fmt.Fprintf(&b, "- 识别 2-3 个真正有深度的设计决策（不是模块清单，是设计思想）\n")
	fmt.Fprintf(&b, "- 每个决策说明：是什么、为什么这样设计、带来了什么好处/代价\n")
	fmt.Fprintf(&b, "- 用 Markdown 引用块（> 开头）标注关键的权衡理由\n\n")
	fmt.Fprintf(&b, "### 通用要求\n")
	fmt.Fprintf(&b, "- 每张 mermaid 图紧跟在其文字说明段落之后，形成\"文字→图→文字→图\"的穿插节奏\n")
	fmt.Fprintf(&b, "- 每张图只画 5-10 个核心模块 — 不要把所有模块塞进一张图\n")
	fmt.Fprintf(&b, "- 两张大图的模块可以有少量重叠，但视角不同（功能 vs 技术）\n")
	fmt.Fprintf(&b, "- 段落末尾用 `*来源：` 标注涉及的源文件路径\n")
	fmt.Fprintf(&b, "- 使用简体中文，叙事式风格，不要逐模块罗列\n")
	fmt.Fprintf(&b, "- 不要包含一级标题（# ），因为页面已有 # 架构说明 标题\n")
	fmt.Fprintf(&b, "- 如果 README 中有架构信息，优先参考其中的人类设计意图\n\n")
	fmt.Fprintf(&b, "直接输出 Markdown 正文，不要加 JSON 包装或代码围栏。")

	return b.String()
}

func buildWhatItDoesPrompt(graph *grapher.Graph, projectName, readme, language string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "你是一位资深技术作家，正在为一位新加入的开发者撰写项目介绍。\n\n")
	fmt.Fprintf(&b, "请基于以下代码库信息，撰写一篇\"%s 能做什么\"的介绍文章。\n", projectName)
	fmt.Fprintf(&b, "用 `# %s 能做什么` 作为文章标题。\n\n", projectName)
	fmt.Fprintf(&b, "要求：\n")
	fmt.Fprintf(&b, "1. 用第一人称\"本项目\"或\"该系统\"的口吻\n")
	fmt.Fprintf(&b, "2. 描述项目解决的核心问题、目标用户、主要使用场景\n")
	fmt.Fprintf(&b, "3. 用表格总结核心能力（至少3列：能力、说明、对应模块）\n")
	fmt.Fprintf(&b, "4. 不要只是罗列模块名称，要体现\"用这些模块能完成什么任务\"\n")
	fmt.Fprintf(&b, "5. 如果可能，提及设计哲学或独特之处\n")
	fmt.Fprintf(&b, "6. 每个段落末尾使用 `*来源：` 标注该段落涉及的核心文件，例如：\n")
	fmt.Fprintf(&b, "   本项目提供用户认证与授权能力，支持多种登录方式。\n")
	fmt.Fprintf(&b, "   *来源：`auth/service.py`、`oauth/providers.py`*\n")
	fmt.Fprintf(&b, "7. 使用简体中文\n")
	fmt.Fprintf(&b, "8. 重要：项目名仅为展示标题，可能与真实业务无关，严禁据此臆测业务领域或行业背景；必须依据下方项目文档与代码结构判断项目实际能力。\n\n")

	if readme != "" {
		fmt.Fprintf(&b, "【项目文档】\n%s\n\n", readme)
	}

	entries := graph.EntryPoints()
	filteredEntries := filterNonNoiseModules(entries)
	if len(filteredEntries) > 0 {
		fmt.Fprintf(&b, "【入口模块】\n")
		for _, e := range filteredEntries {
			fmt.Fprintf(&b, "- %s\n", e.Filename)
		}
		fmt.Fprintln(&b)
	}

	roles := graph.InferModuleRoles()
	fmt.Fprintf(&b, "【模块角色推断】\n")
	for _, r := range roles {
		if (r.Role == "核心领域" || r.Role == "入口层") && !isNoiseModule(r.Name) {
			fmt.Fprintf(&b, "- %s（%s，得分 %.3f）\n", r.Filename, r.Role, r.Score)
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

	cleanNodes := filterNonNoiseModules(graph.Nodes)
	fmt.Fprintf(&b, "项目：%s\n", projectName)
	fmt.Fprintf(&b, "模块数：%d\n", len(cleanNodes))
	fmt.Fprintf(&b, "依赖边数：%d\n\n", len(graph.Edges))

	// Provide architectural signals
	entries := filterNonNoiseModules(graph.EntryPoints())
	if len(entries) > 0 {
		fmt.Fprintf(&b, "【入口模块】\n")
		for _, e := range entries {
			fmt.Fprintf(&b, "- %s\n", e.Filename)
		}
		fmt.Fprintln(&b)
	}

	cycles := graph.DetectCycles()
	if len(cycles) > 0 {
		fmt.Fprintf(&b, "【循环依赖】\n")
		for _, c := range cycles {
			// Skip cycles involving noise modules
			allNoise := true
			for _, n := range c.Nodes {
				if !isNoiseModule(n) {
					allNoise = false
					break
				}
			}
			if !allNoise {
				fmt.Fprintf(&b, "- %s\n", strings.Join(c.Nodes, " → "))
			}
		}
		fmt.Fprintln(&b)
	}

	roles := graph.InferModuleRoles()
	fmt.Fprintf(&b, "【模块角色】\n")
	for _, r := range roles {
		if (r.Role == "核心领域" || r.Role == "入口层" || r.Role == "工具库") && !isNoiseModule(r.Name) {
			deps := graph.DependenciesOf(r.Name)
			dependents := graph.DependentsOf(r.Name)
			fmt.Fprintf(&b, "- %s（%s，依赖 %d 个模块，被 %d 个模块依赖）\n", r.Filename, r.Role, len(deps), len(dependents))
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
			fmt.Fprintf(&b, "- %s\n", e.Filename)
		}
		fmt.Fprintln(&b)
	}

	roles := graph.InferModuleRoles()
	fmt.Fprintf(&b, "【模块角色】\n")
	for _, r := range roles {
		if r.Role == "核心领域" || r.Role == "入口层" {
			fmt.Fprintf(&b, "- %s（%s）\n", r.Filename, r.Role)
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


// buildChapterTitlesPrompt builds an LLM prompt to generate book-like chapter titles.
func buildChapterTitlesPrompt(projectName string, themes map[string][]string, graph *grapher.Graph) string {
	var b strings.Builder
	fmt.Fprintf(&b, "你是一位资深技术文档编辑，正在为一本代码库技术书籍设计章节标题。\n\n")
	fmt.Fprintf(&b, "项目：%s\n\n", projectName)
	fmt.Fprintf(&b, "以下是按功能主题分组的模块列表。请为每个主题设计一个书籍式章节标题：\n\n")

	for _, theme := range sortedThemeKeys(themes) {
		mods := themes[theme]
		var modNames []string
		for _, m := range mods {
			modNames = append(modNames, m)
			if len(modNames) >= 5 {
				break
			}
		}
		fmt.Fprintf(&b, "主题：%s\n", theme)
		fmt.Fprintf(&b, "包含模块：%s\n\n", strings.Join(modNames, ", "))
	}

	fmt.Fprintf(&b, "要求：\n")
	fmt.Fprintf(&b, "1. 为每个主题生成：\n")
	fmt.Fprintf(&b, "   - 标题（4-8个中文字，像书名目录一样精炼有力，不要带'模块'二字）\n")
	fmt.Fprintf(&b, "   - 副标题（10-15字，解释本章内容，吸引读者阅读）\n")
	fmt.Fprintf(&b, "   - 难度（⭐ / ⭐⭐ / ⭐⭐⭐）\n")
	fmt.Fprintf(&b, "   - 学习目标（2-3条，每条10-20字，读完本章后读者能做什么）\n")
	fmt.Fprintf(&b, "   - 前置知识（0-2条，每条5-15字，阅读本章前需了解的概念，基础章节可为空数组）\n")
	fmt.Fprintf(&b, "2. 标题要体现该主题的核心价值，而不是模块名称的简单拼接\n")
	fmt.Fprintf(&b, "3. 按理解难度排序（基础→进阶→深入）\n")
	fmt.Fprintf(&b, "4. 使用简体中文\n\n")
	fmt.Fprintf(&b, "输出格式（JSON 数组，严格的 JSON 格式，不要输出其他内容）：\n")
	fmt.Fprintf(&b, `[{"theme":"主题名称","title":"章节标题","subtitle":"副标题说明","difficulty":"⭐⭐","learning_goals":["理解X的工作原理","掌握Y的配置方法"],"prerequisites":["了解基本Go语法"]}]`+"\n\n")
	fmt.Fprintf(&b, "现在请输出 JSON：")

	return b.String()
}

// generateChapterTitlesFallback generates chapter titles from theme names without LLM.
func generateChapterTitlesFallback(themes map[string][]string, graph *grapher.Graph) map[string]ChapterTitle {
	result := make(map[string]ChapterTitle)
	for _, theme := range sortedThemeKeys(themes) {
		mods := themes[theme]
		title := theme
		for _, suffix := range []string{"与", "和", "及"} {
			if idx := strings.Index(title, suffix); idx > 0 {
				title = title[:idx]
				break
			}
		}
		runes := []rune(title)
		if len(runes) > 8 {
			title = string(runes[:8])
		}
		var modNames []string
		for _, m := range mods {
			base := filepath.Base(m)
			modNames = append(modNames, base)
			if len(modNames) >= 3 {
				break
			}
		}
		subtitle := strings.Join(modNames, " · ")
		if len([]rune(subtitle)) > 15 {
			subtitle = string([]rune(subtitle)[:15]) + "…"
		}
		difficulty := "⭐⭐ 进阶"
		if len(mods) <= 1 {
			difficulty = "⭐ 入门"
		} else if len(mods) >= 5 {
			difficulty = "⭐⭐⭐ 深入"
		}
		result[theme] = ChapterTitle{
			Title:      title,
			Subtitle:   subtitle,
			Difficulty: difficulty,
		}
	}
	return result
}

// generateChapterTitles uses LLM (with fallback) to generate book-like chapter titles.
func generateChapterTitles(ctx context.Context, provider llm.Provider, projectName string, themes map[string][]string, graph *grapher.Graph) map[string]ChapterTitle {
	if provider == nil || len(themes) == 0 {
		return generateChapterTitlesFallback(themes, graph)
	}
	prompt := buildChapterTitlesPrompt(projectName, themes, graph)
	batchCtx, batchCancel := context.WithTimeout(ctx, 8*time.Minute)
	defer batchCancel()
	response, err := streamComplete(batchCtx, provider, prompt)
	if err != nil {
		warnf("LLM 生成章节标题失败 (%v)，使用静态回退", err)
		return generateChapterTitlesFallback(themes, graph)
	}
	response = strings.TrimSpace(response)
	if strings.HasPrefix(response, "```") {
		if idx := strings.Index(response, "\n"); idx != -1 {
			response = response[idx+1:]
		}
		if strings.HasSuffix(response, "```") {
			response = response[:len(response)-3]
		}
		response = strings.TrimSpace(response)
	}
	var entries []struct {
		Theme         string   `json:"theme"`
		Title         string   `json:"title"`
		Subtitle      string   `json:"subtitle"`
		Difficulty    string   `json:"difficulty"`
		LearningGoals []string `json:"learning_goals"`
		Prerequisites []string `json:"prerequisites"`
	}
	if err := json.Unmarshal([]byte(response), &entries); err != nil {
		warnf("解析 LLM 章节标题 JSON 失败 (%v)，使用静态回退", err)
		return generateChapterTitlesFallback(themes, graph)
	}
	result := make(map[string]ChapterTitle)
	for _, e := range entries {
		if e.Title == "" {
			continue
		}
		result[e.Theme] = ChapterTitle{
			Title:         e.Title,
			Subtitle:      e.Subtitle,
			Difficulty:    e.Difficulty,
			LearningGoals: e.LearningGoals,
			Prerequisites: e.Prerequisites,
		}
	}
	fallback := generateChapterTitlesFallback(themes, graph)
	for theme, ct := range fallback {
		if _, ok := result[theme]; !ok {
			result[theme] = ct
		}
	}
	return result
}
// generateThemeIntros uses LLM to generate 2-3 sentence introductions for each theme.
func generateThemeIntros(ctx context.Context, provider llm.Provider, projectName string, themes map[string][]string, titles map[string]ChapterTitle, graph *grapher.Graph) map[string]string {
	if provider == nil || len(themes) == 0 {
		return nil
	}
	prompt := buildThemeIntrosPrompt(projectName, themes, titles, graph)
	batchCtx, batchCancel := context.WithTimeout(ctx, 8*time.Minute)
	defer batchCancel()
	response, err := streamComplete(batchCtx, provider, prompt)
	if err != nil {
		warnf("LLM 生成主题简介失败 (%v)", err)
		return nil
	}
	response = strings.TrimSpace(response)
	if strings.HasPrefix(response, "```") {
		if idx := strings.Index(response, "\n"); idx != -1 {
			response = response[idx+1:]
		}
		if strings.HasSuffix(response, "```") {
			response = response[:len(response)-3]
		}
		response = strings.TrimSpace(response)
	}
	var entries []struct {
		Theme string `json:"theme"`
		Intro string `json:"intro"`
	}
	if err := json.Unmarshal([]byte(response), &entries); err != nil {
		warnf("解析 LLM 主题简介 JSON 失败 (%v)", err)
		return nil
	}
	result := make(map[string]string)
	for _, e := range entries {
		if e.Intro != "" {
			result[e.Theme] = e.Intro
		}
	}
	return result
}

// buildThemeIntrosPrompt builds the LLM prompt for theme introductions.
func buildThemeIntrosPrompt(projectName string, themes map[string][]string, titles map[string]ChapterTitle, graph *grapher.Graph) string {
	var b strings.Builder
	fmt.Fprintf(&b, "你是一位资深技术文档编辑，正在为一本代码库技术书籍撰写章节导语。\n\n")
	fmt.Fprintf(&b, "项目：%s\n\n", projectName)
	fmt.Fprintf(&b, "以下每个主题章节需要一段 2-3 句的引言，说明：\n")
	fmt.Fprintf(&b, "1. 该章节覆盖了什么系统能力\n")
	fmt.Fprintf(&b, "2. 各模块之间如何协作\n")
	fmt.Fprintf(&b, "3. 读者学完本章后能掌握什么\n\n")

	for _, theme := range sortedThemeKeys(themes) {
		mods := themes[theme]
		title := theme
		if ct, ok := titles[theme]; ok {
			title = ct.Title
		}
		fmt.Fprintf(&b, "**%s**（章节标题：%s）\n", theme, title)
		fmt.Fprintf(&b, "包含模块：%s\n\n", strings.Join(mods, ", "))
	}

	fmt.Fprintf(&b, "要求：\n")
	fmt.Fprintf(&b, "1. 每个主题生成一段 40-80 字的简介\n")
	fmt.Fprintf(&b, "2. 使用简体中文，风格专业但亲切\n")
	fmt.Fprintf(&b, "3. 不要重复章节标题\n\n")
	fmt.Fprintf(&b, "输出格式（严格的 JSON 数组，不要输出其他内容）：\n")
	fmt.Fprintf(&b, `[{"theme":"主题名称","intro":"简介内容"}]`+"\n\n")
	fmt.Fprintf(&b, "现在请输出 JSON：")

	return b.String()
}

// buildChapterNarrativePrompt builds an LLM prompt to generate a narrative article
// for one theme chapter, weaving its modules into a coherent learning story.
func buildChapterNarrativePrompt(projectName, theme string, title ChapterTitle, modules []string, graph *grapher.Graph) string {
	var b strings.Builder
	fmt.Fprintf(&b, "你是一位资深技术教程作者，正在为 %s 项目的代码 Wiki 撰写一篇教学文章。\n\n", projectName)
	fmt.Fprintf(&b, "本章主题：%s\n", title.Title)
	if title.Subtitle != "" {
		fmt.Fprintf(&b, "副标题：%s\n", title.Subtitle)
	}
	if title.Difficulty != "" {
		fmt.Fprintf(&b, "难度：%s\n", title.Difficulty)
	}
	fmt.Fprintf(&b, "\n## 本章包含的模块\n\n")

	roles := graph.InferModuleRoles()
	roleMap := make(map[string]string)
	for _, r := range roles {
		roleMap[r.Name] = r.Role
	}
	nodeMap := make(map[string]*grapher.Node, len(graph.Nodes))
	for _, n := range graph.Nodes {
		nodeMap[n.Name] = n
	}

	displayModules := modules
	if len(displayModules) > 10 {
		displayModules = displayModules[:10]
		fmt.Fprintf(&b, "（共 %d 个模块，展示前 10 个关键模块）\n\n", len(modules))
	}

	for _, modName := range displayModules {
		node := nodeMap[modName]
		if node == nil {
			fmt.Fprintf(&b, "- **%s**\n\n", modName)
			continue
		}
		resp := inferModuleResponsibility(node)
		role := roleMap[modName]
		if role == "" {
			role = "通用"
		}
		fmt.Fprintf(&b, "- **%s**\n  职责：%s\n  架构角色：%s\n", modName, resp, role)

		funcs := node.Functions
		if len(funcs) > 3 {
			funcs = funcs[:3]
		}
		if len(funcs) > 0 {
			fmt.Fprintf(&b, "  关键函数：")
			names := make([]string, len(funcs))
			for i, f := range funcs {
				names[i] = f.Name + "()"
			}
			fmt.Fprintf(&b, "%s\n", strings.Join(names, ", "))
		}

		deps := graph.DependenciesOf(modName)
		inTheme := filterInTheme(deps, modules)
		if len(inTheme) > 0 {
			fmt.Fprintf(&b, "  主题内依赖：%s\n", strings.Join(inTheme, ", "))
		}
		fmt.Fprintf(&b, "\n")
	}

	fmt.Fprintf(&b, "## 写作要求\n\n")
	fmt.Fprintf(&b, "请撰写一篇 800-1500 字的中文教学文章，要求：\n\n")
	fmt.Fprintf(&b, "1. **叙事式组织**：不要逐模块罗列，而是围绕本章主题用连贯的故事线把各模块串起来。\n")
	fmt.Fprintf(&b, "   解释这些模块如何协作完成该主题的功能，数据如何在它们之间流动。\n")
	fmt.Fprintf(&b, "2. **回答三个核心问题**：这个子系统是什么？为什么这样设计？读者理解它有什么价值？\n")
	fmt.Fprintf(&b, "3. **设计决策**：用 Markdown 引用块（> 开头）标注 1-2 个关键设计决策及其权衡理由。\n")
	fmt.Fprintf(&b, "4. **来源标注**：在关键技术描述处用 `*来源：[文件路径]*` 标注对应的源码文件。\n")
	fmt.Fprintf(&b, "5. **关键收获**：文章结尾用一个 \"## 关键收获\" 段落总结 3 个要点（用列表）。\n")
	fmt.Fprintf(&b, "6. **格式**：使用 Markdown 格式，包含 ## 和 ### 级别标题来组织段落。不要包含一级标题（# ）。\n")
	fmt.Fprintf(&b, "7. **风格**：像高质量技术博客一样，专业但不枯燥，让读者有「原来是这样」的体验。\n\n")
	fmt.Fprintf(&b, "直接输出 Markdown 文章正文，不要加任何 JSON 包装或代码围栏。")

	return b.String()
}

// buildSimplifiedNarrativePrompt builds a lighter prompt for degradation retry:
// keeps module names and a one-line role summary, drops functions and dependency details.
func buildSimplifiedNarrativePrompt(projectName, theme string, title ChapterTitle, modules []string, graph *grapher.Graph) string {
	var b strings.Builder
	fmt.Fprintf(&b, "为 %s 项目撰写「%s」主题的教学文章。\n\n模块：", projectName, title.Title)

	roles := graph.InferModuleRoles()
	roleMap := make(map[string]string)
	for _, r := range roles {
		roleMap[r.Name] = r.Role
	}

	capped := modules
	if len(capped) > 10 {
		capped = capped[:10]
	}

	names := make([]string, len(capped))
	for i, m := range capped {
		role := roleMap[m]
		if role == "" {
			role = "通用"
		}
		names[i] = fmt.Sprintf("%s(%s)", m, role)
	}
	fmt.Fprintf(&b, "%s\n", strings.Join(names, "、"))

	if len(modules) > 10 {
		fmt.Fprintf(&b, "（共 %d 个模块，仅展示前 10 个）\n", len(modules))
	}

	fmt.Fprintf(&b, "\n撰写 600-1000 字中文教学文章，Markdown 格式，不要 JSON 或代码围栏。")
	return b.String()
}

// buildMinimalNarrativePrompt builds the lightest possible prompt for last-resort retry.
func buildMinimalNarrativePrompt(projectName, theme string, title ChapterTitle, modules []string) string {
	var b strings.Builder
	capped := modules
	if len(capped) > 15 {
		capped = capped[:15]
	}
	fmt.Fprintf(&b, "为 %s 项目的「%s」主题撰写 400-800 字的中文技术概述。\n", projectName, title.Title)
	fmt.Fprintf(&b, "包含模块：%s", strings.Join(capped, "、"))
	if len(modules) > 15 {
		fmt.Fprintf(&b, "（共 %d 个）", len(modules))
	}
	fmt.Fprintf(&b, "\n用 Markdown 格式输出，不要 JSON 或代码围栏。")
	return b.String()
}

// filterInTheme returns only those names present in the theme's module list.
func filterInTheme(names []string, themeModules []string) []string {
	set := make(map[string]bool, len(themeModules))
	for _, m := range themeModules {
		set[m] = true
	}
	var result []string
	for _, n := range names {
		if set[n] {
			result = append(result, n)
		}
	}
	return result
}

// streamCollectWithLiveness reads tokens from ch until it closes or ctx is cancelled.
// If no token arrives within idleTimeout, it cancels and returns what it has.
func streamCollectWithLiveness(ctx context.Context, ch <-chan string, idleTimeout time.Duration) (string, bool) {
	var raw strings.Builder
	timer := time.NewTimer(idleTimeout)
	defer timer.Stop()
	charCount := 0
	for {
		select {
		case token, ok := <-ch:
			if !ok {
				return raw.String(), true
			}
			raw.WriteString(token)
			charCount += len([]rune(token))
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(idleTimeout)
		case <-timer.C:
			warnf("流式接收超过 %v 无新 token，中止", idleTimeout)
			return raw.String(), false
		case <-ctx.Done():
			return raw.String(), false
		}
	}
}

// streamComplete is a drop-in replacement for provider.Complete that uses streaming
// with liveness detection. If streaming fails (establish or idle timeout), it falls
// back to non-streaming Complete. This avoids HTTPClient.Timeout killing long-running
// requests while tolerating API gateways that buffer responses.
func streamComplete(ctx context.Context, provider llm.Provider, prompt string) (string, error) {
	ch, err := provider.CompleteStream(ctx, prompt)
	if err != nil {
		return provider.Complete(ctx, prompt)
	}

	// 3-minute idle timeout: some API gateways buffer the entire response before
	// forwarding the first token, so we need a generous window.
	response, completed := streamCollectWithLiveness(ctx, ch, 3*time.Minute)
	if response == "" {
		if !completed {
			warnf("流式超时，降级到非流式")
			return provider.Complete(ctx, prompt)
		}
		return "", fmt.Errorf("LLM 返回空响应")
	}
	return response, nil
}

// cleanNarrativeResponse strips code fences from an LLM response.
func cleanNarrativeResponse(response string) string {
	response = strings.TrimSpace(response)
	if strings.HasPrefix(response, "```") {
		if idx := strings.Index(response, "\n"); idx != -1 {
			response = response[idx+1:]
		}
		if strings.HasSuffix(response, "```") {
			response = response[:len(response)-3]
		}
		response = strings.TrimSpace(response)
	}
	return response
}

// tryStreamNarrative attempts to generate a narrative using CompleteStream.
func tryStreamNarrative(ctx context.Context, provider llm.Provider, prompt string, timeout time.Duration) (string, error) {
	sCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ch, err := provider.CompleteStream(sCtx, prompt)
	if err != nil {
		return "", fmt.Errorf("流建立失败: %w", err)
	}

	response, completed := streamCollectWithLiveness(sCtx, ch, 3*time.Minute)
	response = cleanNarrativeResponse(response)
	if response == "" {
		if !completed {
			return "", fmt.Errorf("流式接收中断，无有效内容")
		}
		return "", fmt.Errorf("LLM 返回空响应")
	}
	return response, nil
}

// tryCompleteNarrative attempts to generate a narrative using non-streaming Complete.
func tryCompleteNarrative(ctx context.Context, provider llm.Provider, prompt string, timeout time.Duration) (string, error) {
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	response, err := provider.Complete(cCtx, prompt)
	if err != nil {
		return "", err
	}
	response = cleanNarrativeResponse(response)
	if response == "" {
		return "", fmt.Errorf("LLM 返回空响应")
	}
	return response, nil
}

// generateChapterNarratives uses LLM to generate narrative articles for all theme chapters.
// It uses streaming mode with liveness detection and progressive degradation:
//   Level 1: full prompt + CompleteStream (15min)
//   Level 2: simplified prompt + CompleteStream (10min)
//   Level 3: minimal prompt + Complete non-streaming (8min)
//   Fail: skip this chapter
func generateChapterNarratives(ctx context.Context, provider llm.Provider, projectName string, themes map[string][]string, titles map[string]ChapterTitle, graph *grapher.Graph) map[string]string {
	if provider == nil || len(themes) == 0 {
		return nil
	}
	result := make(map[string]string)
	for _, theme := range sortedThemeKeys(themes) {
		modules := themes[theme]
		title, ok := titles[theme]
		if !ok {
			title = ChapterTitle{Title: theme}
		}

		var response string
		var err error

		// Level 1: 完整 prompt + 流式 (thinking 模式需要更长时间推理)
		prompt := buildChapterNarrativePrompt(projectName, theme, title, modules, graph)
		warnf("正在生成章节叙事（流式）：%s（prompt %d 字）...", title.Title, len([]rune(prompt)))
		response, err = tryStreamNarrative(ctx, provider, prompt, 15*time.Minute)
		if err != nil {
			warnf("Level-1 流式失败 (%v)，尝试精简 prompt...", err)

			// Level 2: 精简 prompt + 流式
			prompt = buildSimplifiedNarrativePrompt(projectName, theme, title, modules, graph)
			warnf("重试章节叙事（精简流式）：%s（prompt %d 字）...", title.Title, len([]rune(prompt)))
			response, err = tryStreamNarrative(ctx, provider, prompt, 10*time.Minute)
			if err != nil {
				warnf("Level-2 精简流式失败 (%v)，尝试极简非流式...", err)

				// Level 3: 极简 prompt + 非流式
				prompt = buildMinimalNarrativePrompt(projectName, theme, title, modules)
				warnf("最后重试（极简非流式）：%s（prompt %d 字）...", title.Title, len([]rune(prompt)))
				response, err = tryCompleteNarrative(ctx, provider, prompt, 8*time.Minute)
				if err != nil {
					warnf("所有级别均失败 (%v)，%s 将使用模块拼接模式", err, theme)
					continue
				}
			}
		}

		if isChecklistLike(response, graph) {
			warnf("LLM 返回的叙事像模块清单，%s 将使用模块拼接模式", theme)
			continue
		}

		result[theme] = response
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// generateSingleChapterNarrative generates a narrative article for one theme.
// Returns empty string on failure (caller decides fallback).
func generateSingleChapterNarrative(ctx context.Context, provider llm.Provider, projectName, theme string, title ChapterTitle, modules []string, graph *grapher.Graph) string {
	var response string
	var err error

	// Level 1: 完整 prompt + 流式
	prompt := buildChapterNarrativePrompt(projectName, theme, title, modules, graph)
	response, err = tryStreamNarrative(ctx, provider, prompt, 15*time.Minute)
	if err != nil {
		// Level 2: 精简 prompt + 流式
		prompt = buildSimplifiedNarrativePrompt(projectName, theme, title, modules, graph)
		response, err = tryStreamNarrative(ctx, provider, prompt, 10*time.Minute)
		if err != nil {
			// Level 3: 极简 prompt + 非流式
			prompt = buildMinimalNarrativePrompt(projectName, theme, title, modules)
			response, err = tryCompleteNarrative(ctx, provider, prompt, 8*time.Minute)
			if err != nil {
				return ""
			}
		}
	}

	if isChecklistLike(response, graph) {
		return ""
	}
	return response
}

// generateChapterNarrativesParallel generates narrative articles for all themes
// in parallel using goroutines. Falls back to sequential execution if provider is nil.
func generateChapterNarrativesParallel(ctx context.Context, provider llm.Provider, projectName string, themes map[string][]string, titles map[string]ChapterTitle, graph *grapher.Graph, pr *progressRenderer) map[string]string {
	if provider == nil || len(themes) == 0 {
		return nil
	}

	sorted := sortedThemeKeys(themes)
	result := make(map[string]string)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, theme := range sorted {
		modules := themes[theme]
		title, ok := titles[theme]
		if !ok {
			title = ChapterTitle{Title: theme}
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			pr.update("章节叙事", fmt.Sprintf("开始生成：%s", title.Title))
			narrative := generateSingleChapterNarrative(ctx, provider, projectName, theme, title, modules, graph)
			if narrative != "" {
				mu.Lock()
				result[theme] = narrative
				mu.Unlock()
			pr.log(fmt.Sprintf("[章节叙事] 完成：%s（%d 字）", title.Title, len([]rune(narrative))))
			} else {
			pr.log(fmt.Sprintf("[章节叙事] 失败：%s（将使用模块拼接模式）", title.Title))
			}
		}()
	}

	wg.Wait()

	if len(result) == 0 {
		return nil
	}
	return result
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

// extractKeyDesignDecisions extracts the "## 关键设计决策" section from
// the LLM-generated architecture narrative.  Returns the section content
// (including the heading) or "" if extraction fails.
func extractKeyDesignDecisions(narrative string) string {
	// Try the standard heading
	idx := strings.Index(narrative, "## 关键设计决策")
	if idx < 0 {
		// Fallback: look for "### 小节 3" style pattern
		idx = strings.Index(narrative, "关键设计决策")
		if idx < 0 {
			return ""
		}
		// Walk back to find the heading start
		start := strings.LastIndex(narrative[:idx], "\n## ")
		if start < 0 {
			start = strings.LastIndex(narrative[:idx], "\n### ")
		}
		if start >= 0 {
			idx = start + 1
		} else {
			// Just use the line start
			lineStart := strings.LastIndex(narrative[:idx], "\n")
			if lineStart >= 0 {
				idx = lineStart + 1
			} else {
				idx = 0
			}
		}
	}

	// Find the end: next "## " heading or end of text
	rest := narrative[idx+1:]
	nextH2 := strings.Index(rest, "\n## ")
	if nextH2 >= 0 {
		return narrative[idx : idx+1+nextH2]
	}
	return narrative[idx:]
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
func GenerateModuleDocs(graph *grapher.Graph, sourceDir string) map[string]string {
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

		// Meta: difficulty + role
		b.WriteString(fmt.Sprintf("**难度级别**：%s", inferModuleDifficulty(n, graph)))
		if role := roleMap[n.Name]; role != "" {
			b.WriteString(fmt.Sprintf(" | **架构角色**：%s", role))
		}
		b.WriteString("\n\n")

		// Narrative: what this module does
		b.WriteString("## 功能说明\n\n")
		b.WriteString(inferModuleResponsibility(n))
		var summaries []string
		for _, c := range n.Classes {
			summaries = append(summaries, fmt.Sprintf("定义了 `%s` 类", c.Name))
		}
		for _, f := range n.Functions {
			desc := describeFunction(f.Name, f.Params, f.ReturnType)
			summaries = append(summaries, fmt.Sprintf("`%s` — %s", f.Name, desc))
		}
		if len(summaries) > 0 {
			b.WriteString("详细如下：\n\n")
			for _, s := range summaries {
				b.WriteString(fmt.Sprintf("- %s\n", s))
			}
		}
		b.WriteString("\n")

		// Key code snippets (up to 3)
		if sourceDir != "" {
			snippets := ExtractSnippetsForNode(sourceDir, n, 3)
			if len(snippets) > 0 {
				b.WriteString("## 关键代码片段\n\n")
				for _, s := range snippets {
					b.WriteString(FormatSnippetMarkdown(s))
					b.WriteString("\n")
				}
			}
		}

		// Local dependency diagram
		neighbor := graph.NeighborSubGraph(n.Name)
		if len(neighbor.Nodes) > 1 && len(neighbor.Edges) > 0 {
			subDSL, err := diagram.GenerateSubArchDiagram(neighbor, n.Name)
			if err == nil && subDSL != "" {
				b.WriteString("## 模块依赖关系\n\n")
				b.WriteString("```mermaid\n")
				b.WriteString(subDSL)
				b.WriteString("```\n\n")
			}
		}

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
					b.WriteString("| 方法 | 参数 | 返回值 | 职责 |\n")
					b.WriteString("|------|------|--------|------|\n")
					for _, m := range c.Methods {
						params := strings.Join(m.Params, ", ")
						params = stripSelfParamStr(params)
						b.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s |\n", m.Name, params, m.ReturnType, describeFunction(m.Name, m.Params, m.ReturnType)))
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

// generateModuleThemes uses LLM streaming to semantically group modules into themes.
// Falls back to enhanced static grouping if LLM is unavailable or fails.
func generateModuleThemes(ctx context.Context, provider llm.Provider, projectName string, graph *grapher.Graph) map[string][]*grapher.Node {
	if provider == nil || graph == nil || len(graph.Nodes) == 0 {
		return groupModulesByTheme(graph)
	}

	prompt := buildModuleThemesPrompt(projectName, graph)
	streamCtx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()

	response, err := streamComplete(streamCtx, provider, prompt)
	if err != nil {
		warnf("LLM 分组失败 (%v)，使用静态回退", err)
		return groupModulesByTheme(graph)
	}
	response = strings.TrimSpace(response)
	if response == "" {
		warnf("LLM 分组返回空响应，使用静态回退")
		return groupModulesByTheme(graph)
	}

	// Strip markdown code fences if present
	if strings.HasPrefix(response, "```") {
		if idx := strings.Index(response, "\n"); idx != -1 {
			response = response[idx+1:]
		}
		if strings.HasSuffix(response, "```") {
			response = response[:len(response)-3]
		}
		response = strings.TrimSpace(response)
	}

	var entries []struct {
		Theme   string   `json:"theme"`
		Modules []string `json:"modules"`
	}
	if err := json.Unmarshal([]byte(response), &entries); err != nil {
		warnf("解析 LLM 主题分组 JSON 失败 (%v)，使用静态回退", err)
		return groupModulesByTheme(graph)
	}

	// Build result map and validate coverage
	result := make(map[string][]*grapher.Node)
	nodeMap := make(map[string]*grapher.Node)
	for _, n := range graph.Nodes {
		nodeMap[n.Name] = n
	}
	assigned := make(map[string]bool)

	for _, entry := range entries {
		if entry.Theme == "" || len(entry.Modules) == 0 {
			continue
		}
		for _, modName := range entry.Modules {
			if n, ok := nodeMap[modName]; ok {
				result[entry.Theme] = append(result[entry.Theme], n)
				assigned[modName] = true
			}
		}
	}

	// Collect any unassigned modules
	var unassigned []*grapher.Node
	for _, n := range graph.Nodes {
		if !assigned[n.Name] {
			unassigned = append(unassigned, n)
		}
	}

	// If too many unassigned or too few themes, use two-phase LLM fallback
	if len(unassigned) > len(graph.Nodes)/3 || len(result) < 2 {
		warnf("LLM 一轮覆盖率不足（%d/%d 未分配），启动两阶段回退...",
			len(unassigned), len(graph.Nodes))
		return generateModuleThemesTwoPhase(ctx, provider, projectName, graph, result, unassigned)
	}

	// Distribute unassigned modules to the closest theme by naming similarity
	for _, n := range unassigned {
		bestTheme := findClosestTheme(n.Name, result)
		result[bestTheme] = append(result[bestTheme], n)
	}

	// Remove "其他" / "Other" / "Misc" bucket — redistribute to real themes
	if otherNodes, ok := result["其他"]; ok {
		delete(result, "其他")
		for _, n := range otherNodes {
			best := findClosestTheme(n.Name, result)
			result[best] = append(result[best], n)
		}
	}
	if otherNodes, ok := result["Other"]; ok {
		delete(result, "Other")
		for _, n := range otherNodes {
			best := findClosestTheme(n.Name, result)
			result[best] = append(result[best], n)
		}
	}

	// Sort nodes within each group
	for _, nodes := range result {
		sort.Slice(nodes, func(i, j int) bool {
			return nodes[i].Name < nodes[j].Name
		})
	}

	return result
}

// findClosestTheme assigns a module to the theme with the most keyword overlap.
func findClosestTheme(modName string, themes map[string][]*grapher.Node) string {
	best := ""
	bestScore := -1
	lower := strings.ToLower(modName)
	for theme := range themes {
		score := keywordOverlap(lower, strings.ToLower(theme))
		if score > bestScore {
			bestScore = score
			best = theme
		}
	}
	if best == "" {
		return "核心模块"
	}
	return best
}

// keywordOverlap counts how many substrings of target appear in source.
func keywordOverlap(source, target string) int {
	parts := strings.FieldsFunc(target, func(r rune) bool {
		return r == '/' || r == '\\' || r == '_' || r == '-' || r == ' '
	})
	count := 0
	for _, p := range parts {
		if len(p) >= 2 && strings.Contains(source, p) {
			count++
		}
	}
	return count
}

// generateModuleThemesTwoPhase is the fallback when one-shot LLM grouping fails.
// Phase A: LLM defines 3-8 themes (names + 1-line descriptions only, no module lists).
// Phase B: LLM classifies unassigned modules into those themes (compact name-only list).
// Phase C: Residual modules assigned via findClosestTheme.
func generateModuleThemesTwoPhase(ctx context.Context, provider llm.Provider, projectName string, graph *grapher.Graph, partialResult map[string][]*grapher.Node, unassigned []*grapher.Node) map[string][]*grapher.Node {
	// Phase A: Get compact theme definitions from LLM
	themeDefs := requestThemeDefinitions(ctx, provider, projectName)
	if len(themeDefs) < 2 {
		warnf("两阶段回退 Phase A 失败，使用静态分组")
		return groupModulesByTheme(graph)
	}

	// Merge partial result themes into definitions
	themeNames := make(map[string]bool)
	for name := range themeDefs {
		themeNames[name] = true
	}
	for name := range partialResult {
		if !themeNames[name] {
			themeDefs[name] = name
		}
	}

	// Rebuild result from partial + theme defs
	result := make(map[string][]*grapher.Node)
	for name, nodes := range partialResult {
		result[name] = nodes
	}

	// Collect all unassigned module names (compact list for classification prompt)
	unassignedNames := make([]string, len(unassigned))
	for i, n := range unassigned {
		unassignedNames[i] = n.Name
	}

	// Phase B: LLM classifies unassigned modules into defined themes
	assignments := requestModuleClassification(ctx, provider, projectName, themeDefs, unassignedNames)
	nodeMap := make(map[string]*grapher.Node)
	for _, n := range graph.Nodes {
		nodeMap[n.Name] = n
	}

	assignedInPhaseB := make(map[string]bool)
	for themeName, modNames := range assignments {
		for _, mn := range modNames {
			if n, ok := nodeMap[mn]; ok {
				result[themeName] = append(result[themeName], n)
				assignedInPhaseB[mn] = true
			}
		}
	}

	// Phase C: findClosestTheme for any leftovers
	for _, n := range unassigned {
		if !assignedInPhaseB[n.Name] {
			best := findClosestTheme(n.Name, result)
			result[best] = append(result[best], n)
		}
	}

	warnf("两阶段回退完成：Phase A %d 主题 + Phase B %d 模块 + Phase C %d 残余",
		len(themeDefs), len(assignedInPhaseB), len(unassigned)-len(assignedInPhaseB))
	return result
}

// requestThemeDefinitions asks LLM to define 3-8 themes without assigning specific modules.
func requestThemeDefinitions(ctx context.Context, provider llm.Provider, projectName string) map[string]string {
	var b strings.Builder
	fmt.Fprintf(&b, "项目 %s 是一个代码库。请为该项目设计 3-8 个教学主题章节名称。\n", projectName)
	fmt.Fprintf(&b, "每个主题一行，格式：主题名|一句话描述\n")
	fmt.Fprintf(&b, "主题名 3-8 个中文，按从入门到深入的顺序排列。\n")
	fmt.Fprintf(&b, "例如：\n入口与命令行|程序的启动入口和参数解析\n数据持久化|数据库访问和存储层实现\n")
	fmt.Fprintf(&b, "\n现在请输出主题（只输出主题定义，不要输出其他内容）：\n")

	streamCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	response, err := streamComplete(streamCtx, provider, b.String())
	if err != nil || strings.TrimSpace(response) == "" {
		return nil
	}

	result := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(response), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		name := strings.TrimSpace(parts[0])
		if name == "" {
			continue
		}
		desc := ""
		if len(parts) > 1 {
			desc = strings.TrimSpace(parts[1])
		}
		result[name] = desc
	}
	return result
}

// requestModuleClassification asks LLM to classify module names into predefined themes.
func requestModuleClassification(ctx context.Context, provider llm.Provider, projectName string, themeDefs map[string]string, moduleNames []string) map[string][]string {
	var b strings.Builder
	fmt.Fprintf(&b, "项目 %s 有以下主题章节定义：\n\n", projectName)
	for name, desc := range themeDefs {
		fmt.Fprintf(&b, "- **%s**：%s\n", name, desc)
	}
	fmt.Fprintf(&b, "\n请将以下模块归入最合适的主题。输出严格 JSON：\n")
	fmt.Fprintf(&b, `[{"theme":"主题名","modules":["module/a","module/b"]}]`+"\n\n")
	fmt.Fprintf(&b, "## 待分类模块\n\n")
	for _, mn := range moduleNames {
		fmt.Fprintf(&b, "- %s\n", mn)
	}
	fmt.Fprintf(&b, "\n现在请输出 JSON：")

	streamCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	response, err := streamComplete(streamCtx, provider, b.String())
	if err != nil {
		return nil
	}
	response = strings.TrimSpace(response)
	if strings.HasPrefix(response, "```") {
		if idx := strings.Index(response, "\n"); idx != -1 {
			response = response[idx+1:]
		}
		if strings.HasSuffix(response, "```") {
			response = response[:len(response)-3]
		}
		response = strings.TrimSpace(response)
	}

	var entries []struct {
		Theme   string   `json:"theme"`
		Modules []string `json:"modules"`
	}
	if err := json.Unmarshal([]byte(response), &entries); err != nil {
		return nil
	}

	result := make(map[string][]string)
	for _, e := range entries {
		result[e.Theme] = append(result[e.Theme], e.Modules...)
	}
	return result
}

// buildModuleThemesPrompt builds the LLM prompt for semantic module grouping.
func buildModuleThemesPrompt(projectName string, graph *grapher.Graph) string {
	var b strings.Builder
	fmt.Fprintf(&b, "你是一位资深软件架构师，正在为一本代码库技术书籍设计目录结构。\n\n")
	cleanNodes := filterNonNoiseModules(graph.Nodes)
	fmt.Fprintf(&b, "项目：%s\n", projectName)
	fmt.Fprintf(&b, "模块总数：%d\n\n", len(cleanNodes))
	fmt.Fprintf(&b, "请分析以下所有模块，将它们按语义归入 3-8 个有意义的主题章节中。\n\n")

	// List all modules with their inferred responsibilities and roles
	roles := graph.InferModuleRoles()
	roleMap := make(map[string]string)
	for _, r := range roles {
		roleMap[r.Name] = r.Role
	}

	fmt.Fprintf(&b, "## 模块清单\n\n")
	for _, n := range cleanNodes {
		resp := inferModuleResponsibility(n)
		role := roleMap[n.Name]
		if role == "" {
			role = "通用"
		}
		// List top dependencies
		deps := graph.DependenciesOf(n.Name)
		depStr := strings.Join(deps, ", ")
		if len(deps) > 3 {
			depStr = strings.Join(deps[:3], ", ")
		}
		if depStr == "" {
			depStr = "无"
		}
		fmt.Fprintf(&b, "- **%s**\n  职责：%s\n  架构角色：%s\n  依赖：%s\n\n",
			n.Name, resp, role, depStr)
	}

	fmt.Fprintf(&b, "## 要求\n")
	fmt.Fprintf(&b, "1. 将每个模块归入恰好一个主题（**类型定义和接口**优先与使用它们的业务逻辑归入同一主题，不要单独成章）\n")
	fmt.Fprintf(&b, "2. 主题名称用 3-8 个中文字，体现该主题的核心价值，如\"入口与命令行\"\"数据持久化\"\n")
	fmt.Fprintf(&b, "3. 主题数量 3-8 个，每个主题至少包含 2 个模块\n")
	fmt.Fprintf(&b, "4. 不要创建\"其他\"\"杂项\"\"Misc\"类主题——每个模块都必须归入有意义的主题\n")
	fmt.Fprintf(&b, "5. 按理解难度排序（基础/入口 → 进阶/业务 → 深入/基础设施）\n\n")
	fmt.Fprintf(&b, "输出格式（严格的 JSON 数组，不要输出其他内容）：\n")
	fmt.Fprintf(&b, `[{"theme":"主题名","modules":["module/a","module/b"]}]`+"\n\n")
	fmt.Fprintf(&b, "现在请输出 JSON：")

	return b.String()
}

// groupModulesByTheme groups modules into thematic categories based on filename heuristics.
func groupModulesByTheme(graph *grapher.Graph) map[string][]*grapher.Node {
	groups := make(map[string][]*grapher.Node)
	for _, n := range filterNonNoiseModules(graph.Nodes) {
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
	// Extract the leaf filename and parent directory for keyword matching,
	// avoiding false matches on ancestor path segments (e.g. "testdata/...").
	clean := strings.ReplaceAll(n.Name, "\\", "/")
	base := strings.ToLower(filepath.Base(clean))
	parent := strings.ToLower(filepath.Base(filepath.Dir(clean)))

	match := func(ks ...string) bool {
		for _, k := range ks {
			if strings.Contains(base, k) || strings.Contains(parent, k) {
				return true
			}
		}
		return false
	}

	switch {
	case parent == "test" || parent == "tests" || parent == "spec" || parent == "specs":
		return "该模块包含测试代码，用于验证项目功能的正确性。"
	case match("cmd", "main"):
		return "该模块是项目的入口或命令行接口，负责解析参数并启动核心流程。"
	case match("api", "handler", "route", "router", "controller", "gateway"):
		return "该模块负责对外提供接口或路由处理，是系统与外部交互的边界。"
	case match("model", "entity", "domain"):
		return "该模块定义核心业务实体与领域模型，承载系统的核心数据结构。"
	case match("service", "usecase", "biz", "logic", "workflow"):
		return "该模块包含业务逻辑与服务编排，协调各组件完成具体业务功能。"
	case match("repo", "dao", "store", "repository"):
		return "该模块负责数据持久化与存储访问，封装对数据库或文件系统的操作。"
	case match("util", "helper", "common", "shared", "lib", "toolkit"):
		return "该模块提供通用工具函数与辅助逻辑，供其他模块复用。"
	case match("config", "setting", "env", "option"):
		return "该模块管理配置读取与环境适配，为系统运行提供参数支持。"
	case match("middleware", "intercept", "filter", "guard"):
		return "该模块实现横切关注点（如日志、认证、限流），在请求处理链中生效。"
	case match("client"):
		return "该模块封装对外部服务或资源的客户端访问逻辑。"
	case match("server"):
		return "该模块实现服务端监听与请求处理，是系统的网络入口。"
	case match("view", "ui", "component", "template", "page"):
		return "该模块负责界面展示或视图组件渲染，面向用户交互层。"
	case match("db", "database", "migration", "migrate"):
		return "该模块处理数据库连接、迁移或 schema 管理。"
	case match("cache", "redis", "memo", "buffer"):
		return "该模块实现缓存策略与高速数据存取，提升系统响应性能。"
	case match("queue", "worker", "job", "schedule", "cron"):
		return "该模块负责任务队列管理与异步工作流调度。"
	case match("grpc", "proto", "rpc"):
		return "该模块定义或实现远程过程调用（RPC）接口与协议序列化。"
	case match("schema", "types", "dto", "vo"):
		return "该模块声明数据结构、类型定义或数据传输对象（DTO）。"
	case match("logger", "log", "monitor", "trace", "metric"):
		return "该模块提供日志记录、监控埋点或可观测性支持。"
	case match("crypto", "encrypt", "hash", "sign", "security"):
		return "该模块提供加密、签名、哈希等安全相关功能。"
	default:
		return "该模块是项目的组成部分，承担特定的业务或技术职责。"
	}
}

// isNoiseModule reports whether a module name corresponds to test, debug, or
// benchmark scaffolding that should be excluded from architecture analysis.
func isNoiseModule(name string) bool {
	clean := strings.ReplaceAll(name, "\\", "/")
	lower := strings.ToLower(clean)
	if strings.HasSuffix(lower, "_test") || strings.HasSuffix(lower, "_test.go") {
		return true
	}
	if (strings.Contains(lower, "testdata/") || strings.Contains(lower, "/testdata")) && !strings.Contains(lower, "testdata/repos/") {
		return true
	}
	if strings.Contains(lower, "scripts/debug/") || strings.Contains(lower, "/scripts/debug") {
		return true
	}
	if strings.HasPrefix(lower, "benchmark/") && strings.HasSuffix(lower, "_test") {
		return true
	}
	return false
}

// filterNonNoiseModules returns only the nodes that are not test/debug noise.
func filterNonNoiseModules(nodes []*grapher.Node) []*grapher.Node {
	result := make([]*grapher.Node, 0, len(nodes))
	for _, n := range nodes {
		if !isNoiseModule(n.Name) {
			result = append(result, n)
		}
	}
	return result
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

// GenerateArchitectureMarkdown creates a narrative-driven architecture document
// with a top-level diagram and per-directory sub-system diagrams.
func GenerateArchitectureMarkdown(graph *grapher.Graph, narrative string) (string, error) {
	var b strings.Builder
	b.WriteString("# 架构\n\n")

	if narrative != "" {
		b.WriteString(narrative)
		b.WriteString("\n\n")
	}

	// Sub-system diagrams: one per directory group that has internal edges
	groups := graph.GroupByDirectory()
	type dirGroup struct {
		dir   string
		nodes []*grapher.Node
	}
	var validGroups []dirGroup
	for dir, nodes := range groups {
		clean := filterNonNoiseModules(nodes)
		if len(clean) < 2 {
			continue
		}
		sub := graph.SubGraphForDirectory(dir)
		if len(sub.Edges) == 0 {
			continue
		}
		validGroups = append(validGroups, dirGroup{dir: dir, nodes: clean})
	}
	sort.Slice(validGroups, func(i, j int) bool {
		return validGroups[i].dir < validGroups[j].dir
	})

	if len(validGroups) > 0 {
		b.WriteString("## 子系统详图\n\n")
		for _, g := range validGroups {
			sub := graph.SubGraphForDirectory(g.dir)
			title := g.dir + " 模块关系"
			dsl, err := diagram.GenerateSubArchDiagram(sub, title)
			if err != nil || dsl == "" {
				continue
			}
			b.WriteString(fmt.Sprintf("### %s\n\n", g.dir))
			b.WriteString("```mermaid\n")
			b.WriteString(dsl)
			b.WriteString("```\n\n")
		}
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
	files["index.html"] = GenerateStaticHTML(wiki, graph)

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


	// Chapter pages — standalone HTML for each theme with contextual sidebar
	if len(wiki.ModuleThemes) > 0 && graph != nil {
		chaptersDir := filepath.Join(outputDir, "chapters")
		if err := os.MkdirAll(chaptersDir, 0755); err != nil {
			fmt.Printf("警告：创建 chapters 目录失败 (%v)\n", err)
		} else {
			if wiki.ChapterPages == nil {
				wiki.ChapterPages = GenerateChapterPages(wiki, graph)
			}
			for theme, html := range wiki.ChapterPages {
				fname := safeThemeName(theme) + ".html"
				chapPath := filepath.Join(chaptersDir, fname)
				if err := os.WriteFile(chapPath, []byte(html), 0644); err != nil {
					fmt.Printf("警告：写入章节页面 %s 失败 (%v)\n", fname, err)
				}
			}
		}
	}
	// PDF export (best-effort: don't block other exports)
	pdfPath := filepath.Join(outputDir, "wiki.pdf")
	if HasChrome() {
		if err := GeneratePDFViaChrome(wiki, graph, pdfPath); err != nil {
			fmt.Printf("警告：Chrome PDF 导出失败 (%v)，尝试降级方案...\n", err)
			if pdfBytes, err2 := GeneratePDF(wiki); err2 == nil {
				if err2 := os.WriteFile(pdfPath, pdfBytes, 0644); err2 != nil {
					fmt.Printf("警告：写入 PDF 失败: %v\n", err2)
				}
			} else {
				fmt.Printf("警告：生成 PDF 失败: %v\n", err2)
			}
		}
	} else {
		if pdfBytes, err := GeneratePDF(wiki); err == nil {
			if err := os.WriteFile(pdfPath, pdfBytes, 0644); err != nil {
				fmt.Printf("警告：写入 PDF 失败: %v\n", err)
			}
		} else {
			fmt.Printf("警告：生成 PDF 失败: %v\n", err)
		}
	}

	return nil
}

// GenerateStaticHTML renders the entire Wiki as a single standalone HTML file
// suitable for offline viewing. It includes sidebar navigation, all Markdown
// content converted to HTML, and embedded Mermaid diagrams.
func GenerateStaticHTML(wiki *Wiki, graph *grapher.Graph) string {
	var fileTreeHTML string
	if graph != nil {
		fileTree := BuildFileTree(graph)
		fileTreeHTML = RenderFileTreeHTML(fileTree)
	}

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

	primaryExt := primaryExtForLang(wiki.Language)
	for _, sec := range sections {
		body.WriteString(fmt.Sprintf("<section id=\"%s\">\n", sec.id))
		body.WriteString(makeSourceRefsClickable(RenderMarkdownBody([]byte(sec.content)), primaryExt))
		body.WriteString("</section>\n")
	}

	// Diagrams are now embedded inside their thematic articles via Markdown
	// mermaid code blocks, so RenderMarkdownBody already renders them inline.
	// No standalone diagram sections needed.


	// Chapter listing — links to standalone chapter pages with contextual sidebar
	if len(wiki.ModuleThemes) > 0 {
		body.WriteString(`<section id="chapters">` + "\n")
		body.WriteString(`<h2>📕 深入剖析</h2>` + "\n")
		body.WriteString(`<p>以下章节按功能主题组织，每个章节包含相关模块的详细文档和上下文源码导航。</p>` + "\n")
		body.WriteString(`<div class="chapter-grid">` + "\n")
		for _, theme := range sortedThemeKeys(wiki.ModuleThemes) {
			safeName := safeThemeName(theme)
			ct, ok := wiki.ChapterTitles[theme]
			title := theme
			subtitle := ""
			diff := "⭐⭐ 进阶"
			if ok {
				title = ct.Title
				subtitle = ct.Subtitle
				if ct.Difficulty != "" {
					diff = ct.Difficulty
				}
			}
			modCount := len(wiki.ModuleThemes[theme])
			body.WriteString(fmt.Sprintf(`<a class="chapter-card" href="chapters/%s.html">`, safeName))
			body.WriteString(fmt.Sprintf(`<span class="chapter-card-title">%s</span>`, title))
			if subtitle != "" {
				body.WriteString(fmt.Sprintf(`<span class="chapter-card-subtitle">%s</span>`, subtitle))
			}
			body.WriteString(fmt.Sprintf(`<span class="chapter-card-meta"><span>%s</span><span>%d 个模块</span></span>`, diff, modCount))
			body.WriteString(`</a>` + "\n")
		}
		body.WriteString(`</div>` + "\n")
		body.WriteString(`</section>` + "\n")
	}
	var nav strings.Builder
	nav.WriteString(`<nav class="sidebar">
<div class="sidebar-header"><span class="logo-dot"></span><a href="#" style="color:inherit;text-decoration:none;font-weight:700;">`)
	nav.WriteString(HTMLEscape(wiki.ProjectName))
	nav.WriteString(`</a></div>
`)

		sectionIcons := map[string]string{
			"overview": "📊", "what-it-does": "🚀", "architecture": "🏗️",
			"project-structure": "📁", "key-concepts": "💡", "learning-path": "📚",
			"api-reference": "📖",
		}

		// 📘 认识项目
		introItems := filterSections(sections, "overview", "what-it-does")
		nav.WriteString(`<div class="nav-group">
	<div class="nav-group-header"><span class="nav-group-label">📘 认识项目</span><span class="nav-group-count">`)
		nav.WriteString(fmt.Sprintf("%d", len(introItems)))
		nav.WriteString(`</span><span class="chevron">&#9660;</span></div>
	<ul class="nav-group-items">
	`)
		for _, s := range introItems {
			icon := sectionIcons[s.id]
			if icon == "" {
				icon = "📄"
			}
			nav.WriteString(fmt.Sprintf(`<li><a href="#%s"><span class="nav-icon">%s</span><span class="nav-title">%s</span></a></li>`+"\n", s.id, icon, s.title))
		}
		nav.WriteString("</ul>\n</div>\n")

		// 📗 开始阅读
		readItems := filterSections(sections, "architecture", "project-structure")
		readCount := len(readItems)
		nav.WriteString(`<div class="nav-group">
	<div class="nav-group-header"><span class="nav-group-label">📗 开始阅读</span><span class="nav-group-count">`)
		nav.WriteString(fmt.Sprintf("%d", readCount))
		nav.WriteString(`</span><span class="chevron">&#9660;</span></div>
	<ul class="nav-group-items">
	`)
		for _, s := range readItems {
			icon := sectionIcons[s.id]
			if icon == "" {
				icon = "📄"
			}
			nav.WriteString(fmt.Sprintf(`<li><a href="#%s"><span class="nav-icon">%s</span><span class="nav-title">%s</span></a></li>`+"\n", s.id, icon, s.title))
		}
		nav.WriteString("</ul>\n</div>\n")

		// 📕 深入剖析
		nav.WriteString(`<div class="nav-group">
	<div class="nav-group-header"><span class="nav-group-label">📕 深入剖析</span><span class="nav-group-count">`)
		nav.WriteString(fmt.Sprintf("%d", len(wiki.ModuleThemes)))
		nav.WriteString(`</span><span class="chevron">&#9660;</span></div>
	<ul class="nav-group-items">
	`)
		if len(wiki.ModuleThemes) > 0 {
			for _, theme := range sortedThemeKeys(wiki.ModuleThemes) {
				safeName := safeThemeName(theme)
				ct, ok := wiki.ChapterTitles[theme]
				title := theme
				if ok {
					title = ct.Title
				}
				icon := "📦"
				if ok {
					icon = "🏷️"
				}
				nav.WriteString(fmt.Sprintf(`<li><a href="chapters/%s.html"><span class="nav-icon">%s</span><span class="nav-title">%s</span>`, safeName, icon, title))
				if ct.Difficulty != "" {
					nav.WriteString(fmt.Sprintf(`<span class="nav-meta"><span class="nav-diff">%s</span></span>`, ct.Difficulty))
				}
				nav.WriteString("</a></li>\n")
			}
		} else if len(wiki.ModuleDocs) > 0 {
			var count int
			for name := range wiki.ModuleDocs {
				if count >= 8 {
					break
				}
				secID := "module-" + mermaidEscape(name)
				nav.WriteString(fmt.Sprintf(`<li><a href="#%s"><span class="nav-icon">📦</span><span class="nav-title">%s</span></a></li>`+"\n", secID, filepath.Base(name)))
				count++
			}
		}
		nav.WriteString("</ul>\n</div>\n")

		// 📓 速查
		refItems := filterSections(sections, "key-concepts", "learning-path", "api-reference")
		nav.WriteString(`<div class="nav-group">
	<div class="nav-group-header"><span class="nav-group-label">📓 速查</span><span class="nav-group-count">`)
		nav.WriteString(fmt.Sprintf("%d", len(refItems)))
		nav.WriteString(`</span><span class="chevron">&#9660;</span></div>
	<ul class="nav-group-items">
	`)
		for _, s := range refItems {
			icon := sectionIcons[s.id]
			if icon == "" {
				icon = "📄"
			}
			nav.WriteString(fmt.Sprintf(`<li><a href="#%s"><span class="nav-icon">%s</span><span class="nav-title">%s</span></a></li>`+"\n", s.id, icon, s.title))
		}
		nav.WriteString("</ul>\n</div>\n")
		nav.WriteString("</nav>\n")

	var out strings.Builder
	out.WriteString(`<!DOCTYPE html>
<html lang="zh-CN" data-theme="light">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>`)
	out.WriteString(HTMLEscape(wiki.ProjectName))
	out.WriteString(` Wiki</title>
<style>`)
	out.WriteString(wikiPageCSS)
	out.WriteString(`
/* ---- Static HTML overrides: three-column layout ---- */
.content { margin-left:var(--sidebar-w); margin-right:280px; max-width:none; width:auto; }
.right-sidebar { width:280px; min-width:280px; background:rgba(246,248,250,.85); backdrop-filter:blur(12px) saturate(180%); -webkit-backdrop-filter:blur(12px) saturate(180%); border-left:1px solid var(--border2); height:100vh; position:fixed; right:0; top:0; overflow-y:auto; z-index:60; transition:background .3s; }
[data-theme="dark"] .right-sidebar { background:rgba(22,27,34,.88); }
.right-sidebar-header { padding:14px 18px; font-weight:700; font-size:14px; border-bottom:1px solid var(--border2); background:rgba(255,255,255,.6); backdrop-filter:blur(8px); position:sticky; top:0; z-index:3; color:var(--text2); display:flex; align-items:center; gap:8px; }
[data-theme="dark"] .right-sidebar-header { background:rgba(13,17,23,.6); }
.right-sidebar-header::before { content:''; width:8px; height:8px; border-radius:50%; background:#10b981; flex-shrink:0; }
.file-tree { padding:8px 0; font-size:13px; }
.file-tree details { margin:0; }
.file-tree summary { padding:6px 16px; cursor:pointer; color:var(--text3); font-weight:600; font-size:12px; user-select:none; transition:all .15s; }
.file-tree summary:hover { color:var(--accent); background:var(--accent-glow); }
.file-tree details[open]>summary { color:var(--text); }
.file-tree a { display:block; padding:4px 16px 4px 34px; color:var(--text2); text-decoration:none; font-size:12px; border-left:2px solid transparent; transition:all .15s; }
.file-tree a:hover { background:var(--accent-glow); color:var(--text); }
.file-tree a.active { background:var(--accent-glow); border-left-color:var(--accent); color:var(--accent); font-weight:600; }
.file-tree details details summary { padding-left:30px; font-size:11px; }
.file-tree details details a { padding-left:46px; }
section { scroll-margin-top:calc(var(--topbar-h) + 16px); }
@keyframes fadeUp2 { from{opacity:0;transform:translateY(12px)} to{opacity:1;transform:translateY(0)} }
section { animation:fadeUp2 .4s ease-out; }
</style>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/gh/highlightjs/cdn-release@11.9.0/build/styles/github-dark.min.css">
<script src="https://cdn.jsdelivr.net/gh/highlightjs/cdn-release@11.9.0/build/highlight.min.js"></script>
<script src="https://cdn.jsdelivr.net/npm/svg-pan-zoom@3.6.1/dist/svg-pan-zoom.min.js"></script>
<script type="module">
  import mermaid from 'https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.esm.min.mjs';
  mermaid.initialize({ startOnLoad:true, securityLevel:'loose', theme:'neutral' });
  window.addEventListener('load',function(){
    if(typeof svgPanZoom!=='undefined'){
      document.querySelectorAll('.mermaid svg').forEach(function(svg){svg.style.maxWidth='none';svgPanZoom(svg,{zoomEnabled:true,panEnabled:true,controlIconsEnabled:true,fit:true,center:true})});
    }
  });
</script>
`)
	out.WriteString(wikiPageJS)
	out.WriteString(`</head>
<body>
<div id="reading-progress"></div>
`)

	// Top bar
	out.WriteString(`<div class="topbar" style="left:var(--sidebar-w);right:280px">
<div class="topbar-title">`)
	out.WriteString(HTMLEscape(wiki.ProjectName))
	out.WriteString(` Wiki</div>
<div class="topbar-search">
<svg class="search-icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.35-4.35"/></svg>
<input type="text" id="topbar-search-trigger" placeholder="搜索文章、模块..." readonly>
<kbd>Ctrl+K</kbd>
</div>
<div class="topbar-actions">
<button id="theme-toggle" class="theme-toggle" title="切换主题"></button>
</div>
</div>
`)

	out.WriteString(nav.String())
	out.WriteString(`<div class="content">
`)
	out.WriteString(body.String())
	out.WriteString(`</div>
<aside class="right-sidebar">
<div class="right-sidebar-header">代码结构</div>
<div class="file-tree">
`)
	if fileTreeHTML != "" {
		out.WriteString(fileTreeHTML)
	} else {
		out.WriteString(`<div style="padding:16px;color:var(--text3);font-size:12px">暂无代码结构信息</div>`)
	}
	out.WriteString(`</div>
</aside>

<!-- Search overlay for static HTML -->
<div class="search-overlay" onclick="if(event.target===this)this.classList.remove('active')">
<div class="search-modal">
<input type="text" id="static-search-input" placeholder="搜索文章、模块..." oninput="filterStaticSearch(this.value)">
<div class="search-results" id="static-search-results"></div>
</div>
</div>

<script>
/* ---- File tree click & scroll spy ---- */
document.querySelectorAll('.file-tree a[data-target]').forEach(function(a) {
  a.addEventListener('click', function(e) {
    e.preventDefault();
    var target = document.getElementById(this.getAttribute('data-target'));
    if (target) {
      target.scrollIntoView({ behavior: 'smooth', block: 'start' });
      document.querySelectorAll('.file-tree a.active').forEach(function(el){el.classList.remove('active');});
      this.classList.add('active');
    }
  });
});

/* ---- Scroll spy: file tree + left nav ---- */
var observer = new IntersectionObserver(function(entries) {
  entries.forEach(function(entry) {
    if (!entry.isIntersecting) return;
    var id = entry.target.id;
    // Highlight file tree link
    var ftLink = document.querySelector('.file-tree a[data-target="' + id + '"]');
    if (ftLink) {
      document.querySelectorAll('.file-tree a.active').forEach(function(el){el.classList.remove('active');});
      ftLink.classList.add('active');
    }
    // Highlight sidebar nav link
    var navLink = document.querySelector('.sidebar a[href="#' + id + '"]');
    if (navLink) {
      document.querySelectorAll('.nav-group-items a.active').forEach(function(el){el.classList.remove('active');});
      navLink.classList.add('active');
    }
  });
}, { rootMargin: '-' + (52+20) + 'px 0px -70% 0px' });
document.querySelectorAll('section[id]').forEach(function(section) {
  observer.observe(section);
});

/* ---- Sidebar smooth scroll ---- */
document.querySelectorAll('.sidebar a[href^="#"]').forEach(function(a) {
  a.addEventListener('click', function(e) {
    e.preventDefault();
    var target = document.getElementById(this.getAttribute('href').substring(1));
    if (target) { target.scrollIntoView({ behavior: 'smooth', block: 'start' }); }
  });
});

/* ---- Static search ---- */
(function(){
  var trigger=document.getElementById('topbar-search-trigger');
  if(trigger)trigger.addEventListener('click',function(){
    document.querySelector('.search-overlay').classList.add('active');
    document.getElementById('static-search-input').focus();
  });
})();
function filterStaticSearch(q){
  var r=document.getElementById('static-search-results');
  q=q.toLowerCase();
  if(!q){r.innerHTML='';return;}
  var html='';
  document.querySelectorAll('section[id]').forEach(function(sec){
    var h=sec.querySelector('h1,h2,h3');
    if(!h)return;
    var t=h.textContent;
    if(t.toLowerCase().indexOf(q)>=0||sec.id.toLowerCase().indexOf(q)>=0){
      html+='<a class="search-hit" href="#'+sec.id+'" onclick="document.querySelector(\'.search-overlay\').classList.remove(\'active\')"><strong>'+t+'</strong><small>#'+sec.id+'</small></a>';
    }
  });
  r.innerHTML=html||'<div class="search-empty">未找到匹配结果</div>';
}
</script>
`)
	out.WriteString(SourcePopupJS)
	out.WriteString(`
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

	// 覆盖率策略（按函数总规模分段）：
	//   < 300      ：100% 全覆盖
	//   300 - 600  ：覆盖 50%
	//   > 600      ：覆盖 40%
	var target int
	switch {
	case total < 300:
		target = total
	case total <= 600:
		target = int(float64(total) * 0.5)
	default:
		target = int(float64(total) * 0.4)
	}
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
		warnf("LLM 函数描述返回格式无法解析，原始内容前 200 字：%q", response[:min(len(response), 200)])
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

// filterSections returns section items whose id matches one of the given names.
// buildDesignDecisions produces a narrative summary of key architectural choices
// inferred from PageRank, roles, cycles, and dependency structure.
func buildDesignDecisions(graph *grapher.Graph, roles []grapher.ModuleRole, roleMap map[string]string) string {
	var b strings.Builder

	// 1. Layering strategy
	pr := graph.PageRank()
	var coreMods, entryMods, utilMods []string
	for _, r := range roles {
		switch r.Role {
		case "核心领域":
			coreMods = append(coreMods, r.Name)
		case "入口层":
			entryMods = append(entryMods, r.Name)
		case "工具库":
			utilMods = append(utilMods, r.Name)
		}
	}
	if len(entryMods) > 0 && len(coreMods) > 0 {
		b.WriteString(fmt.Sprintf("**分层策略**：项目采用入口层-领域层-工具层的三层架构。`%s` 等入口模块接收外部请求，转发给 `%s` 等核心领域模块处理，`%s` 等工具模块提供跨层支撑。这种分层使得修改核心逻辑时不会影响入口层，新增功能时只需组合已有模块。\n\n",
			firstOf(entryMods), firstOf(coreMods), firstOr(utilMods, "—")))
	}

	// 2. Dependency direction
	entries := graph.EntryPoints()
	if len(entries) > 0 {
		b.WriteString(fmt.Sprintf("**依赖方向**：项目入口为 `%s`，依赖关系从入口向下游模块单向流动。", entries[0].Name))
	} else {
		b.WriteString("**依赖方向**：项目无明显单一入口，各模块通过相互依赖协作。")
	}

	// 3. Cycles as design tension
	cycles := graph.DetectCycles()
	if len(cycles) > 0 {
		b.WriteString(fmt.Sprintf("存在 %d 处循环依赖（%s），这些模块存在紧密的双向耦合，可能需要在重构时引入接口或合并模块来解决。\n",
			len(cycles), strings.Join(cycles[0].Nodes, " → ")))
	} else {
		b.WriteString("未检测到循环依赖，模块间耦合方向清晰。\n")
	}

	// 4. Central modules
	type prPair struct{ name string; score float64 }
	var prList []prPair
	for name, s := range pr {
		prList = append(prList, prPair{name, s})
	}
	sort.Slice(prList, func(i, j int) bool { return prList[i].score > prList[j].score })
	if len(prList) >= 2 {
		b.WriteString(fmt.Sprintf("**核心模块**：PageRank 分析表明 `%s`（%.2f）和 `%s`（%.2f）在依赖网络中处于中心位置，修改这些模块前应评估对上下游的影响。\n",
			prList[0].name, prList[0].score, prList[1].name, prList[1].score))
	}

	return b.String()
}

func firstOf(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

func firstOr(s []string, defaultVal string) string {
	if len(s) == 0 {
		return defaultVal
	}
	return s[0]
}

func filterSections(sections []struct{ id, title, content string }, names ...string) []struct{ id, title string } {
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}
	var result []struct{ id, title string }
	for _, s := range sections {
		if nameSet[s.id] {
			result = append(result, struct{ id, title string }{s.id, s.title})
		}
	}
	return result
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


// ---------- Source Code Snippet Extraction ----------

// CodeSnippet holds an extracted source code fragment for inline embedding.
type CodeSnippet struct {
	Filename  string
	StartLine int
	EndLine   int
	Code      string
	Label     string
	Language  string
}

// ExtractKeySnippets selects the most important code snippets from the entire graph.
func ExtractKeySnippets(sourceDir string, graph *grapher.Graph, maxSnippets int) []CodeSnippet {
	if sourceDir == "" || graph == nil || len(graph.Nodes) == 0 {
		return nil
	}

	pr := graph.PageRank()
	type candidate struct {
		node      *grapher.Node
		name      string
		startLine int
		endLine   int
		score     float64
	}

	var candidates []candidate
	for _, n := range graph.Nodes {
		nodeScore := pr[n.Name]
		for _, f := range n.Functions {
			if f.StartLine <= 0 {
				continue
			}
			end := f.EndLine
			if end <= f.StartLine {
				end = f.StartLine + 20
			}
			candidates = append(candidates, candidate{
				node: n, name: f.Name, startLine: f.StartLine, endLine: end,
				score: nodeScore*10 + float64(len(graph.DependentsOf(n.Name))),
			})
		}
		for _, c := range n.Classes {
			if c.StartLine <= 0 {
				continue
			}
			end := c.EndLine
			if end <= c.StartLine {
				end = c.StartLine + 30
			}
			candidates = append(candidates, candidate{
				node: n, name: c.Name, startLine: c.StartLine, endLine: end,
				score: nodeScore*10 + float64(len(graph.DependentsOf(n.Name))) + 5,
			})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	if maxSnippets > 0 && len(candidates) > maxSnippets {
		candidates = candidates[:maxSnippets]
	}

	var snippets []CodeSnippet
	for _, c := range candidates {
		code := readFileLines(sourceDir, c.node.Filename, c.startLine, c.endLine)
		if code == "" {
			continue
		}
		lang := inferLanguageFromFilename(c.node.Filename)
		snippets = append(snippets, CodeSnippet{
			Filename:  c.node.Filename,
			StartLine: c.startLine,
			EndLine:   c.endLine,
			Code:      code,
			Label:     c.name,
			Language:  lang,
		})
	}
	return snippets
}

// ExtractSnippetsForNode extracts code snippets for a specific module node.
func ExtractSnippetsForNode(sourceDir string, node *grapher.Node, maxSnippets int) []CodeSnippet {
	if sourceDir == "" || node == nil {
		return nil
	}

	type candidate struct {
		name      string
		startLine int
		endLine   int
		isClass   bool
	}

	var candidates []candidate
	for _, f := range node.Functions {
		if f.StartLine <= 0 {
			continue
		}
		end := f.EndLine
		if end <= f.StartLine {
			end = f.StartLine + 20
		}
		candidates = append(candidates, candidate{name: f.Name, startLine: f.StartLine, endLine: end})
	}
	for _, c := range node.Classes {
		if c.StartLine <= 0 {
			continue
		}
		end := c.EndLine
		if end <= c.StartLine {
			end = c.StartLine + 30
		}
		candidates = append(candidates, candidate{name: c.Name, startLine: c.StartLine, endLine: end, isClass: true})
	}

	if maxSnippets > 0 && len(candidates) > maxSnippets {
		candidates = candidates[:maxSnippets]
	}

	lang := inferLanguageFromFilename(node.Filename)
	var snippets []CodeSnippet
	for _, c := range candidates {
		code := readFileLines(sourceDir, node.Filename, c.startLine, c.endLine)
		if code == "" {
			continue
		}
		snippets = append(snippets, CodeSnippet{
			Filename:  node.Filename,
			StartLine: c.startLine,
			EndLine:   c.endLine,
			Code:      code,
			Label:     c.name,
			Language:  lang,
		})
	}
	return snippets
}

// FormatSnippetMarkdown formats a CodeSnippet as Markdown with source attribution.
func FormatSnippetMarkdown(s CodeSnippet) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("**%s** — `%s:%d-%d`\n\n", s.Label, s.Filename, s.StartLine, s.EndLine))
	b.WriteString("```" + s.Language + "\n")
	b.WriteString(s.Code)
	b.WriteString("\n```\n")
	return b.String()
}

// readFileLines reads a range of lines from a source file.
func readFileLines(sourceDir, filename string, startLine, endLine int) string {
	ext := filepath.Ext(filename)
	// Try exact path first, then common variations.
	// When filename is already absolute (e.g. from grapher.Node.Filename),
	// use it directly; filepath.Join may not handle cross-platform absolute
	// paths correctly (e.g. Windows drive-letter paths under WSL).
	var paths []string
	if filepath.IsAbs(filename) {
		paths = append(paths, filename)
	} else {
		paths = append(paths, filename) // filename may already include sourceDir prefix
	}
	paths = append(paths, filepath.Join(sourceDir, filename))
	if ext == "" {
		paths = append(paths, filepath.Join(sourceDir, filename+".py"),
			filepath.Join(sourceDir, filename+".go"),
			filepath.Join(sourceDir, filename+".js"),
			filepath.Join(sourceDir, filename+".ts"),
			filepath.Join(sourceDir, filename+".java"))
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		if startLine < 1 {
			startLine = 1
		}
		if endLine > len(lines) {
			endLine = len(lines)
		}
		if startLine > len(lines) {
			return ""
		}
		selected := lines[startLine-1 : endLine]
		return strings.TrimRight(strings.Join(selected, "\n"), "\n")
	}
	return ""
}

// inferLanguageFromFilename returns a language tag for syntax highlighting.
func inferLanguageFromFilename(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".py":
		return "python"
	case ".go":
		return "go"
	case ".js":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".java":
		return "java"
	case ".rs":
		return "rust"
	case ".cpp", ".cc", ".c":
		return "cpp"
	default:
		return ""
	}
}

// coreModule represents a module selected for its architectural significance.
type coreModule struct {
	Node   *grapher.Node
	Reason string
}

// selectCoreModules picks up to 3 architecturally significant modules using
// distinct criteria: entry point, most depended-on, and most class-rich.
func selectCoreModules(graph *grapher.Graph) []coreModule {
	cleanNodes := filterNonNoiseModules(graph.Nodes)
	if len(cleanNodes) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var result []coreModule

	// Criterion 1: first non-noise entry point
	entries := filterNonNoiseModules(graph.EntryPoints())
	if len(entries) > 0 {
		e := entries[0]
		result = append(result, coreModule{Node: e, Reason: "项目入口，负责启动和初始化"})
		seen[e.Name] = true
	}

	// Criterion 2: most depended-on module
	var hub *grapher.Node
	maxDep := 0
	for _, n := range cleanNodes {
		if seen[n.Name] {
			continue
		}
		d := len(graph.DependentsOf(n.Name))
		if d > maxDep {
			maxDep = d
			hub = n
		}
	}
	if hub != nil && maxDep > 0 {
		result = append(result, coreModule{Node: hub, Reason: fmt.Sprintf("被 %d 个模块依赖，是依赖图的中心节点", maxDep)})
		seen[hub.Name] = true
	}

	// Criterion 3: most classes
	var rich *grapher.Node
	maxClass := 0
	for _, n := range cleanNodes {
		if seen[n.Name] {
			continue
		}
		if len(n.Classes) > maxClass {
			maxClass = len(n.Classes)
			rich = n
		}
	}
	if rich != nil && maxClass > 0 {
		result = append(result, coreModule{Node: rich, Reason: fmt.Sprintf("定义了 %d 个核心类，承载领域模型", maxClass)})
	} else if rich != nil {
		result = append(result, coreModule{Node: rich, Reason: "模块结构清晰，是重要的业务组件"})
	}

	return result
}

// buildCoreModuleSourceSection generates a markdown section showcasing
// architecturally significant modules with code snippets and rationale.
func buildCoreModuleSourceSection(graph *grapher.Graph, sourceDir string) string {
	modules := selectCoreModules(graph)
	if len(modules) == 0 || sourceDir == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\n## 核心模块源码\n\n")
	b.WriteString("> 以下模块从入口、依赖中心、领域模型三个维度代表系统的架构核心。\n\n")

	for _, cm := range modules {
		n := cm.Node
		fmt.Fprintf(&b, "### `%s` — %s\n\n", n.Filename, cm.Reason)

		snippets := ExtractSnippetsForNode(sourceDir, n, 2)
		if len(snippets) == 0 {
			continue
		}
		for _, s := range snippets {
			b.WriteString(FormatSnippetMarkdown(s) + "\n")
		}
	}

	return b.String()
}


// collectCoreClasses returns the most important classes across the graph.
func collectCoreClasses(graph *grapher.Graph, maxClasses int) []analyzer.ClassInfo {
	pr := graph.PageRank()
	type classRef struct {
		class analyzer.ClassInfo
		score float64
	}
	var all []classRef
	for _, node := range graph.Nodes {
		for _, c := range node.Classes {
			all = append(all, classRef{class: c, score: pr[node.Name]})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].score > all[j].score })
	seen := make(map[string]bool)
	var result []analyzer.ClassInfo
	for _, ref := range all {
		if seen[ref.class.Name] {
			continue
		}
		seen[ref.class.Name] = true
		result = append(result, ref.class)
		if len(result) >= maxClasses {
			break
		}
	}
	return result
}

// EmbedContextualContent injects contextually relevant diagrams and code snippets
// into each wiki article based on the article'sthematic focus.
func EmbedContextualContent(wiki *Wiki, graph *grapher.Graph, sourceDir string, sequences []sequencer.Sequence) {
	if wiki == nil || graph == nil {
		return
	}

	// Overview: entry point snippet
	entries := graph.EntryPoints()
	if len(entries) > 0 && sourceDir != "" {
		snippets := ExtractSnippetsForNode(sourceDir, entries[0], 2)
		if len(snippets) > 0 {
			wiki.Overview += "\n## 入口代码\n\n"
			for _, s := range snippets {
				wiki.Overview += FormatSnippetMarkdown(s) + "\n"
			}
		}
	}

	// Key Concepts: class diagram for top classes
	coreClasses := collectCoreClasses(graph, 10)
	if len(coreClasses) > 0 {
		if classDSL, err := diagram.GenerateSubClassDiagram(coreClasses, "核心类型关系"); err == nil && classDSL != "" {
			wiki.KeyConcepts += "\n## 类型关系图\n\n"
			wiki.KeyConcepts += "```mermaid\n" + classDSL + "\n```\n"
		}
	}

	// Learning Path: up to 3 sequence diagrams
	for i := 0; i < len(sequences) && i < 3; i++ {
		seq := sequences[i]
		seqDSL := sequencer.GenerateSequenceDiagram(seq)
		if seqDSL == "" {
			continue
		}
		title := seq.Description
		if title == "" {
			title = fmt.Sprintf("调用流程 %d", i+1)
		}
		wiki.LearningPath += fmt.Sprintf("\n## %s\n\n", title)
		wiki.LearningPath += "```mermaid\n" + seqDSL + "\n```\n"
	}

	// Project Structure: directory-level sub-diagrams (up to 3)
	groups := graph.GroupByDirectory()
	count := 0
	for dir, nodes := range groups {
		if count >= 3 {
			break
		}
		if dir == "." || dir == "" || len(nodes) < 2 {
			continue
		}
		sub := graph.SubGraphForDirectory(dir)
		if len(sub.Edges) == 0 {
			continue
		}
		subDSL, err := diagram.GenerateSubArchDiagram(sub, dir)
		if err != nil || subDSL == "" {
			continue
		}
		wiki.ProjectStructure += fmt.Sprintf("\n## %s 目录依赖图\n\n", dir)
		wiki.ProjectStructure += "```mermaid\n" + subDSL + "\n```\n"
		count++
	}
}

// ---------- File Tree for Code Navigation ----------

// FileTreeNode represents a node in the code structure tree.
type FileTreeNode struct {
	Name     string
	Path     string
	IsDir    bool
	Children []*FileTreeNode
}

// BuildFileTree constructs a directory tree from graph nodes.
func BuildFileTree(graph *grapher.Graph) *FileTreeNode {
	root := &FileTreeNode{Name: ".", Path: "", IsDir: true}
	dirSet := make(map[string]bool)
	for _, n := range graph.Nodes {
		fn := strings.ReplaceAll(n.Filename, "\\", "/")
		parts := strings.Split(fn, "/")
		for i := 0; i < len(parts)-1; i++ {
			dirSet[strings.Join(parts[:i+1], "/")] = true
		}
	}

	nodeByPath := make(map[string]*FileTreeNode)
	nodeByPath[""] = root

	var dirs []string
	for d := range dirSet {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		parent := ""
		if idx := strings.LastIndex(d, "/"); idx >= 0 {
			parent = d[:idx]
		}
		name := d
		if idx := strings.LastIndex(d, "/"); idx >= 0 {
			name = d[idx+1:]
		}
		child := &FileTreeNode{Name: name, Path: d, IsDir: true}
		if p, ok := nodeByPath[parent]; ok {
			p.Children = append(p.Children, child)
		}
		nodeByPath[d] = child
	}

	for _, n := range graph.Nodes {
		fn := strings.ReplaceAll(n.Filename, "\\", "/")
		parent := ""
		if idx := strings.LastIndex(fn, "/"); idx >= 0 {
			parent = fn[:idx]
		}
		name := fn
		if idx := strings.LastIndex(fn, "/"); idx >= 0 {
			name = fn[idx+1:]
		}
		safe := strings.ReplaceAll(n.Name, "/", "_")
		safe = strings.ReplaceAll(safe, "\\", "_")
		safe = strings.ReplaceAll(safe, ":", "_")
		child := &FileTreeNode{Name: name, Path: "module-" + safe, IsDir: false}
		if p, ok := nodeByPath[parent]; ok {
			p.Children = append(p.Children, child)
		} else {
			root.Children = append(root.Children, child)
		}
	}

	var sortNode func(n *FileTreeNode)
	sortNode = func(n *FileTreeNode) {
		sort.Slice(n.Children, func(i, j int) bool {
			if n.Children[i].IsDir != n.Children[j].IsDir {
				return n.Children[i].IsDir
			}
			return n.Children[i].Name < n.Children[j].Name
		})
		for _, c := range n.Children {
			sortNode(c)
		}
	}
	sortNode(root)
	return root
}

// RenderFileTreeHTML renders a FileTreeNode as collapsible HTML.
func RenderFileTreeHTML(node *FileTreeNode) string {
	var b strings.Builder
	renderFileTreeNode(&b, node, 0)
	return b.String()
}

func renderFileTreeNode(b *strings.Builder, node *FileTreeNode, depth int) {
	if node.Name == "." {
		for _, c := range node.Children {
			renderFileTreeNode(b, c, 0)
		}
		return
	}
	if node.IsDir {
		b.WriteString("<details class=\"file-tree-dir\" open>\n")
		b.WriteString(fmt.Sprintf("<summary>%s</summary>\n", node.Name))
		for _, c := range node.Children {
			renderFileTreeNode(b, c, depth+1)
		}
		b.WriteString("</details>\n")
	} else {
		b.WriteString(fmt.Sprintf("<a href=\"#%s\" class=\"file-tree-link\" data-target=\"%s\">%s</a>\n",
			node.Path, node.Path, node.Name))
	}
}


// buildProjectStructurePrompt creates an LLM prompt to generate a narrative
// project structure with multiple focused mermaid diagrams (5-8 nodes each),
// replacing the old static role→description lookup table.
func buildProjectStructurePrompt(graph *grapher.Graph, projectName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "你是一位资深软件架构师。请为 %s 项目撰写一篇\"项目结构详解\"叙事文章。\n\n", projectName)

	// Filter and prepare module data
	cleanNodes := filterNonNoiseModules(graph.Nodes)
	roles := graph.InferModuleRoles()
	roleMap := make(map[string]string, len(roles))
	for _, r := range roles {
		roleMap[r.Name] = r.Role
	}

	// Directory tree for context
	fmt.Fprintf(&b, "## 目录结构\n\n")
	fmt.Fprintf(&b, "```\n")
	fmt.Fprint(&b, buildProjectTree(graph))
	fmt.Fprintf(&b, "```\n\n")

	// Module details (up to 40 to keep prompt manageable)
	const maxModules = 40
	fmt.Fprintf(&b, "## 模块详情\n\n")
	for i, n := range cleanNodes {
		if i >= maxModules {
			fmt.Fprintf(&b, "... 还有 %d 个模块未列出\n", len(cleanNodes)-maxModules)
			break
		}
		fmt.Fprintf(&b, "### %s\n\n", n.Filename)

		if len(n.Classes) > 0 {
			fmt.Fprintf(&b, "- 类：")
			for j, c := range n.Classes {
				if j > 0 {
					fmt.Fprintf(&b, "、")
				}
				fmt.Fprintf(&b, "%s", c.Name)
				if len(c.Methods) > 0 {
					methodNames := make([]string, len(c.Methods))
					for k, m := range c.Methods {
						methodNames[k] = m.Name
					}
					fmt.Fprintf(&b, "（方法：%s）", strings.Join(methodNames, ", "))
				}
			}
			fmt.Fprintf(&b, "\n")
		}

		if len(n.Functions) > 0 {
			fmt.Fprintf(&b, "- 函数：")
			for j, f := range n.Functions {
				if j > 0 {
					fmt.Fprintf(&b, "、")
				}
				sig := f.Name + "("
				if len(f.Params) > 0 {
					sig += strings.Join(f.Params, ", ")
				}
				sig += ")"
				if f.ReturnType != "" {
					sig += " -> " + f.ReturnType
				}
				fmt.Fprintf(&b, "%s", sig)
			}
			fmt.Fprintf(&b, "\n")
		}

		deps := graph.DependenciesOf(n.Name)
		if len(deps) > 0 {
			fmt.Fprintf(&b, "- 依赖：%s\n", strings.Join(deps, ", "))
		}

		dependents := graph.DependentsOf(n.Name)
		if len(dependents) > 0 {
			fmt.Fprintf(&b, "- 被依赖：%s\n", strings.Join(dependents, ", "))
		}

		if role := roleMap[n.Name]; role != "" {
			fmt.Fprintf(&b, "- 架构角色：%s\n", role)
		}
		fmt.Fprintf(&b, "\n")
	}

	// Full dependency edges for LLM to decide diagram grouping
	fmt.Fprintf(&b, "## 全部依赖边\n\n")
	for _, e := range graph.Edges {
		fmt.Fprintf(&b, "- %s → %s\n", e.From, e.To)
	}
	fmt.Fprintf(&b, "\n")

	// Writing instructions
	fmt.Fprintf(&b, "## 写作要求\n\n")
	fmt.Fprintf(&b, "请撰写一篇 Markdown 格式的项目结构说明，要求：\n\n")
	fmt.Fprintf(&b, "1. **组织逻辑**：首先用一段话讲清楚项目按什么逻辑组织（分层架构？功能模块？插件式？），让读者立刻理解代码库的整体布局思路。\n")
	fmt.Fprintf(&b, "2. **逐层展开**：将模块分成 2-4 个子系统/层，每层用一段文字讲清该层的职责和设计意图。\n")
	fmt.Fprintf(&b, "3. **多图穿插**：每个子系统/层画一张 ```mermaid``` 依赖图。每张图只包含该层 5-8 个核心模块。图紧跟在对应段落后。\n")
	fmt.Fprintf(&b, "4. **图要简洁**：不要把全部模块塞进一张图——那会导致混乱不可读。每张图只画当前讨论的子系统。\n")
	fmt.Fprintf(&b, "5. **基于代码而非猜测**：根据上面提供的类名、函数签名、依赖关系来推断模块职责，不要仅凭文件名猜测。\n")
	fmt.Fprintf(&b, "6. **来源标注**：每段末尾用 `*来源：[显示名](文件路径)*` 标注涉及的源文件。\n")
	fmt.Fprintf(&b, "7. **格式**：使用 ### 级别标题组织各层，每个图前有一段文字说明。全文总字数 600-1200 字。\n\n")
	fmt.Fprintf(&b, "直接输出 Markdown 正文，不要加任何 JSON 包装或代码围栏。")

	return b.String()
}

// generateModuleChineseNames uses LLM to generate short Chinese names for all modules.
// Returns a map from module full name to Chinese label, or nil on failure.
func generateModuleChineseNames(ctx context.Context, provider llm.Provider, projectName string, nodes []*grapher.Node) map[string]string {
	if len(nodes) == 0 || provider == nil {
		return nil
	}

	// Cap to 60 modules to keep the LLM prompt manageable
	const maxModules = 60
	limited := nodes
	if len(nodes) > maxModules {
		limited = nodes[:maxModules]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "你是一位软件项目文档编写者。请为下列代码模块各生成一个简短的中文名称（3-8字），让代码读者一眼看懂每个模块的用途。\n\n")
	fmt.Fprintf(&b, "项目：%s\n\n", projectName)
	fmt.Fprintf(&b, "严格按紧凑的 JSON 格式输出（不要换行、不要注释），键为模块完整路径，值为对应的中文名称：\n")
	fmt.Fprintf(&b, `{"模块路径": "中文名称", ...}`+"\n\n")

	for _, n := range limited {
		fmt.Fprintf(&b, "- %s：%s\n", n.Name, inferModuleResponsibility(n))
	}

	fmt.Fprintf(&b, "\n直接输出 JSON，不要加任何解释说明。")

	result, err := streamComplete(ctx, provider, b.String())
	if err != nil || result == "" {
		return nil
	}

	return parseChineseNamesJSON(result)
}

// parseChineseNamesJSON extracts a map[string]string from LLM JSON output.
func parseChineseNamesJSON(raw string) map[string]string {
	// Find JSON block: the first { to the last }
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < 0 || start >= end {
		return nil
	}
	jsonStr := raw[start : end+1]

	var result map[string]string
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil
	}
	return result
}
