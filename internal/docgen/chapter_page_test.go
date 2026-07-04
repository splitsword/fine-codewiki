package docgen

import (
	"strings"
	"testing"

	"github.com/splitsword/fine-codewiki/internal/analyzer"
	"github.com/splitsword/fine-codewiki/internal/grapher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeThemeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"入口与命令行", "入口与命令行"},
		{"auth/login", "auth_login"},
		{"path\\to\\thing", "path_to_thing"},
		{"user:auth", "user_auth"},
		{"hello world", "hello_world"},
		{"a/b:c d", "a_b_c_d"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, safeThemeName(tt.input))
		})
	}
}

func TestDocgenEstimateTotalMinutes(t *testing.T) {
	wiki := &Wiki{
		ModuleDocs: map[string]string{
			"mod1": strings.Repeat("a ", 500),  // ~2 min
			"mod2": strings.Repeat("b ", 250),  // ~1 min
		},
		Overview:        strings.Repeat("o ", 300), // ~1 min
		WhatItDoes:      "short",
		Architecture:    strings.Repeat("c ", 250), // ~1 min
		ProjectStructure: "short",
		LearningPath:    "short",
		APIReference:    strings.Repeat("d ", 300), // ~1 min
	}
	minutes := docgenEstimateTotalMinutes(wiki)
	assert.Greater(t, minutes, 0)
}

func TestBuildNodeListTree(t *testing.T) {
	nodes := []*grapher.Node{
		{Name: "cmd/main", Filename: "cmd/main.go"},
		{Name: "models/user", Filename: "models/user.go"},
		{Name: "utils/helper", Filename: "utils/helper.go"},
		{Name: "models/common", Filename: "models/common.go"},
	}
	tree := buildNodeListTree(nodes)
	require.NotNil(t, tree)
	assert.Equal(t, ".", tree.Name)
	assert.True(t, tree.IsDir)

	// Should have top-level dirs: cmd, models, utils
	dirNames := make(map[string]bool)
	for _, c := range tree.Children {
		dirNames[c.Name] = true
	}
	assert.True(t, dirNames["cmd"])
	assert.True(t, dirNames["models"])
	assert.True(t, dirNames["utils"])
}

func TestBuildNodeListTreeEmpty(t *testing.T) {
	tree := buildNodeListTree(nil)
	require.NotNil(t, tree)
	assert.Empty(t, tree.Children)
}

func TestBuildStaticNavSections(t *testing.T) {
	wiki := &Wiki{
		Overview:    "# overview",
		WhatItDoes:  "# what",
		ModuleThemes: map[string][]string{
			"入口与命令行":  {"cmd/main"},
			"数据模型与实体": {"models/user"},
		},
		ChapterTitles: map[string]ChapterTitle{
			"入口与命令行":  {Title: "入口与命令行", Subtitle: "程序入口", Difficulty: "⭐"},
			"数据模型与实体": {Title: "数据模型", Subtitle: "数据结构", Difficulty: "⭐⭐"},
		},
		APIReference: "# api",
	}

	sections := buildStaticNavSections(wiki, nil, "入口与命令行")

	require.Len(t, sections, 4)

	// Section 0: 认识项目
	assert.Equal(t, "认识项目", sections[0].Label)
	assert.GreaterOrEqual(t, len(sections[0].Items), 1)

	// Section 2: 深入剖析 — should have chapter links
	assert.Equal(t, "深入剖析", sections[2].Label)
	assert.Len(t, sections[2].Items, 2)
	assert.Equal(t, "入口与命令行", sections[2].Items[0].Title)
	assert.Equal(t, "数据模型", sections[2].Items[1].Title)

	// Section 3: 速查 — should have API ref
	assert.Equal(t, "速查", sections[3].Label)
	hasAPI := false
	for _, item := range sections[3].Items {
		if strings.Contains(item.Title, "API") {
			hasAPI = true
		}
	}
	assert.True(t, hasAPI)
}

func TestBuildStaticNavSectionsWithGraph(t *testing.T) {
	wiki := &Wiki{
		Overview:     "# overview",
		ModuleThemes: map[string][]string{},
	}
	files := []*analyzer.FileResult{
		{Filename: "cmd/main.go", Functions: []analyzer.FunctionInfo{{Name: "main"}}},
		{Filename: "pkg/server.go", Imports: []analyzer.ImportInfo{{Module: "cmd/main"}}},
	}
	graph := grapher.BuildGraph(files)

	sections := buildStaticNavSections(wiki, graph, "")
	require.Len(t, sections, 4)

	// Section 1: 开始阅读 — has architecture and project-structure
	assert.Equal(t, "开始阅读", sections[1].Label)
	assert.Equal(t, "🏗️", sections[1].Items[0].Icon)
	assert.Equal(t, "📁", sections[1].Items[1].Icon)
}

func TestGenerateChapterPage(t *testing.T) {
	wiki := &Wiki{
		ProjectName: "test-project",
		ChapterTitles: map[string]ChapterTitle{
			"入口与命令行": {Title: "入口与命令行", Subtitle: "程序启动与参数解析", Difficulty: "⭐"},
		},
		ModuleThemes: map[string][]string{
			"入口与命令行": {"cmd/main"},
		},
		ModuleDocs: map[string]string{
			"cmd/main": "# cmd/main\n\n这是命令行入口模块。\n",
		},
		APIReference: "# API 参考\n\nAPI 内容。\n",
	}

	html := GenerateChapterPage(wiki, "入口与命令行", nil)
	require.NotEmpty(t, html)

	// Should contain chapter title
	assert.Contains(t, html, "入口与命令行")

	// Should contain project name
	assert.Contains(t, html, "test-project")

	// Should contain difficulty
	assert.Contains(t, html, "⭐")

	// Should contain module content
	assert.Contains(t, html, "cmd/main")
	assert.Contains(t, html, "这是命令行入口模块")

	// Should contain nav sidebar (4 groups)
	assert.Contains(t, html, "认识项目")
	assert.Contains(t, html, "开始阅读")
	assert.Contains(t, html, "深入剖析")
	assert.Contains(t, html, "速查")
	// 速查 should have API link
	assert.Contains(t, html, "API")

	// Should contain chapter navigation (prev/next)
	assert.Contains(t, html, "chapter-nav")

	// Should have right sidebar
	assert.Contains(t, html, "right-sidebar")

	// Should be valid HTML structure
	assert.Contains(t, html, "<!DOCTYPE html>")
	assert.Contains(t, html, "</html>")
}

func TestGenerateChapterPageWithoutTitles(t *testing.T) {
	wiki := &Wiki{
		ProjectName: "bare-project",
		ModuleThemes: map[string][]string{
			"raw-theme": {"pkg/util"},
		},
		ModuleDocs: map[string]string{
			"pkg/util": "# util\n\n工具模块。\n",
		},
	}

	html := GenerateChapterPage(wiki, "raw-theme", nil)
	require.NotEmpty(t, html)

	// Should fall back to theme name as title
	assert.Contains(t, html, "raw-theme")
	assert.Contains(t, html, "bare-project")
}

func TestGenerateChapterPages(t *testing.T) {
	wiki := &Wiki{
		ProjectName: "multi-chapter",
		ModuleThemes: map[string][]string{
			"主题一": {"a"},
			"主题二": {"b"},
		},
		ModuleDocs: map[string]string{
			"a": "# a\n",
			"b": "# b\n",
		},
	}

	pages := GenerateChapterPages(wiki, nil)
	require.Len(t, pages, 2)
	assert.Contains(t, pages, "主题一")
	assert.Contains(t, pages, "主题二")
	assert.Contains(t, pages["主题一"], "主题一")
	assert.Contains(t, pages["主题二"], "主题二")
}

func TestBuildChapterHTMLStructure(t *testing.T) {
	wiki := &Wiki{
		ProjectName: "struct-test",
		ModuleThemes: map[string][]string{
			"test-theme": {"pkg/x"},
		},
		ModuleDocs: map[string]string{
			"pkg/x": "# x module\n",
		},
		ChapterTitles: map[string]ChapterTitle{
			"test-theme": {Title: "测试主题", Subtitle: "测试副标题", Difficulty: "⭐⭐⭐"},
		},
	}
	sections := buildStaticNavSections(wiki, nil, "test-theme")

	html := buildChapterHTML(
		wiki.ProjectName,
		"测试主题",
		"<section id='test'>content</section>",
		sections,
		10, 5,
		"<div>sidebar tree</div>",
		"test-theme",
	)
	require.NotEmpty(t, html)

	// Should contain key structural elements
	assert.Contains(t, html, "<!DOCTYPE html>")
	assert.Contains(t, html, "struct-test")
	assert.Contains(t, html, "测试主题")
	assert.Contains(t, html, "10 篇文章")
	assert.Contains(t, html, "约 5 分钟")
	assert.Contains(t, html, "sidebar tree")
	assert.Contains(t, html, "right-sidebar")
	assert.Contains(t, html, `<section id='test'>`)
	assert.Contains(t, html, "nav-group-label")
	assert.Contains(t, html, "nav-group-count")
}

func TestBuildNodeListTreeSortOrder(t *testing.T) {
	nodes := []*grapher.Node{
		{Name: "zzz_last", Filename: "a/zzz.go"},
		{Name: "aaa_first", Filename: "a/aaa.go"},
		{Name: "dir_only", Filename: "b/c/file.go"},
	}

	tree := buildNodeListTree(nodes)
	require.NotNil(t, tree)

	// Directories should come before files (IsDir first), then alphabetical
	// Both "a" and "b" are dirs, and "dir_only" is a file under "b/c/"
	// So children of root: [a, b] — both dirs, alphabetical
	require.GreaterOrEqual(t, len(tree.Children), 2)
	assert.True(t, tree.Children[0].IsDir)
	assert.True(t, tree.Children[1].IsDir)
	assert.Equal(t, "a", tree.Children[0].Name)
	assert.Equal(t, "b", tree.Children[1].Name)
}

func TestGenerateChapterPageWithNarrative(t *testing.T) {
	wiki := &Wiki{
		ProjectName: "narrative-project",
		ChapterTitles: map[string]ChapterTitle{
			"认证系统": {Title: "用户认证", Subtitle: "身份验证与会话管理", Difficulty: "⭐⭐"},
		},
		ModuleThemes: map[string][]string{
			"认证系统": {"auth/middleware", "auth/session"},
		},
		ModuleDocs: map[string]string{
			"auth/middleware": "# auth/middleware\n\n中间件模块。\n",
			"auth/session":   "# auth/session\n\n会话管理模块。\n",
		},
		ThemeIntros: map[string]string{
			"认证系统": "本章介绍认证系统的设计与实现。",
		},
		ChapterNarratives: map[string]string{
			"认证系统": "## 认证流程概览\n\n当一个请求到达系统时，首先经过认证中间件验证身份。\n\n> **设计决策**：选择 JWT 而非传统 Session，因为前者更适合无状态架构。\n\n*来源：[auth/middleware]*\n\n## 关键收获\n\n- 认证中间件是系统安全的第一道防线\n- 会话管理与身份验证解耦\n- JWT 适合无状态微服务架构",
		},
		APIReference: "# API\n",
	}

	html := GenerateChapterPage(wiki, "认证系统", nil)
	require.NotEmpty(t, html)

	// Narrative content should be present
	assert.Contains(t, html, "chapter-narrative")
	assert.Contains(t, html, "认证流程概览")
	assert.Contains(t, html, "设计决策")

	// Module docs should be in collapsible details
	assert.Contains(t, html, "module-reference")
	assert.Contains(t, html, "module-detail")
	assert.Contains(t, html, "<details")
	assert.Contains(t, html, "<summary>middleware</summary>")
	assert.Contains(t, html, "<summary>session</summary>")

	// Module content should still be rendered inside details
	assert.Contains(t, html, "中间件模块")
	assert.Contains(t, html, "会话管理模块")
}

func TestGenerateChapterPageNarrativeFallback(t *testing.T) {
	wiki := &Wiki{
		ProjectName: "fallback-project",
		ModuleThemes: map[string][]string{
			"工具库": {"pkg/util"},
		},
		ModuleDocs: map[string]string{
			"pkg/util": "# util\n\n工具模块内容。\n",
		},
		// No ChapterNarratives — should fall back to direct module rendering
	}

	html := GenerateChapterPage(wiki, "工具库", nil)
	require.NotEmpty(t, html)

	// Should NOT have narrative or collapsible wrappers in the body (CSS declarations are OK)
	assert.NotContains(t, html, `<div class="chapter-narrative">`)
	assert.NotContains(t, html, `<div class="module-reference">`)
	assert.NotContains(t, html, `<details class="module-detail"`)


	// Module content should be rendered directly in sections
	assert.Contains(t, html, `<section id="module-`)
	assert.Contains(t, html, "工具模块内容")
}

func TestGenerateChapterPageNarrativeWithEmptyModuleDocs(t *testing.T) {
	wiki := &Wiki{
		ProjectName: "narrative-only",
		ModuleThemes: map[string][]string{
			"核心逻辑": {"core/engine", "core/pipeline"},
		},
		ModuleDocs: map[string]string{
			"core/engine": "# engine\n\n引擎模块。\n",
			// core/pipeline has no doc
		},
		ChapterNarratives: map[string]string{
			"核心逻辑": "## 核心引擎\n\n引擎和管线协同工作。\n\n## 关键收获\n\n- 模块化设计",
		},
	}

	html := GenerateChapterPage(wiki, "核心逻辑", nil)
	require.NotEmpty(t, html)

	// Should have narrative
	assert.Contains(t, html, "chapter-narrative")
	assert.Contains(t, html, "核心引擎")

	// Should still show the one module doc that exists
	assert.Contains(t, html, "module-detail")
	assert.Contains(t, html, "引擎模块")

	// Should NOT have a details for the missing module
	assert.NotContains(t, html, "pipeline</summary>")
}

func TestBuildStaticNavSectionsWithSubItems(t *testing.T) {
	t.Run("without narrative fallback to modules", func(t *testing.T) {
		wiki := &Wiki{
			Overview: "# overview",
			ModuleThemes: map[string][]string{
				"认证系统": {"auth/middleware", "auth/session"},
				"数据层":  {"db/postgres"},
			},
			ChapterTitles: map[string]ChapterTitle{
				"认证系统": {Title: "用户认证", Subtitle: "身份验证", Difficulty: "⭐⭐"},
				"数据层":  {Title: "数据层", Subtitle: "持久化", Difficulty: "⭐"},
			},
		}

		sections := buildStaticNavSections(wiki, nil, "认证系统")
		require.Len(t, sections[2].Items, 2)

		var authItem NavItem
		for _, item := range sections[2].Items {
			if item.Title == "用户认证" {
				authItem = item
				break
			}
		}
		require.Len(t, authItem.SubItems, 2)
		assert.Equal(t, "middleware", authItem.SubItems[0].Title)
		assert.Equal(t, "session", authItem.SubItems[1].Title)
		assert.Contains(t, authItem.SubItems[0].URL, "#module-")

		var dbItem NavItem
		for _, item := range sections[2].Items {
			if item.Title == "数据层" {
				dbItem = item
				break
			}
		}
		assert.Empty(t, dbItem.SubItems)
	})

	t.Run("with narrative uses h2 headings", func(t *testing.T) {
		wiki := &Wiki{
			Overview: "# overview",
			ModuleThemes: map[string][]string{
				"认证系统": {"auth/middleware", "auth/session"},
				"数据层":  {"db/postgres"},
			},
			ChapterTitles: map[string]ChapterTitle{
				"认证系统": {Title: "用户认证", Subtitle: "身份验证", Difficulty: "⭐⭐"},
				"数据层":  {Title: "数据层", Subtitle: "持久化", Difficulty: "⭐"},
			},
			ChapterNarratives: map[string]string{
				"认证系统": "## 认证流程概览\n\n描述认证流程。\n\n## 关键收获\n\n总结要点。",
			},
		}

		sections := buildStaticNavSections(wiki, nil, "认证系统")
		require.Len(t, sections[2].Items, 2)

		var authItem NavItem
		for _, item := range sections[2].Items {
			if item.Title == "用户认证" {
				authItem = item
				break
			}
		}
		require.Len(t, authItem.SubItems, 2)
		assert.Equal(t, "认证流程概览", authItem.SubItems[0].Title)
		assert.Equal(t, "关键收获", authItem.SubItems[1].Title)
		assert.Equal(t, "📑", authItem.SubItems[0].Icon)
		assert.Contains(t, authItem.SubItems[0].URL, "#")
		assert.NotContains(t, authItem.SubItems[0].URL, "#module-")
	})
}

func TestGenerateChapterPageWithGoals(t *testing.T) {
	wiki := &Wiki{
		ProjectName: "goals-project",
		ChapterTitles: map[string]ChapterTitle{
			"认证系统": {
				Title:         "用户认证",
				Subtitle:      "身份验证与会话管理",
				Difficulty:    "⭐⭐",
				LearningGoals: []string{"理解认证中间件的工作原理", "掌握 JWT 会话管理"},
				Prerequisites: []string{"了解 HTTP 协议基础"},
			},
		},
		ModuleThemes: map[string][]string{
			"认证系统": {"auth/login"},
		},
		ModuleDocs: map[string]string{
			"auth/login": "# login\n\n登录模块。\n",
		},
	}

	html := GenerateChapterPage(wiki, "认证系统", nil)
	require.NotEmpty(t, html)

	// Should contain learning goals
	assert.Contains(t, html, "学习目标")
	assert.Contains(t, html, "理解认证中间件的工作原理")
	assert.Contains(t, html, "掌握 JWT 会话管理")

	// Should contain prerequisites
	assert.Contains(t, html, "前置知识")
	assert.Contains(t, html, "了解 HTTP 协议基础")
}

func TestGenerateChapterPageTOC(t *testing.T) {
	wiki := &Wiki{
		ProjectName: "toc-project",
		ModuleThemes: map[string][]string{
			"核心": {"core/engine"},
		},
		ChapterNarratives: map[string]string{
			"核心": "# 引擎\n\n## 架构总览\n\n叙事。\n\n## 核心流程\n\n流程。\n",
		},
		ModuleDocs: map[string]string{
			"core/engine": "# 引擎核心\n\n## 初始化流程\n\n初始化说明。\n\n## 配置管理\n\n配置说明。\n\n### 环境变量\n\n环境变量说明。\n",
		},
	}

	html := GenerateChapterPage(wiki, "核心", nil)
	require.NotEmpty(t, html)

	// Should contain TOC section
	assert.Contains(t, html, "本章目录")
	assert.Contains(t, html, "toc-list")
	assert.Contains(t, html, "toc-item")
	// Narrative headings enter the TOC
	assert.Contains(t, html, ">架构总览</a>")
	// Module-detail internal headings are demoted, not in TOC
	assert.NotContains(t, html, ">初始化流程</a>")
}

func TestExtractHeadings(t *testing.T) {
	html := `<h2 id="section-1">第一节</h2><p>内容</p><h3 id="sub-1">子节点</h3><h2 id="section-2">第二节</h2>`

	headings := extractHeadings(html)
	require.Len(t, headings, 3)

	assert.Equal(t, 2, headings[0].level)
	assert.Equal(t, "section-1", headings[0].id)
	assert.Equal(t, "第一节", headings[0].text)

	assert.Equal(t, 3, headings[1].level)
	assert.Equal(t, "sub-1", headings[1].id)
	assert.Equal(t, "子节点", headings[1].text)

	assert.Equal(t, 2, headings[2].level)
	assert.Equal(t, "section-2", headings[2].id)
	assert.Equal(t, "第二节", headings[2].text)
}

func TestExtractMarkdownH2s(t *testing.T) {
	md := "## 认证流程概览\n\n正文段落。\n\n## 关键收获\n\n- 要点一\n\n### 三级标题\n\n不应包含"
	titles := extractMarkdownH2s(md)
	require.Len(t, titles, 2)
	assert.Equal(t, "认证流程概览", titles[0])
	assert.Equal(t, "关键收获", titles[1])

	assert.Empty(t, extractMarkdownH2s("no headings here"))
	assert.Empty(t, extractMarkdownH2s(""))
}

func TestExtractHeadingsEmpty(t *testing.T) {
	headings := extractHeadings("<p>no headings here</p>")
	assert.Empty(t, headings)
}

func TestExtractHeadingsStripsInnerHTML(t *testing.T) {
	html := `<h2 id="test"><a href="#">链接标题</a></h2>`
	headings := extractHeadings(html)
	require.Len(t, headings, 1)
	assert.Equal(t, "链接标题", headings[0].text)
}

// TestDowngradeModuleHeadings verifies module-detail internal headings are
// demoted out of the h2/h3 range so they don't enter the chapter TOC.
func TestDowngradeModuleHeadings(t *testing.T) {
	in := `<h2 id="职责">职责</h2><p>x</p><h3 id="关键设计">关键设计</h3>`
	out := downgradeModuleHeadings(in)
	assert.Contains(t, out, "<h4 id=\"职责\">职责</h4>")
	assert.Contains(t, out, "<h5 id=\"关键设计\">关键设计</h5>")
	assert.NotContains(t, out, "<h2")
	assert.NotContains(t, out, "<h3")
	// headingRe only matches h2/h3 → demoted headings won't be picked up.
	assert.Empty(t, extractHeadings(out))
}
