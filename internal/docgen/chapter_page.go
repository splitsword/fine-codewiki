package docgen

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/splitsword/fine-codewiki/internal/diagram"
	"github.com/splitsword/fine-codewiki/internal/grapher"
	"github.com/splitsword/fine-codewiki/internal/sequencer"
)

type heading struct {
	level int
	id    string
	text  string
}

var headingRe = regexp.MustCompile(`<h([23])\s+id="([^"]+)"[^>]*>(.*?)</h[23]>`)

func extractHeadings(html string) []heading {
	matches := headingRe.FindAllStringSubmatch(html, -1)
	var result []heading
	for _, m := range matches {
		lvl := 2
		if m[1] == "3" {
			lvl = 3
		}
		text := regexp.MustCompile(`<[^>]+>`).ReplaceAllString(m[3], "")
		result = append(result, heading{level: lvl, id: m[2], text: strings.TrimSpace(text)})
	}
	return result
}

// GenerateChapterPage builds a standalone HTML page for one theme chapter,
// with contextual right sidebar showing only the related source files.
func GenerateChapterPage(wiki *Wiki, theme string, graph *grapher.Graph) string {
	ct, hasTitle := wiki.ChapterTitles[theme]
	modNames := wiki.ModuleThemes[theme]

	// Build contextual file tree: only nodes belonging to this theme's modules
	var themeNodes []*grapher.Node
	if graph != nil {
		modSet := make(map[string]bool)
		for _, m := range modNames {
			modSet[m] = true
		}
		for _, n := range graph.Nodes {
			if modSet[n.Name] {
				themeNodes = append(themeNodes, n)
			}
		}
	}
	var rightSidebar string
	if len(themeNodes) > 0 {
		tree := buildNodeListTree(themeNodes)
		rightSidebar = RenderFileTreeHTML(tree)
	} else {
		rightSidebar = `<div style="padding:16px;color:var(--text3);font-size:12px">暂无相关代码文件</div>`
	}

	// Build navigation sections
	sections := buildStaticNavSections(wiki, graph, theme)

	// Count articles and minutes for sidebar summary
	totalArts := 5 // 00-05 narrative docs
	if wiki.KeyConcepts != "" {
		totalArts++
	}
	totalArts += len(wiki.ModuleDocs) + 1 // +1 for api-reference
	totalMins := docgenEstimateTotalMinutes(wiki) // approximate

	// Build body content
	var body strings.Builder

	// Chapter title header
	chapterTitle := theme
	chapterSubtitle := ""
	chapterDiff := ""
	if hasTitle {
		chapterTitle = ct.Title
		chapterSubtitle = ct.Subtitle
		chapterDiff = ct.Difficulty
	}
	body.WriteString(`<div class="chapter-header">`)
	body.WriteString(fmt.Sprintf(`<h1>%s</h1>`, chapterTitle))
	if chapterSubtitle != "" {
		body.WriteString(fmt.Sprintf(`<p class="chapter-subtitle">%s</p>`, chapterSubtitle))
	}
	body.WriteString(`<div class="chapter-meta">`)
	if chapterDiff != "" {
		body.WriteString(fmt.Sprintf(`<span class="chapter-diff">%s</span>`, chapterDiff))
	}
	body.WriteString(fmt.Sprintf(`<span class="chapter-count">%d 个模块</span>`, len(modNames)))
	body.WriteString(`</div>`)
	if hasTitle && len(ct.LearningGoals) > 0 {
		body.WriteString(`<div class="chapter-goals"><div class="goals-label">🎯 学习目标</div><ul>`)
		for _, g := range ct.LearningGoals {
			body.WriteString(fmt.Sprintf(`<li>%s</li>`, HTMLEscape(g)))
		}
		body.WriteString(`</ul></div>`)
	}
	if hasTitle && len(ct.Prerequisites) > 0 {
		body.WriteString(`<div class="chapter-prereqs"><div class="prereqs-label">📋 前置知识</div><ul>`)
		for _, p := range ct.Prerequisites {
			body.WriteString(fmt.Sprintf(`<li>%s</li>`, HTMLEscape(p)))
		}
		body.WriteString(`</ul></div>`)
	}
	body.WriteString(`</div>`)

	// Theme-level intro
	if intro, ok := wiki.ThemeIntros[theme]; ok && intro != "" {
		body.WriteString(fmt.Sprintf(`<div class="chapter-intro"><p>%s</p></div>`, intro))
	}

	// Chapter narrative — LLM-generated cross-module educational article
	narrative := ""
	if wiki.ChapterNarratives != nil {
		narrative = wiki.ChapterNarratives[theme]
	}
	primaryExt := primaryExtForLang(wiki.Language)
	if narrative != "" {
		body.WriteString(`<div class="chapter-narrative">`)
		body.WriteString(makeSourceRefsClickable(RenderMarkdownBody([]byte(narrative)), primaryExt))
		body.WriteString("</div>\n")
	}

	// ─── Per-chapter architecture diagram ───
	if graph != nil && len(modNames) > 0 {
		subGraph := graph.SubGraphForNodes(modNames)
		if len(subGraph.Nodes) > 0 && len(subGraph.Edges) > 0 {
			subDSL, _ := diagram.GenerateSubArchDiagram(subGraph, theme)
			if subDSL != "" {
				body.WriteString("<h2>模块架构图</h2>\n")
				body.WriteString("<p>本章涉及的模块及其内部依赖关系：</p>\n")
				body.WriteString("<pre class=\"mermaid\">\n")
				body.WriteString(subDSL)
				body.WriteString("</pre>\n\n")
			}
		}
	}

	// ─── Per-chapter sequence diagram ───
	if len(wiki.Sequences) > 0 && len(modNames) > 0 {
		modSet := make(map[string]bool, len(modNames))
		for _, m := range modNames {
			modSet[m] = true
		}
		var themeSeqs []sequencer.Sequence
		for _, seq := range wiki.Sequences {
			for _, p := range seq.Participants {
				if modSet[p] {
					themeSeqs = append(themeSeqs, seq)
					break
				}
			}
		}
		for i := 0; i < len(themeSeqs) && i < 2; i++ {
			seqDSL := sequencer.GenerateSequenceDiagram(themeSeqs[i])
			if seqDSL != "" {
				if themeSeqs[i].Description != "" {
					body.WriteString(fmt.Sprintf("<p><strong>调用流程：</strong>%s</p>\n", themeSeqs[i].Description))
				}
				body.WriteString("<pre class=\"mermaid\">\n")
				body.WriteString(seqDSL)
				body.WriteString("</pre>\n\n")
			}
		}
	}

	// Module docs — shown as collapsible reference when narrative exists,
	// or as main content when no narrative is available.
	hasModDocs := false
	for _, modName := range modNames {
		if _, ok := wiki.ModuleDocs[modName]; ok {
			hasModDocs = true
			break
		}
	}
	if hasModDocs {
		if narrative != "" {
			body.WriteString(`<div class="module-reference">`)
			body.WriteString(`<div class="module-reference-header">📦 模块详情参考</div>`)
		}
		for _, modName := range modNames {
			doc, ok := wiki.ModuleDocs[modName]
			if !ok {
				continue
			}
			secID := "module-" + mermaidEscape(modName)
			if narrative != "" {
				body.WriteString(fmt.Sprintf(`<details class="module-detail" id="%s">`, secID))
				baseName := filepath.Base(modName)
				summaryLabel := baseName
				if cn, ok := wiki.ModuleChineseNames[modName]; ok && cn != "" {
					summaryLabel = fmt.Sprintf("%s（%s）", cn, baseName)
				}
				body.WriteString(fmt.Sprintf(`<summary>%s</summary>`, summaryLabel))
				body.WriteString(`<div class="module-body">`)
				body.WriteString(makeSourceRefsClickable(RenderMarkdownBody([]byte(doc)), primaryExt))
				body.WriteString("</div></details>\n")
			} else {
				body.WriteString(fmt.Sprintf(`<section id="%s">`, secID))
				body.WriteString(makeSourceRefsClickable(RenderMarkdownBody([]byte(doc)), primaryExt))
				body.WriteString("</section>\n")
			}
		}
		if narrative != "" {
			body.WriteString("</div>\n")
		}
	}

	// Chapter navigation — prev/next
	sortedThemes := sortedThemeKeys(wiki.ModuleThemes)
	currentIdx := -1
	for i, t := range sortedThemes {
		if t == theme {
			currentIdx = i
			break
		}
	}
	if currentIdx >= 0 {
		body.WriteString(`<nav class="chapter-nav">`)
		if currentIdx > 0 {
			prevTheme := sortedThemes[currentIdx-1]
			prevTitle := prevTheme
			if ct, ok := wiki.ChapterTitles[prevTheme]; ok {
				prevTitle = ct.Title
			}
			body.WriteString(fmt.Sprintf(`<a class="chapter-nav-prev" href="%s.html">← %s</a>`, safeThemeName(prevTheme), prevTitle))
		} else {
			body.WriteString(`<a class="chapter-nav-prev" href="../index.html">← 返回首页</a>`)
		}
		if currentIdx < len(sortedThemes)-1 {
			nextTheme := sortedThemes[currentIdx+1]
			nextTitle := nextTheme
			if ct, ok := wiki.ChapterTitles[nextTheme]; ok {
				nextTitle = ct.Title
			}
			body.WriteString(fmt.Sprintf(`<a class="chapter-nav-next" href="%s.html">%s →</a>`, safeThemeName(nextTheme), nextTitle))
		} else {
			body.WriteString(`<a class="chapter-nav-next" href="../index.html">返回首页 →</a>`)
		}
		body.WriteString(`</nav>`)
	}

	return buildChapterHTML(wiki.ProjectName, chapterTitle, body.String(), sections, totalArts, totalMins, rightSidebar, theme)
}

// GenerateChapterPages builds standalone HTML pages for all themes.
func GenerateChapterPages(wiki *Wiki, graph *grapher.Graph) map[string]string {
	pages := make(map[string]string)
	for _, theme := range sortedThemeKeys(wiki.ModuleThemes) {
		pages[theme] = GenerateChapterPage(wiki, theme, graph)
	}
	return pages
}

// buildNodeListTree builds a simplified file tree from a flat list of nodes.
func buildNodeListTree(nodes []*grapher.Node) *FileTreeNode {
	root := &FileTreeNode{Name: ".", Path: "", IsDir: true}
	dirSet := make(map[string]bool)
	nodeByPath := make(map[string]*FileTreeNode)
	nodeByPath[""] = root

	for _, n := range nodes {
		fn := strings.ReplaceAll(n.Filename, "\\", "/")
		parts := strings.Split(fn, "/")
		// Create directory hierarchy
		for i := 0; i < len(parts)-1; i++ {
			dirPath := strings.Join(parts[:i+1], "/")
			if dirSet[dirPath] {
				continue
			}
			dirSet[dirPath] = true
			parent := ""
			if idx := strings.LastIndex(dirPath, "/"); idx >= 0 {
				parent = dirPath[:idx]
			}
			name := dirPath
			if idx := strings.LastIndex(dirPath, "/"); idx >= 0 {
				name = dirPath[idx+1:]
			}
			child := &FileTreeNode{Name: name, Path: dirPath, IsDir: true}
			if p, ok := nodeByPath[parent]; ok {
				p.Children = append(p.Children, child)
			}
			nodeByPath[dirPath] = child
		}
	}

	for _, n := range nodes {
		fn := strings.ReplaceAll(n.Filename, "\\", "/")
		parent := ""
		if idx := strings.LastIndex(fn, "/"); idx >= 0 {
			parent = fn[:idx]
		}
		name := fn
		if idx := strings.LastIndex(fn, "/"); idx >= 0 {
			name = fn[idx+1:]
		}
		safe := mermaidEscape(n.Name)
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

// buildStaticNavSections builds the 4-section nav for static HTML pages,
// with chapter titles used in the 深入剖析 section.
func buildStaticNavSections(wiki *Wiki, graph *grapher.Graph, currentTheme string) []NavSection {
	sections := []NavSection{
		{Label: "认识项目", Icon: "📘"},
		{Label: "开始阅读", Icon: "📗"},
		{Label: "深入剖析", Icon: "📕"},
		{Label: "速查", Icon: "📓"},
	}

	// 认识项目
	sections[0].Items = append(sections[0].Items, NavItem{Icon: "📊", Title: "项目概述", URL: "../index.html"})
	if wiki.WhatItDoes != "" {
		sections[0].Items = append(sections[0].Items, NavItem{Icon: "🎯", Title: "项目能做什么", URL: "../index.html#what-it-does"})
	}

	// 开始阅读
	sections[1].Items = append(sections[1].Items, NavItem{Icon: "🏗️", Title: "架构说明", URL: "../index.html#architecture"})
	sections[1].Items = append(sections[1].Items, NavItem{Icon: "📁", Title: "项目结构", URL: "../index.html#project-structure"})

	// 深入剖析 — use chapter titles, with module sub-items for current theme
	for _, theme := range sortedThemeKeys(wiki.ModuleThemes) {
		safeName := safeThemeName(theme)
		ct, ok := wiki.ChapterTitles[theme]
		title := theme
		diff := ""
		if ok {
			title = ct.Title
			diff = ct.Difficulty
		}
		icon := "📦"
		if ok {
			icon = "🏷️"
		}
		item := NavItem{
			Icon:       icon,
			Title:      title,
			URL:        safeName + ".html",
			Difficulty: diff,
		}
		if theme == currentTheme {
			narrative := ""
			if wiki.ChapterNarratives != nil {
				narrative = wiki.ChapterNarratives[theme]
			}
			if narrative != "" {
				for _, h2 := range extractMarkdownH2s(narrative) {
					item.SubItems = append(item.SubItems, NavItem{
						Icon:  "📑",
						Title: h2,
						URL:   "#" + headingSlug(h2),
					})
				}
			} else {
				for _, modName := range wiki.ModuleThemes[theme] {
					item.SubItems = append(item.SubItems, NavItem{
						Icon:  "📄",
						Title: filepath.Base(modName),
						URL:   "#module-" + mermaidEscape(modName),
					})
				}
			}
		}
		sections[2].Items = append(sections[2].Items, item)
	}

	// 速查
	if wiki.APIReference != "" {
		sections[3].Items = append(sections[3].Items, NavItem{Icon: "📋", Title: "API 参考", URL: "../index.html#api-reference"})
	}

	return sections
}

var markdownH2Re = regexp.MustCompile(`(?m)^## (.+)$`)

func extractMarkdownH2s(markdown string) []string {
	matches := markdownH2Re.FindAllStringSubmatch(markdown, -1)
	var titles []string
	for _, m := range matches {
		titles = append(titles, strings.TrimSpace(m[1]))
	}
	return titles
}

// safeThemeName converts a theme name to a safe filename.
func safeThemeName(theme string) string {
	s := strings.ReplaceAll(theme, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	s = strings.ReplaceAll(s, ":", "_")
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

// docgenEstimateTotalMinutes provides a rough total reading time estimate.
func docgenEstimateTotalMinutes(wiki *Wiki) int {
	total := 0
	for _, doc := range wiki.ModuleDocs {
		total += EstimateReadingTime(doc)
	}
	// Narrative articles
	narratives := []string{wiki.Overview, wiki.WhatItDoes, wiki.Architecture, wiki.ProjectStructure, wiki.LearningPath, wiki.APIReference}
	for _, n := range narratives {
		total += EstimateReadingTime(n)
	}
	if total < 1 {
		total = 1
	}
	return total
}

// buildChapterHTML assembles a complete HTML page for a chapter, with sidebar
// navigation, content area, and contextual right sidebar.
func buildChapterHTML(projectName, pageTitle, body string, sections []NavSection, totalArticles, totalMinutes int, rightSidebarHTML, currentTheme string) string {
	var out strings.Builder
	out.WriteString(`<!DOCTYPE html>
<html lang="zh-CN" data-theme="light">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>`)
	out.WriteString(HTMLEscape(pageTitle))
	out.WriteString(" — ")
	out.WriteString(HTMLEscape(projectName))
	out.WriteString(` Wiki</title>
<style>`)
	out.WriteString(wikiPageCSS)
	out.WriteString(`
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
/* ---- Chapter nav ---- */
.chapter-nav { display:flex; justify-content:space-between; align-items:center; margin-top:40px; padding-top:20px; border-top:1px solid var(--border); gap:16px; }
.chapter-nav a { display:inline-flex; align-items:center; gap:6px; padding:10px 18px; border-radius:var(--radius); border:1px solid var(--border); color:var(--accent); text-decoration:none; font-size:14px; font-weight:600; transition:all .15s; }
.chapter-nav a:hover { background:var(--accent-glow); border-color:var(--accent); }
.chapter-nav-prev { margin-right:auto; }
.chapter-nav-next { margin-left:auto; }
/* ---- Narrative + module reference ---- */
.chapter-narrative { margin-bottom:32px; line-height:1.8; }
.chapter-narrative blockquote { border-left:4px solid var(--accent); background:var(--accent-glow); padding:12px 16px; margin:16px 0; border-radius:0 var(--radius) var(--radius) 0; font-style:italic; }
.module-reference { margin-top:32px; border-top:1px solid var(--border); padding-top:24px; }
.module-reference-header { font-size:18px; font-weight:700; margin-bottom:16px; color:var(--text2); }
.module-detail { margin-bottom:8px; border:1px solid var(--border2); border-radius:var(--radius); overflow:hidden; transition:border-color .15s; }
.module-detail:hover { border-color:var(--accent); }
.module-detail summary { padding:12px 16px; cursor:pointer; font-weight:600; font-size:14px; color:var(--text); user-select:none; list-style:none; display:flex; align-items:center; gap:8px; transition:background .15s; }
.module-detail summary::-webkit-details-marker { display:none; }
.module-detail summary::before { content:'▶'; font-size:10px; color:var(--text3); transition:transform .2s; }
.module-detail[open] summary::before { transform:rotate(90deg); }
.module-detail[open] summary { border-bottom:1px solid var(--border2); background:var(--accent-glow); }
.module-detail .module-body { padding:16px; }
/* ---- Sidebar sub-items ---- */
.nav-sub-items { list-style:none; padding:0; margin:2px 0 4px 0; }
.nav-sub-items li a { padding:4px 16px 4px 44px; font-size:12px; color:var(--text3); display:flex; align-items:center; gap:4px; text-decoration:none; transition:all .15s; }
.nav-sub-items li a:hover { color:var(--accent); background:var(--accent-glow); }
/* ---- Learning goals / prerequisites ---- */
.chapter-goals,.chapter-prereqs { margin-top:12px; padding:10px 16px; background:var(--bg2); border-radius:var(--radius); border:1px solid var(--border2); }
.goals-label,.prereqs-label { font-size:13px; font-weight:700; color:var(--text2); margin-bottom:4px; }
.chapter-goals ul,.chapter-prereqs ul { margin:0; padding-left:20px; }
.chapter-goals li,.chapter-prereqs li { font-size:13px; color:var(--text2); line-height:1.6; }
/* ---- TOC in right sidebar ---- */
.toc-header::before { background:#6366f1 !important; }
.toc-list { padding:8px 0; }
.toc-item { display:block; padding:4px 18px; font-size:12px; color:var(--text3); text-decoration:none; border-left:2px solid transparent; transition:all .15s; }
.toc-item:hover { color:var(--accent); background:var(--accent-glow); }
.toc-item.active { color:var(--accent); border-left-color:var(--accent); font-weight:600; }
.toc-item.toc-h3 { padding-left:30px; font-size:11px; }
</style>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/gh/highlightjs/cdn-release@11.9.0/build/styles/github-dark.min.css">
<script src="https://cdn.jsdelivr.net/gh/highlightjs/cdn-release@11.9.0/build/highlight.min.js"></script>
<script src="https://cdn.jsdelivr.net/npm/svg-pan-zoom@3.6.1/dist/svg-pan-zoom.min.js"></script>
<script type="module">
  import mermaid from 'https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.esm.min.mjs';
  mermaid.initialize({ startOnLoad:true, securityLevel:'loose', theme:'neutral' });
</script>
`)
	out.WriteString(wikiPageJS)
	out.WriteString(`</head>
<body>
<div id="reading-progress"></div>
`)

	// Top bar with search trigger
	out.WriteString(`<div class="topbar" style="left:var(--sidebar-w);right:280px">
<div class="topbar-title">`)
	out.WriteString(HTMLEscape(pageTitle))
	out.WriteString(`</div>
<div class="topbar-search">
<svg class="search-icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.35-4.35"/></svg>
<input type="text" id="topbar-search-trigger" placeholder="搜索文章、模块..." readonly>
<kbd>Ctrl+K</kbd>
</div>
<div class="topbar-actions">
<a href="../index.html" class="topbar-btn" style="display:inline-flex;align-items:center;gap:6px">🏠 首页</a>
<button id="theme-toggle" class="theme-toggle" title="切换主题"></button>
</div>
</div>
`)

	// Sidebar
	out.WriteString(`<nav class="sidebar">
<div class="sidebar-header"><span class="logo-dot"></span><a href="../index.html" style="color:inherit;text-decoration:none;font-weight:700;">`)
	out.WriteString(HTMLEscape(projectName))
	out.WriteString(`</a></div>
`)
	if totalArticles > 0 {
		out.WriteString(fmt.Sprintf(`<div class="sidebar-summary"><span>%d 篇文章</span><span class="dot">·</span><span>约 %d 分钟</span></div>
`, totalArticles, totalMinutes))
	}

	for _, sec := range sections {
		if len(sec.Items) == 0 {
			continue
		}
		out.WriteString(fmt.Sprintf(`<div class="nav-group">
<div class="nav-group-header"><span class="nav-group-label">%s %s</span><span class="nav-group-count">%d</span><span class="chevron">&#9660;</span></div>
<ul class="nav-group-items">
`, sec.Icon, sec.Label, len(sec.Items)))
		for _, item := range sec.Items {
			activeClass := ""
			if currentTheme != "" && strings.Contains(item.URL, safeThemeName(currentTheme)) {
				activeClass = ` class="active"`
			}
			out.WriteString(fmt.Sprintf(`<li><a href="%s"%s><span class="nav-icon">%s</span><span class="nav-title">%s</span>`, item.URL, activeClass, item.Icon, HTMLEscape(item.Title)))
			if item.Difficulty != "" {
				out.WriteString(fmt.Sprintf(`<span class="nav-meta"><span class="nav-diff">%s</span></span>`, item.Difficulty))
			}
			out.WriteString("</a>")
			if len(item.SubItems) > 0 {
				out.WriteString(`<ul class="nav-sub-items">`)
				for _, sub := range item.SubItems {
					out.WriteString(fmt.Sprintf(`<li><a href="%s"><span class="nav-icon">%s</span>%s</a></li>`, sub.URL, sub.Icon, HTMLEscape(sub.Title)))
				}
				out.WriteString("</ul>")
			}
			out.WriteString("</li>\n")
		}
		out.WriteString("</ul>\n</div>\n")
	}
	out.WriteString("</nav>\n")

	// Content
	out.WriteString(`<div class="content">
`)
	out.WriteString(body)
	out.WriteString(`</div>
`)

	// Right sidebar (contextual)
	out.WriteString(`<aside class="right-sidebar">
`)
	headings := extractHeadings(body)
	if len(headings) > 0 {
		out.WriteString(`<div class="right-sidebar-header toc-header">📑 本章目录</div>
<div class="toc-list">
`)
		for _, h := range headings {
			cls := "toc-item"
			if h.level == 3 {
				cls += " toc-h3"
			}
			out.WriteString(fmt.Sprintf(`<a class="%s" href="#%s">%s</a>`, cls, h.id, HTMLEscape(h.text)))
			out.WriteString("\n")
		}
		out.WriteString("</div>\n")
	}
	out.WriteString(`<div class="right-sidebar-header">相关源码</div>
<div class="file-tree">
`)
	out.WriteString(rightSidebarHTML)
	out.WriteString(`</div>
</aside>

<!-- Search overlay -->
<div class="search-overlay" onclick="if(event.target===this)this.classList.remove('active')">
<div class="search-modal">
<input type="text" id="static-search-input" placeholder="搜索文章、模块..." oninput="filterStaticSearch(this.value)">
<div class="search-results" id="static-search-results"></div>
</div>
</div>

`)
	out.WriteString(`
<script>
/* ---- Ctrl+K & search overlay ---- */
document.addEventListener('keydown',function(e){
  if((e.ctrlKey||e.metaKey)&&e.key==='k'){
    e.preventDefault();
    var ov=document.querySelector('.search-overlay');
    if(ov){ov.classList.toggle('active');if(ov.classList.contains('active'))ov.querySelector('input').focus();}
  }
  if(e.key==='Escape'){
    var ov=document.querySelector('.search-overlay');
    if(ov)ov.classList.remove('active');
  }
});
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
var observer = new IntersectionObserver(function(entries) {
  entries.forEach(function(entry) {
    if (!entry.isIntersecting) return;
    var id = entry.target.id;
    var ftLink = document.querySelector('.file-tree a[data-target="' + id + '"]');
    if (ftLink) {
      document.querySelectorAll('.file-tree a.active').forEach(function(el){el.classList.remove('active');});
      ftLink.classList.add('active');
    }
    var navLink = document.querySelector('.nav-group-items a[href*="' + id + '"]');
    if (navLink) {
      document.querySelectorAll('.nav-group-items a.active').forEach(function(el){el.classList.remove('active');});
      navLink.classList.add('active');
    }
    var tocLink = document.querySelector('.toc-item[href="#' + id + '"]');
    if (tocLink) {
      document.querySelectorAll('.toc-item.active').forEach(function(el){el.classList.remove('active');});
      tocLink.classList.add('active');
    }
  });
}, { rootMargin: '-' + (52+20) + 'px 0px -70% 0px' });
document.querySelectorAll('section[id]').forEach(function(section) { observer.observe(section); });
document.querySelectorAll('h2[id],h3[id]').forEach(function(h) { observer.observe(h); });
</script>
`)
	out.WriteString(SourcePopupJS)
	out.WriteString(`
</body>
</html>
`)
	return out.String()
}
