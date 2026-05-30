package docgen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/splitsword/fine-codewiki/internal/grapher"
)

// GeneratePDFViaChrome generates a high-quality PDF by rendering a print-optimized
// HTML through Chrome/Chromium headless. Falls back to the legacy gopdf renderer
// if no Chrome executable is found.
func GeneratePDFViaChrome(wiki *Wiki, graph *grapher.Graph, outputPath string) error {
	chrome, err := findChrome()
	if err != nil {
		return fmt.Errorf("chrome not found: %w", err)
	}

	html, err := buildPrintHTML(wiki, graph)
	if err != nil {
		return fmt.Errorf("build print html: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "codewiki-pdf-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	htmlPath := filepath.Join(tmpDir, "print.html")
	if err := os.WriteFile(htmlPath, []byte(html), 0644); err != nil {
		return fmt.Errorf("write print html: %w", err)
	}

	pdfTmp := filepath.Join(tmpDir, "output.pdf")
	args := []string{
		"--headless=new",
		"--disable-gpu",
		"--no-sandbox",
		"--disable-setuid-sandbox",
		"--disable-dev-shm-usage",
		"--disable-background-timer-throttling",
		"--disable-backgrounding-occluded-windows",
		"--disable-renderer-backgrounding",
		"--run-all-compositor-stages-before-draw",
		"--virtual-time-budget=10000",
		"--no-pdf-header-footer",
		"--print-to-pdf=" + pdfTmp,
		"file:///" + filepath.ToSlash(htmlPath),
	}

	cmd := exec.Command(chrome, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("chrome print-to-pdf failed: %w\noutput: %s", err, string(out))
	}

	if _, err := os.Stat(pdfTmp); os.IsNotExist(err) {
		return fmt.Errorf("chrome did not produce output.pdf")
	}

	data, err := os.ReadFile(pdfTmp)
	if err != nil {
		return fmt.Errorf("read generated pdf: %w", err)
	}

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("write final pdf: %w", err)
	}

	return nil
}

// HasChrome returns whether a Chrome/Chromium executable is available.
func HasChrome() bool {
	_, err := findChrome()
	return err == nil
}

func findChrome() (string, error) {
	candidates := []string{}
	switch runtime.GOOS {
	case "windows":
		candidates = []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
			filepath.Join(os.Getenv("LOCALAPPDATA"), `Google\Chrome\Application\chrome.exe`),
			`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
			`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
			filepath.Join(os.Getenv("LOCALAPPDATA"), `Microsoft\Edge\Application\msedge.exe`),
			`chrome`, `chromium`, `msedge`,
		}
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
			"google-chrome", "chrome", "chromium", "msedge", "brave",
		}
	default: // linux and others
		candidates = []string{
			"google-chrome-stable", "google-chrome", "chrome",
			"chromium-browser", "chromium", "msedge", "brave-browser", "brave",
		}
	}

	for _, name := range candidates {
		if path, err := exec.LookPath(name); err == nil {
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
		if _, err := os.Stat(name); err == nil {
			return name, nil
		}
	}
	return "", fmt.Errorf("no Chrome/Chromium/Edge executable found")
}

// buildPrintHTML assembles a single-page print-optimized HTML document.
// It contains a cover page, table of contents, and all chapters organised
// sequentially with page-break controls.
func buildPrintHTML(wiki *Wiki, graph *grapher.Graph) (string, error) {
	var b strings.Builder

	b.WriteString(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<title>`)
	b.WriteString(HTMLEscape(wiki.ProjectName))
	b.WriteString(` Wiki</title>
<style>
`)
	b.WriteString(printPageCSS)
	b.WriteString(`
</style>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/gh/highlightjs/cdn-release@11.9.0/build/styles/github.min.css">
<script src="https://cdn.jsdelivr.net/gh/highlightjs/cdn-release@11.9.0/build/highlight.min.js"></script>
<script type="module">
  import mermaid from 'https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.esm.min.mjs';
  mermaid.initialize({ startOnLoad:true, securityLevel:'loose', theme:'neutral' });
</script>
<script>
document.addEventListener('DOMContentLoaded', function() {
  if (typeof hljs !== 'undefined') { hljs.highlightAll(); }
  // Give mermaid a moment to render before Chrome captures the page
  setTimeout(function() { document.body.classList.add('ready'); }, 2500);
});
</script>
</head>
<body>
`)

	// ---- Cover page ----
	b.WriteString(`<div class="cover-page">
<div class="cover-inner">
<div class="cover-logo">`)
	b.WriteString(HTMLEscape(wiki.ProjectName))
	b.WriteString(`</div>
<h1 class="cover-title">项目文档</h1>
<p class="cover-meta">CodeWiki 自动生成 · `)
	b.WriteString(time.Now().Format("2006-01-02"))
	b.WriteString(`</p>
</div>
</div>
`)

	// ---- Table of contents ----
	b.WriteString(`<div class="toc-page chapter-page">
<h2 class="toc-title">目录</h2>
<ul class="toc-list">
`)
	tocItems := []struct{ id, title string }{
		{"overview", "项目概述"},
		{"what-it-does", "项目能做什么"},
		{"architecture", "架构说明"},
		{"project-structure", "项目结构"},
	}
	if wiki.KeyConcepts != "" {
		tocItems = append(tocItems, struct{ id, title string }{"key-concepts", "核心概念"})
	}
	tocItems = append(tocItems, struct{ id, title string }{"learning-path", "学习路径"})
	for _, theme := range sortedThemeKeys(wiki.ModuleThemes) {
		title := theme
		if ct, ok := wiki.ChapterTitles[theme]; ok && ct.Title != "" {
			title = ct.Title
		}
		tocItems = append(tocItems, struct{ id, title string }{"theme-" + safeThemeName(theme), title})
	}
	if wiki.APIReference != "" {
		tocItems = append(tocItems, struct{ id, title string }{"api-reference", "API 参考"})
	}

	for _, item := range tocItems {
		b.WriteString(fmt.Sprintf(`<li><a href="#%s">%s</a></li>
`, item.id, HTMLEscape(item.title)))
	}
	b.WriteString(`</ul>
</div>
`)

	primaryExt := primaryExtForLang(wiki.Language)

	// ---- Helper to emit a section ----
	emitSection := func(id, title, markdown string) {
		if strings.TrimSpace(markdown) == "" {
			return
		}
		b.WriteString(fmt.Sprintf(`<section id="%s" class="chapter-page">
<h1 class="chapter-title">%s</h1>
`, id, HTMLEscape(title)))
		body := RenderMarkdownBody([]byte(markdown))
		body = makeSourceRefsClickable(body, primaryExt)
		b.WriteString(body)
		b.WriteString(`</section>
`)
	}

	emitSection("overview", "项目概述", wiki.Overview)
	emitSection("what-it-does", "项目能做什么", wiki.WhatItDoes)
	emitSection("architecture", "架构说明", wiki.Architecture)
	emitSection("project-structure", "项目结构", wiki.ProjectStructure)
	if wiki.KeyConcepts != "" {
		emitSection("key-concepts", "核心概念", "# 核心概念与设计决策\n\n"+wiki.KeyConcepts)
	}
	emitSection("learning-path", "学习路径", wiki.LearningPath)

	// ---- Theme chapters ----
	for _, theme := range sortedThemeKeys(wiki.ModuleThemes) {
		ct, hasTitle := wiki.ChapterTitles[theme]
		modNames := wiki.ModuleThemes[theme]
		title := theme
		subtitle := ""
		if hasTitle && ct.Title != "" {
			title = ct.Title
			subtitle = ct.Subtitle
		}
		secID := "theme-" + safeThemeName(theme)

		b.WriteString(fmt.Sprintf(`<section id="%s" class="chapter-page">
<h1 class="chapter-title">%s</h1>
`, secID, HTMLEscape(title)))
		if subtitle != "" {
			b.WriteString(fmt.Sprintf(`<p class="chapter-subtitle-print">%s</p>
`, HTMLEscape(subtitle)))
		}

		// Difficulty + module count
		b.WriteString(`<div class="chapter-meta-print">`)
		if hasTitle && ct.Difficulty != "" {
			b.WriteString(fmt.Sprintf(`<span class="chapter-diff-print">%s</span>`, HTMLEscape(ct.Difficulty)))
		}
		b.WriteString(fmt.Sprintf(`<span>%d 个模块</span></div>
`, len(modNames)))

		// Learning goals
		if hasTitle && len(ct.LearningGoals) > 0 {
			b.WriteString(`<div class="chapter-goals-print"><strong>学习目标</strong><ul>`)
			for _, g := range ct.LearningGoals {
				b.WriteString(fmt.Sprintf(`<li>%s</li>`, HTMLEscape(g)))
			}
			b.WriteString(`</ul></div>
`)
		}

		// Prerequisites
		if hasTitle && len(ct.Prerequisites) > 0 {
			b.WriteString(`<div class="chapter-prereqs-print"><strong>前置知识</strong><ul>`)
			for _, p := range ct.Prerequisites {
				b.WriteString(fmt.Sprintf(`<li>%s</li>`, HTMLEscape(p)))
			}
			b.WriteString(`</ul></div>
`)
		}

		// Theme intro
		if intro, ok := wiki.ThemeIntros[theme]; ok && intro != "" {
			b.WriteString(fmt.Sprintf(`<div class="chapter-intro-print"><p>%s</p></div>
`, HTMLEscape(intro)))
		}

		// Narrative
		if narrative := wiki.ChapterNarratives[theme]; narrative != "" {
			b.WriteString(`<div class="chapter-narrative-print">`)
			b.WriteString(makeSourceRefsClickable(RenderMarkdownBody([]byte(narrative)), primaryExt))
			b.WriteString(`</div>
`)
		}

		// Module docs
		hasModDocs := false
		for _, modName := range modNames {
			if _, ok := wiki.ModuleDocs[modName]; ok {
				hasModDocs = true
				break
			}
		}
		if hasModDocs {
			b.WriteString(`<h2>模块详情</h2>
`)
			for _, modName := range modNames {
				doc, ok := wiki.ModuleDocs[modName]
				if !ok {
					continue
				}
				label := filepath.Base(modName)
				if cn, ok := wiki.ModuleChineseNames[modName]; ok && cn != "" {
					label = fmt.Sprintf("%s（%s）", cn, filepath.Base(modName))
				}
				b.WriteString(fmt.Sprintf(`<h3 id="module-%s">%s</h3>
`, safeThemeName(modName), HTMLEscape(label)))
				b.WriteString(makeSourceRefsClickable(RenderMarkdownBody([]byte(doc)), primaryExt))
			}
		}

		b.WriteString(`</section>
`)
	}

	if wiki.APIReference != "" {
		emitSection("api-reference", "API 参考", wiki.APIReference)
	}

	b.WriteString(`</body>
</html>`)

	return b.String(), nil
}

const printPageCSS = `
/* ===== Base ===== */
:root {
  --bg: #ffffff; --bg2: #f6f8fa; --bg3: #e9ecef;
  --text: #1a1a2e; --text2: #4a4a68; --text3: #8b8ba7;
  --border: #e2e4e9; --border2: rgba(0,0,0,.06);
  --accent: #6366f1; --accent2: #818cf8; --accent-glow: rgba(99,102,241,.15);
  --accent-gradient: linear-gradient(135deg, #6366f1, #8b5cf6, #a78bfa);
  --radius: 10px; --radius-lg: 16px;
  --pre-bg: #1e1e2e; --pre-text: #cdd6f4;
  --inline-code-bg: rgba(99,102,241,.08);
}
* { box-sizing: border-box; margin: 0; }
html { scroll-behavior: smooth; }
body {
  font-family: -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Noto Sans SC","PingFang SC",sans-serif;
  line-height: 1.75; color: var(--text); background: var(--bg);
  font-size: 11pt;
}

/* ===== Print rules ===== */
@media print {
  @page { margin: 18mm 16mm; size: A4; }
  body { background: #fff; }
  .chapter-page { page-break-before: always; }
  h1, h2, h3 { page-break-after: avoid; }
  pre, blockquote, table, img, svg, .mermaid { page-break-inside: avoid; }
  a { text-decoration: none; color: var(--text); }
  a[href]::after { content: none !important; }
  /* Chrome PDF often drops background-clip:text; fallback to solid colors */
  .cover-logo, .chapter-title, h1 {
    -webkit-text-fill-color: #1a1a2e !important;
    color: #1a1a2e !important;
    background: none !important;
  }
}

/* ===== Cover ===== */
.cover-page {
  display: flex; align-items: center; justify-content: center;
  min-height: 100vh; page-break-after: always;
  text-align: center;
}
.cover-inner { max-width: 80%; }
.cover-logo {
  font-size: 2.2em; font-weight: 800;
  background: var(--accent-gradient);
  -webkit-background-clip: text; -webkit-text-fill-color: transparent; background-clip: text;
  margin-bottom: 12px;
}
.cover-title { font-size: 1.6em; font-weight: 700; color: var(--text2); margin-bottom: 16px; }
.cover-meta { font-size: 0.95em; color: var(--text3); }

/* ===== TOC ===== */
.toc-page { padding: 60px 40px; }
.toc-title {
  font-size: 1.8em; font-weight: 700; margin-bottom: 32px;
  border-bottom: 2px solid var(--border); padding-bottom: 12px;
}
.toc-list { list-style: none; padding: 0; }
.toc-list li { padding: 10px 0; border-bottom: 1px solid var(--border2); font-size: 1.05em; }
.toc-list li a { color: var(--text); text-decoration: none; }

/* ===== Chapter typography ===== */
.chapter-page { padding: 40px 0; }
.chapter-title {
  font-size: 1.9em; font-weight: 700; margin-bottom: 20px;
  border-bottom: 2px solid var(--border); padding-bottom: 10px;
  background: var(--accent-gradient);
  -webkit-background-clip: text; -webkit-text-fill-color: transparent; background-clip: text;
}
.chapter-subtitle-print { font-size: 1.15em; color: var(--text2); margin: -8px 0 16px; }
.chapter-meta-print { display: flex; gap: 12px; margin-bottom: 20px; font-size: 0.9em; color: var(--text3); }
.chapter-diff-print {
  display: inline-block; padding: 2px 10px; border-radius: 20px;
  background: var(--accent-glow); color: var(--accent); font-weight: 600;
}
.chapter-goals-print, .chapter-prereqs-print {
  margin: 16px 0; padding: 12px 18px;
  background: var(--bg2); border-radius: var(--radius); border: 1px solid var(--border2);
}
.chapter-goals-print ul, .chapter-prereqs-print ul { margin: 8px 0 0 18px; padding: 0; }
.chapter-goals-print li, .chapter-prereqs-print li { font-size: 0.95em; color: var(--text2); line-height: 1.6; }
.chapter-intro-print {
  margin: 20px 0 28px; padding: 16px 20px;
  background: var(--accent-glow); border-radius: var(--radius-lg);
  border-left: 3px solid var(--accent);
}
.chapter-intro-print p { margin: 0; font-size: 0.95em; color: var(--text2); line-height: 1.7; }
.chapter-narrative-print { line-height: 1.8; }

/* ===== Content elements ===== */
h1 { font-size: 1.7em; margin-top: 32px; margin-bottom: 14px; font-weight: 700; }
h2 { font-size: 1.4em; margin-top: 28px; margin-bottom: 12px; font-weight: 700; border-bottom: 1px solid var(--border); padding-bottom: 6px; }
h3 { font-size: 1.2em; margin-top: 24px; margin-bottom: 10px; font-weight: 700; }
h4 { font-size: 1.05em; margin-top: 20px; margin-bottom: 8px; font-weight: 700; }
p { margin: 10px 0; }
a { color: var(--accent); text-decoration: none; }
ul, ol { padding-left: 1.8em; margin: 10px 0; }
li+li { margin-top: .25em; }

/* ===== Code ===== */
:not(pre) > code {
  font-family: "Cascadia Code","Fira Code","JetBrains Mono",ui-monospace,SFMono-Regular,monospace;
  background: var(--inline-code-bg); padding: .15em .4em; border-radius: 4px;
  font-size: 85%; color: var(--accent); font-weight: 500;
}
pre {
  background: var(--pre-bg); color: var(--pre-text);
  padding: 14px 16px; overflow: auto; border-radius: var(--radius);
  margin: 14px 0; font-size: 9.5pt; line-height: 1.55;
}
pre code { background: none; padding: 0; font-size: inherit; line-height: inherit; }
.code-block { border-radius: var(--radius); overflow: hidden; margin: 14px 0; border: 1px solid var(--border2); }
.code-header { display: flex; align-items: center; justify-content: space-between; padding: 5px 12px; background: var(--bg3); border-bottom: 1px solid var(--border2); }
.code-lang { font-size: 10px; font-weight: 700; color: var(--text3); text-transform: uppercase; }
.code-copy { display: none; } /* hide in print */

/* ===== Blockquote ===== */
blockquote {
  margin: 16px 0; padding: 12px 18px; color: var(--text2);
  border-left: 3px solid; border-image: var(--accent-gradient) 1;
  background: var(--accent-glow); border-radius: 0 var(--radius) var(--radius) 0;
}

/* ===== Table ===== */
table { border-collapse: collapse; width: 100%; margin: 14px 0; font-size: 0.95em; }
th, td { border: 1px solid var(--border); padding: 8px 12px; text-align: left; }
tr:nth-child(even) { background: var(--bg2); }
th { background: var(--bg3); font-weight: 700; }

/* ===== Mermaid ===== */
.mermaid-wrap { position: relative; margin: 14px 0; }
.mermaid { background: var(--bg2); padding: 20px; border-radius: var(--radius); overflow: auto; border: 1px solid var(--border2); }
.diagram-expand { display: none !important; }

/* ===== Source ref ===== */
.source-ref {
  display: inline-block; font-weight: 700; color: var(--accent);
  background: var(--inline-code-bg); padding: 0 4px; border-radius: 4px;
  font-family: "Cascadia Code","Fira Code","JetBrains Mono",ui-monospace,SFMono-Regular,monospace;
  font-size: 85%;
}

/* ===== Utility ===== */
hr { height: 1px; padding: 0; margin: 28px 0; background: var(--border); border: 0; }
img { max-width: 100%; height: auto; }
`
