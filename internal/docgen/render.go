package docgen

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var nonAlnumRe = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// reSourceRef matches rendered source attribution:
// <em>来源：<code>path/file.go</code></em> or <em>来源：path/file.go</em>
var reSourceRef = regexp.MustCompile(`<em>来源：(?:<code>)?([^<]+)(?:</code>)?</em>`)

func headingSlug(text string) string {
	plain := regexp.MustCompile(`<[^>]+>`).ReplaceAllString(text, "")
	slug := strings.TrimSpace(plain)
	slug = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return '-'
	}, slug)
	slug = nonAlnumRe.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "section"
	}
	return slug
}

// NavItem represents a single clickable item in the sidebar navigation.
type NavItem struct {
	URL         string    // href target (relative file path or anchor)
	Title       string    // display name
	Icon        string    // emoji icon
	ReadingTime int       // minutes, 0 = don't show
	Difficulty  string    // "⭐", "⭐⭐", "⭐⭐⭐", "" = don't show
	SubItems    []NavItem // nested items (e.g. modules under a chapter)
}

// NavSection represents a collapsible group in the sidebar.
type NavSection struct {
	Label string    // section label, e.g. "认识项目"
	Icon  string    // section emoji, e.g. "📘"
	Items []NavItem
}

// EstimateReadingTime estimates reading time in minutes for Chinese+code content.
func EstimateReadingTime(content string) int {
	chars := 0
	for _, r := range content {
		if r > 127 {
			chars++
		} else if r == ' ' || r == '\n' {
			continue
		} else {
			chars++
		}
	}
	minutes := chars / 400
	if minutes < 1 {
		minutes = 1
	}
	return minutes
}

// RenderMarkdownBody converts Markdown to HTML body content (no html/head/body wrapper).
func RenderMarkdownBody(src []byte) string {
	lines := strings.Split(string(src), "\n")
	var body strings.Builder

	var inCodeBlock bool
	var codeLang string
	var codeLines []string
	var inUL, inOL bool
	var inTable bool
	var tableRows []string

	flushCode := func() {
		if !inCodeBlock {
			return
		}
		if codeLang == "mermaid" {
			body.WriteString("<div class=\"mermaid-wrap\"><div class=\"mermaid\">\n")
			for _, cl := range codeLines {
				body.WriteString(cl)
				body.WriteByte('\n')
			}
			body.WriteString("</div><button class=\"diagram-expand\" title=\"全屏查看\" onclick=\"expandDiagram(this)\">&#x26F6;</button></div>\n")
		} else {
			langClass := ""
			langLabel := codeLang
			if codeLang != "" {
				langClass = fmt.Sprintf(` class="language-%s"`, codeLang)
			}
			if langLabel == "" {
				langLabel = "code"
			}
			body.WriteString(fmt.Sprintf("<div class=\"code-block\"><div class=\"code-header\"><span class=\"code-lang\">%s</span><button class=\"code-copy\" onclick=\"copyCode(this)\" title=\"复制代码\"><svg width=\"14\" height=\"14\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2\"><rect x=\"9\" y=\"9\" width=\"13\" height=\"13\" rx=\"2\"/><path d=\"M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1\"/></svg><span>复制</span></button></div><pre><code%s>", HTMLEscape(langLabel), langClass))
			for _, cl := range codeLines {
				body.WriteString(HTMLEscape(cl))
				body.WriteByte('\n')
			}
			body.WriteString("</code></pre></div>\n")
		}
		inCodeBlock = false
		codeLang = ""
		codeLines = nil
	}

	flushList := func() {
		if inUL {
			body.WriteString("</ul>\n")
			inUL = false
		}
		if inOL {
			body.WriteString("</ol>\n")
			inOL = false
		}
	}

	flushTable := func() {
		if !inTable || len(tableRows) == 0 {
			return
		}
		body.WriteString("<table>\n")
		for i, row := range tableRows {
			body.WriteString("<tr>\n")
			cells := splitTableCells(row)
			for _, cell := range cells {
				cell = strings.TrimSpace(cell)
				if i == 0 {
					body.WriteString("<th>")
					body.WriteString(RenderInline(cell))
					body.WriteString("</th>\n")
				} else if isTableSeparator(row) {
					continue
				} else {
					body.WriteString("<td>")
					body.WriteString(RenderInline(cell))
					body.WriteString("</td>\n")
				}
			}
			body.WriteString("</tr>\n")
		}
		body.WriteString("</table>\n")
		inTable = false
		tableRows = nil
	}

	for i, line := range lines {
		trimmed := strings.TrimRight(line, " \r\t")

		// Code blocks
		if strings.HasPrefix(trimmed, "```") {
			flushList()
			flushTable()
			if !inCodeBlock {
				inCodeBlock = true
				codeLang = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
				codeLines = nil
			} else {
				flushCode()
			}
			continue
		}
		if inCodeBlock {
			codeLines = append(codeLines, line)
			continue
		}

		// Empty lines flush lists/tables
		if trimmed == "" {
			flushList()
			flushTable()
			continue
		}

		// Horizontal rule
		if trimmed == "---" || trimmed == "***" || trimmed == "___" {
			flushList()
			flushTable()
			body.WriteString("<hr>\n")
			continue
		}

		// Blockquote
		if strings.HasPrefix(trimmed, "> ") {
			flushList()
			flushTable()
			body.WriteString("<blockquote>")
			body.WriteString(RenderInline(strings.TrimPrefix(trimmed, "> ")))
			body.WriteString("</blockquote>\n")
			continue
		}

		// Table
		if strings.HasPrefix(trimmed, "|") {
			flushList()
			inTable = true
			tableRows = append(tableRows, trimmed)
			continue
		} else if inTable {
			flushTable()
		}

		// Unordered list
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			flushTable()
			if !inUL {
				body.WriteString("<ul>\n")
				inUL = true
			}
			item := strings.TrimPrefix(trimmed, "- ")
			item = strings.TrimPrefix(item, "* ")
			body.WriteString("<li>")
			body.WriteString(RenderInline(item))
			body.WriteString("</li>\n")
			continue
		}

		// Ordered list
		if orderedListMatch(trimmed) {
			flushTable()
			if !inOL {
				body.WriteString("<ol>\n")
				inOL = true
			}
			item := orderedListItem(trimmed)
			body.WriteString("<li>")
			body.WriteString(RenderInline(item))
			body.WriteString("</li>\n")
			continue
		}

		// Flush any open list if line is not a list item
		flushList()

		// Headers
		if strings.HasPrefix(trimmed, "# ") {
			content := RenderInline(strings.TrimPrefix(trimmed, "# "))
			body.WriteString(fmt.Sprintf(`<h1 id="%s">`, headingSlug(content)))
			body.WriteString(content)
			body.WriteString("</h1>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			content := RenderInline(strings.TrimPrefix(trimmed, "## "))
			body.WriteString(fmt.Sprintf(`<h2 id="%s">`, headingSlug(content)))
			body.WriteString(content)
			body.WriteString("</h2>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "### ") {
			content := RenderInline(strings.TrimPrefix(trimmed, "### "))
			body.WriteString(fmt.Sprintf(`<h3 id="%s">`, headingSlug(content)))
			body.WriteString(content)
			body.WriteString("</h3>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "#### ") {
			content := RenderInline(strings.TrimPrefix(trimmed, "#### "))
			body.WriteString(fmt.Sprintf(`<h4 id="%s">`, headingSlug(content)))
			body.WriteString(content)
			body.WriteString("</h4>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "##### ") {
			content := RenderInline(strings.TrimPrefix(trimmed, "##### "))
			body.WriteString(fmt.Sprintf(`<h5 id="%s">`, headingSlug(content)))
			body.WriteString(content)
			body.WriteString("</h5>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "###### ") {
			content := RenderInline(strings.TrimPrefix(trimmed, "###### "))
			body.WriteString(fmt.Sprintf(`<h6 id="%s">`, headingSlug(content)))
			body.WriteString(content)
			body.WriteString("</h6>\n")
			continue
		}

		// Paragraph (or continuation of previous paragraph)
		if i > 0 && body.Len() > 0 && !strings.HasSuffix(body.String(), "\n") {
			body.WriteByte(' ')
			body.WriteString(RenderInline(trimmed))
		} else {
			body.WriteString("<p>")
			body.WriteString(RenderInline(trimmed))
			body.WriteString("</p>\n")
		}
	}

	flushList()
	flushTable()
	flushCode()

	return body.String()
}

// RenderMarkdownWithSources renders markdown to HTML and post-processes source
// attribution tags into clickable .source-ref spans with the correct file
// extension for the target project language (e.g. ".go" for Go projects).
func RenderMarkdownWithSources(src []byte, lang string) string {
	return makeSourceRefsClickable(RenderMarkdownBody(src), primaryExtForLang(lang))
}

// makeSourceRefsClickable transforms rendered source attribution HTML into
// clickable spans. Handles single/multiple code-wrapped paths, bracket-wrapped
// paths, and plain-text paths.
// primaryExt is the primary file extension for the target project language (e.g. ".go", ".py").
// Pass "" to leave paths without extension unchanged.
func makeSourceRefsClickable(html string, primaryExt string) string {
	reSrcBlock := regexp.MustCompile(`<em>来源：(.+?)</em>`)

	return reSrcBlock.ReplaceAllStringFunc(html, func(block string) string {
		m := reSrcBlock.FindStringSubmatch(block)
		if len(m) < 2 {
			return block
		}
		inner := m[1]

		// Extract individual file paths from <code> tags
		reCode := regexp.MustCompile(`<code>([^<]+)</code>`)
		codes := reCode.FindAllStringSubmatch(inner, -1)
		if len(codes) > 0 {
			var refs []string
			for _, c := range codes {
				path := cleanSourcePath(c[1])
				if path != "" {
					refs = append(refs, buildSourceSpan(path, primaryExt))
				}
			}
			if len(refs) > 0 {
				return "来源：" + strings.Join(refs, "、")
			}
		}

		// Fallback: split plain text on delimiters (handles comma-separated multi-path)
		plain := reCode.ReplaceAllString(inner, "")
		plain = strings.TrimSpace(plain)
		if plain == "" {
			return block
		}
		parts := splitPathList(plain)
		var refs []string
		for _, p := range parts {
			path := cleanSourcePath(p)
			if path != "" {
				refs = append(refs, buildSourceSpan(path, primaryExt))
			}
		}
		if len(refs) > 0 {
			return "来源：" + strings.Join(refs, "、")
		}
		return block
	})
}

// buildSourceSpan creates a clickable source-ref span for a single file path.
// If the path has no extension and primaryExt is provided, the extension is appended
// to both data-file and the display name, so the popup title shows the correct filename.
func buildSourceSpan(rawPath, primaryExt string) string {
	fileAttr := rawPath
	if filepath.Ext(rawPath) == "" && primaryExt != "" {
		fileAttr = rawPath + primaryExt
	}
	displayName := filepath.Base(fileAttr)
	return fmt.Sprintf(`<span class="source-ref" data-file="%s"><strong>%s</strong></span>`, fileAttr, displayName)
}

// primaryExtForLang returns the primary file extension for the given language name.
// Returns "" for unknown languages so callers know not to add any extension.
func primaryExtForLang(lang string) string {
	m := map[string]string{
		"python": ".py", "go": ".go",
		"javascript": ".js", "typescript": ".ts",
		"rust": ".rs", "java": ".java",
		"cpp": ".cpp", "c": ".c",
		"ruby": ".rb", "php": ".php",
		"swift": ".swift", "kotlin": ".kt",
	}
	return m[strings.ToLower(strings.TrimSpace(lang))]
}

// cleanSourcePath strips brackets and whitespace from a file path.
func cleanSourcePath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "[")
	p = strings.TrimSuffix(p, "]")
	p = strings.TrimSuffix(p, "/") // remove trailing slash
	return strings.TrimSpace(p)
}

// splitPathList splits a string on common path delimiters.
func splitPathList(s string) []string {
	s = strings.ReplaceAll(s, "、", "\x00")
	s = strings.ReplaceAll(s, "，", "\x00")
	s = strings.ReplaceAll(s, ", ", "\x00")
	s = strings.ReplaceAll(s, ",", "\x00")
	parts := strings.Split(s, "\x00")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// wikiPageCSS is the shared CSS for all wiki pages (serve mode).
const wikiPageCSS = `
:root {
  --bg: #ffffff; --bg2: #f6f8fa; --bg3: #e9ecef;
  --text: #1a1a2e; --text2: #4a4a68; --text3: #8b8ba7;
  --border: #e2e4e9; --border2: rgba(0,0,0,.06);
  --accent: #6366f1; --accent2: #818cf8; --accent-glow: rgba(99,102,241,.15);
  --accent-gradient: linear-gradient(135deg, #6366f1, #8b5cf6, #a78bfa);
  --shadow: 0 1px 3px rgba(0,0,0,.06), 0 1px 2px rgba(0,0,0,.04);
  --shadow-md: 0 4px 12px rgba(0,0,0,.08);
  --shadow-lg: 0 8px 30px rgba(0,0,0,.12);
  --radius: 10px; --radius-lg: 16px;
  --pre-bg: #1e1e2e; --pre-text: #cdd6f4;
  --inline-code-bg: rgba(99,102,241,.08);
  --sidebar-w: 260px;
  --topbar-h: 52px;
}
[data-theme="dark"] {
  --bg: #0d1117; --bg2: #161b22; --bg3: #21262d;
  --text: #e6edf3; --text2: #9da5b4; --text3: #545d68;
  --border: #30363d; --border2: rgba(255,255,255,.06);
  --accent: #818cf8; --accent2: #a78bfa; --accent-glow: rgba(129,140,248,.2);
  --accent-gradient: linear-gradient(135deg, #818cf8, #a78bfa, #c4b5fd);
  --shadow: 0 1px 3px rgba(0,0,0,.3); --shadow-md: 0 4px 12px rgba(0,0,0,.4); --shadow-lg: 0 8px 30px rgba(0,0,0,.5);
  --pre-bg: #0d1117; --pre-text: #e6edf3;
  --inline-code-bg: rgba(129,140,248,.12);
}
* { box-sizing: border-box; margin: 0; }
html { scroll-behavior: smooth; }
body { font-family: -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Noto Sans SC","PingFang SC",sans-serif; line-height:1.75; color:var(--text); background:var(--bg); transition:background .3s,color .3s; overflow-x:hidden; }

/* ---- Reading progress bar ---- */
#reading-progress { position:fixed; top:0; left:0; height:3px; background:var(--accent-gradient); z-index:9999; transition:width .15s; width:0; border-radius:0 2px 2px 0; }

/* ---- Top bar ---- */
.topbar { position:fixed; top:0; left:var(--sidebar-w); right:0; height:var(--topbar-h); background:rgba(255,255,255,.72); backdrop-filter:blur(16px) saturate(180%); -webkit-backdrop-filter:blur(16px) saturate(180%); border-bottom:1px solid var(--border2); z-index:50; display:flex; align-items:center; padding:0 24px; gap:12px; transition:background .3s; }
[data-theme="dark"] .topbar { background:rgba(13,17,23,.78); }
.topbar-title { font-weight:700; font-size:14px; color:var(--text); white-space:nowrap; overflow:hidden; text-overflow:ellipsis; max-width:200px; }
.topbar-search { flex:1; max-width:560px; position:relative; }
.topbar-search input { width:100%; padding:7px 14px 7px 36px; border:1px solid var(--border); border-radius:8px; font-size:13px; background:var(--bg2); color:var(--text); outline:none; transition:all .2s; }
.topbar-search input:focus { border-color:var(--accent); box-shadow:0 0 0 3px var(--accent-glow); }
.topbar-search .search-icon { position:absolute; left:10px; top:50%; transform:translateY(-50%); color:var(--text3); pointer-events:none; }
.topbar-search kbd { position:absolute; right:92px; top:50%; transform:translateY(-50%); font-size:11px; padding:2px 6px; background:var(--bg3); border:1px solid var(--border); border-radius:4px; color:var(--text3); font-family:inherit; }
.topbar-search .topbar-btn { position:absolute; right:10px; top:50%; transform:translateY(-50%); }
.topbar-actions { display:flex; align-items:center; gap:8px; margin-left:auto; }
.topbar-btn { display:inline-flex; align-items:center; gap:6px; padding:6px 14px; font-size:12px; font-weight:600; border:1px solid var(--border); border-radius:8px; background:var(--bg); color:var(--text2); cursor:pointer; transition:all .2s; text-decoration:none; }
.topbar-btn:hover { background:var(--bg2); border-color:var(--accent); color:var(--accent); box-shadow:0 0 0 3px var(--accent-glow); }
.topbar-btn.primary { background:var(--accent-gradient); color:#fff; border:none; }
.topbar-btn.primary:hover { opacity:.9; box-shadow:0 0 0 3px var(--accent-glow); }
.theme-toggle { background:none; border:1px solid var(--border); border-radius:8px; cursor:pointer; font-size:15px; padding:6px 10px; line-height:1; color:var(--text2); transition:all .2s; }
.theme-toggle:hover { background:var(--bg2); border-color:var(--accent); }

/* ---- Sidebar ---- */
.sidebar { width:var(--sidebar-w); background:rgba(246,248,250,.85); backdrop-filter:blur(12px) saturate(180%); -webkit-backdrop-filter:blur(12px) saturate(180%); border-right:1px solid var(--border2); height:100vh; position:fixed; left:0; top:0; overflow-y:auto; z-index:60; transition:background .3s; }
[data-theme="dark"] .sidebar { background:rgba(22,27,34,.88); }
.sidebar::-webkit-scrollbar { width:4px; }
.sidebar::-webkit-scrollbar-thumb { background:var(--border); border-radius:4px; }
.sidebar-header { padding:14px 18px; font-weight:700; font-size:15px; border-bottom:1px solid var(--border2); background:rgba(255,255,255,.6); backdrop-filter:blur(8px); position:sticky; top:0; z-index:3; display:flex; align-items:center; gap:10px; }
[data-theme="dark"] .sidebar-header { background:rgba(13,17,23,.6); }
.sidebar-header .logo-dot { width:10px; height:10px; border-radius:50%; background:var(--accent-gradient); display:inline-block; flex-shrink:0; box-shadow:0 0 8px var(--accent-glow); }
.sidebar ul { list-style:none; padding:0; margin:0; }
.nav-group { border-bottom:1px solid var(--border2); }
.nav-group-header { display:flex; align-items:center; justify-content:space-between; padding:12px 18px 8px; font-size:11px; font-weight:700; color:var(--text3); text-transform:uppercase; letter-spacing:.8px; cursor:pointer; user-select:none; transition:color .15s; }
.nav-group-header:hover { color:var(--accent); }
.nav-group-header .chevron { transition:transform .25s; font-size:10px; }
.nav-group.collapsed .chevron { transform:rotate(-90deg); }
.nav-group.collapsed .nav-group-items { display:none; }
.nav-group-items li a { display:flex; align-items:center; gap:8px; padding:7px 18px 7px 22px; color:var(--text2); text-decoration:none; font-size:13px; transition:all .2s; border-left:2px solid transparent; position:relative; }
.nav-group-items li a:hover { background:var(--accent-glow); color:var(--text); border-left-color:var(--accent); }
.nav-group-items li a.active { background:var(--accent-gradient); color:#fff; font-weight:600; border-left-color:transparent; border-radius:0 6px 6px 0; margin-right:8px; }
.nav-group-items li a .nav-icon { font-size:14px; flex-shrink:0; width:20px; text-align:center; }
.nav-group-items li a .nav-title { flex:1; min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.nav-group-items li a .nav-meta { display:flex; align-items:center; gap:4px; flex-shrink:0; margin-left:auto; }
.nav-group-items li a .nav-time { font-size:10px; padding:1px 5px; border-radius:8px; background:var(--inline-code-bg); color:var(--text3); font-weight:500; }
.nav-group-items li a .nav-diff { font-size:10px; padding:1px 5px; border-radius:8px; background:var(--accent-glow); color:var(--accent); font-weight:600; }
.nav-group-items li a.active .nav-time { background:rgba(255,255,255,.25); color:rgba(255,255,255,.85); }
.nav-group-items li a.active .nav-diff { background:rgba(255,255,255,.25); color:#fff; }
.nav-group-header { gap:8px; }
.nav-group-count { font-size:10px; padding:1px 6px; border-radius:10px; background:var(--bg3); color:var(--text3); font-weight:600; }
.sidebar-summary { padding:10px 18px; font-size:11px; color:var(--text3); border-bottom:1px solid var(--border2); display:flex; align-items:center; gap:4px; }
.sidebar-summary .dot { margin:0 2px; }

/* ---- Right panel (Ask AI + Search) ---- */
.right-panel { position:fixed; top:0; right:0; width:380px; height:100vh; background:var(--bg); border-left:1px solid var(--border); z-index:65; display:flex; flex-direction:column; transform:translateX(100%); transition:transform .25s cubic-bezier(.4,0,.2,1); box-shadow:none; }
.right-panel.open { transform:translateX(0); box-shadow:-4px 0 24px rgba(0,0,0,.08); }
[data-theme="dark"] .right-panel.open { box-shadow:-4px 0 24px rgba(0,0,0,.3); }
body.rp-open .right-sidebar { transform:translateX(100%); transition:transform .25s cubic-bezier(.4,0,.2,1); }
body.rp-open .content { margin-right:380px; transition:margin-right .25s cubic-bezier(.4,0,.2,1); }
body.rp-open .topbar { right:380px; transition:right .25s cubic-bezier(.4,0,.2,1); }
.rp-header { display:flex; align-items:center; gap:0; padding:0; border-bottom:1px solid var(--border); background:var(--bg2); height:var(--topbar-h); flex-shrink:0; }
.rp-tab { flex:1; padding:0 12px; height:100%; border:none; background:none; font-size:13px; font-weight:600; color:var(--text3); cursor:pointer; transition:all .2s; border-bottom:2px solid transparent; }
.rp-tab:hover { color:var(--text); background:var(--accent-glow); }
.rp-tab.active { color:var(--accent); border-bottom-color:var(--accent); }
.rp-close { width:42px; height:100%; border:none; background:none; font-size:18px; cursor:pointer; color:var(--text3); display:flex; align-items:center; justify-content:center; flex-shrink:0; border-left:1px solid var(--border); transition:all .15s; }
.rp-close:hover { background:var(--bg3); color:var(--text); }
.rp-body { flex:1; overflow:hidden; display:flex; flex-direction:column; }
.rp-pane { display:none; flex-direction:column; flex:1; overflow:hidden; }
.rp-pane.active { display:flex; }
.rp-search-input { width:100%; padding:12px 16px; border:none; border-bottom:1px solid var(--border); font-size:14px; background:var(--bg); color:var(--text); outline:none; flex-shrink:0; }
.rp-search-input:focus { background:var(--bg2); }
.rp-search-input::placeholder { color:var(--text3); }
.rp-results { flex:1; overflow-y:auto; }
.rp-result { display:block; padding:12px 16px; color:var(--text); text-decoration:none; border-bottom:1px solid var(--border2); transition:background .1s; cursor:pointer; }
.rp-result:hover { background:var(--accent-glow); }
.rp-result-title { font-size:13px; font-weight:600; display:block; }
.rp-result-path { font-size:11px; color:var(--text3); display:block; margin-top:2px; }
.rp-empty { padding:24px 16px; text-align:center; color:var(--text3); font-size:13px; }
.rp-chat { flex:1; overflow-y:auto; padding:16px; display:flex; flex-direction:column; gap:12px; }
.rp-msg { max-width:95%; animation:fadeUp .3s ease-out; }
.rp-msg.user { align-self:flex-end; }
.rp-msg .rp-bubble { padding:10px 14px; border-radius:12px; font-size:13px; line-height:1.6; word-break:break-word; }
.rp-msg.user .rp-bubble { background:var(--accent-gradient); color:#fff; border-bottom-right-radius:4px; }
.rp-msg.assistant .rp-bubble { background:var(--bg2); color:var(--text); border:1px solid var(--border); border-bottom-left-radius:4px; }
.rp-msg .rp-sources { margin-top:6px; display:flex; flex-wrap:wrap; gap:4px; }
.rp-msg .rp-src-tag { font-size:11px; padding:2px 8px; border-radius:10px; background:var(--accent-glow); color:var(--accent); cursor:pointer; font-weight:500; transition:all .15s; border:none; }
.rp-msg .rp-src-tag:hover { background:var(--accent); color:#fff; }
.rp-loading { display:flex; align-items:center; gap:8px; padding:10px 14px; color:var(--text3); font-size:13px; }
.rp-loading-dot { width:6px; height:6px; border-radius:50%; background:var(--accent); animation:pulse 1.2s infinite; }
.rp-loading-dot:nth-child(2) { animation-delay:.2s; }
.rp-loading-dot:nth-child(3) { animation-delay:.4s; }
.rp-input-area { display:flex; gap:8px; padding:12px; border-top:1px solid var(--border); background:var(--bg2); flex-shrink:0; }
.rp-input-area input { flex:1; padding:8px 14px; border:1px solid var(--border); border-radius:20px; font-size:13px; background:var(--bg); color:var(--text); outline:none; }
.rp-input-area input:focus { border-color:var(--accent); box-shadow:0 0 0 2px var(--accent-glow); }
.rp-input-area button { padding:8px 16px; background:var(--accent-gradient); color:#fff; border:none; border-radius:20px; font-size:13px; font-weight:600; cursor:pointer; transition:opacity .15s; }
.rp-input-area button:hover { opacity:.9; }
.rp-input-area button:disabled { opacity:.5; cursor:not-allowed; }
.rp-welcome { padding:32px 20px; text-align:center; color:var(--text3); }
.rp-welcome-icon { font-size:32px; margin-bottom:12px; }
.rp-welcome-title { font-size:14px; font-weight:600; color:var(--text2); margin-bottom:6px; }
.rp-welcome-desc { font-size:12px; line-height:1.6; }

/* ---- Content ---- */
.content { margin-left:var(--sidebar-w); padding:calc(var(--topbar-h) + 28px) 48px 60px; max-width:900px; width:100%; transition:margin-right .25s cubic-bezier(.4,0,.2,1); }
h1,h2,h3,h4,h5,h6 { margin-top:36px; margin-bottom:16px; font-weight:700; line-height:1.35; color:var(--text); }
h1 { font-size:2.1em; border-bottom:2px solid var(--border); padding-bottom:.3em; background:var(--accent-gradient); -webkit-background-clip:text; -webkit-text-fill-color:transparent; background-clip:text; border-image:var(--accent-gradient) 1; }
h2 { font-size:1.55em; border-bottom:1px solid var(--border); padding-bottom:.25em; }
h3 { font-size:1.25em; }
a { color:var(--accent); text-decoration:none; transition:color .15s; }
a:hover { color:var(--accent2); text-decoration:underline; }
p { margin:12px 0; }

/* ---- Code ---- */
:not(pre) > code { font-family:"Cascadia Code","Fira Code","JetBrains Mono",ui-monospace,SFMono-Regular,monospace; background:var(--inline-code-bg); padding:.15em .45em; border-radius:5px; font-size:85%; color:var(--accent); font-weight:500; }
.code-block { border-radius:var(--radius); overflow:hidden; box-shadow:var(--shadow-md); margin:20px 0; border:1px solid var(--border2); }
.code-header { display:flex; align-items:center; justify-content:space-between; padding:6px 14px; background:var(--bg3); border-bottom:1px solid var(--border2); }
[data-theme="dark"] .code-header { background:#1c2028; }
.code-lang { font-size:11px; font-weight:700; color:var(--text3); text-transform:uppercase; letter-spacing:.5px; }
.code-copy { display:inline-flex; align-items:center; gap:4px; background:none; border:none; color:var(--text3); font-size:11px; cursor:pointer; padding:3px 8px; border-radius:4px; transition:all .2s; font-family:inherit; }
.code-copy:hover { background:var(--accent-glow); color:var(--accent); }
.code-copy.copied { color:#10b981; }
.code-block pre { margin:0; border-radius:0; box-shadow:none; border:none; }
pre { background:var(--pre-bg); color:var(--pre-text); padding:18px 20px; overflow:auto; border-radius:var(--radius); box-shadow:var(--shadow); position:relative; }
pre code { background:none; padding:0; font-size:13px; line-height:1.65; font-weight:400; font-family:"Cascadia Code","Fira Code","JetBrains Mono",ui-monospace,SFMono-Regular,monospace; }
pre code.hljs { background:inherit !important; }

/* ---- Blockquote ---- */
blockquote { margin:20px 0; padding:14px 20px; color:var(--text2); border-left:3px solid; border-image:var(--accent-gradient) 1; background:var(--accent-glow); border-radius:0 var(--radius) var(--radius) 0; }

/* ---- Lists ---- */
ul,ol { padding-left:2em; }
li+li { margin-top:.3em; }
li { color:var(--text); }

/* ---- Table ---- */
table { border-collapse:collapse; width:100%; margin:20px 0; border-radius:var(--radius); overflow:hidden; box-shadow:var(--shadow); }
th,td { border:1px solid var(--border); padding:10px 14px; text-align:left; }
tr:nth-child(even) { background:var(--bg2); }
th { background:var(--bg3); font-weight:700; color:var(--text); font-size:13px; text-transform:uppercase; letter-spacing:.3px; }
hr { height:1px; padding:0; margin:36px 0; background:var(--border); border:0; }

/* ---- Mermaid diagrams ---- */
.mermaid-wrap { position:relative; margin:20px 0; }
.mermaid { background:var(--bg2); padding:24px; border-radius:var(--radius); overflow:auto; border:1px solid var(--border2); }
.diagram-expand { position:absolute; top:8px; right:8px; background:var(--bg); border:1px solid var(--border); border-radius:6px; width:32px; height:32px; display:flex; align-items:center; justify-content:center; cursor:pointer; color:var(--text3); font-size:16px; transition:all .2s; z-index:2; }
.diagram-expand:hover { background:var(--accent); color:#fff; border-color:var(--accent); box-shadow:0 0 0 3px var(--accent-glow); }
.diagram-fullscreen { position:fixed; inset:0; background:var(--bg); z-index:9998; display:flex; align-items:center; justify-content:center; padding:40px; }
.diagram-fullscreen .mermaid { max-width:95vw; max-height:90vh; overflow:auto; flex:1; }
.diagram-fullscreen .diagram-close { position:absolute; top:16px; right:16px; width:40px; height:40px; border-radius:50%; background:var(--bg3); border:none; font-size:20px; cursor:pointer; color:var(--text); display:flex; align-items:center; justify-content:center; }

/* ---- Index page ---- */
.index-page h1 { border:none; font-size:2.5em; margin-bottom:8px; -webkit-text-fill-color:unset; background:none; }
.index-page .tagline { color:var(--text2); font-size:1.1em; margin-bottom:32px; }
.index-page .index-preview { background:var(--bg2); border-left:3px solid; border-image:var(--accent-gradient) 1; padding:16px 20px; margin-bottom:32px; border-radius:0 var(--radius) var(--radius) 0; }
.index-page .index-section { margin-bottom:32px; }
.index-page .index-section h2 { border:none; font-size:1.3em; margin-bottom:12px; -webkit-text-fill-color:unset; background:none; }
.index-page .index-section ul { list-style:none; padding-left:0; }
.index-page .index-section li { padding:10px 0; border-bottom:1px solid var(--border2); transition:transform .15s; }
.index-page .index-section li:hover { transform:translateX(4px); }
.index-page .index-section li:last-child { border-bottom:none; }
.index-page .index-ask { background:var(--accent-glow); padding:24px; border-radius:var(--radius-lg); border:1px solid rgba(99,102,241,.15); }
.index-page .index-ask h2 { border:none; margin-top:0; }

/* ---- Source reference ---- */
.source-ref { display:inline-block; font-weight:700; color:var(--accent); cursor:pointer; background:var(--inline-code-bg); padding:0 5px; border-radius:4px; margin:0 2px; font-family:"Cascadia Code","Fira Code","JetBrains Mono",ui-monospace,SFMono-Regular,monospace; font-size:85%; transition:background .15s; }
.source-ref:hover { background:var(--accent-glow); outline:2px solid var(--accent-glow); outline-offset:0; }
/* Fallback styles for un-processed em tags */
em code { cursor:pointer; transition:background .15s; }
em code:hover { background:var(--accent-glow); }
em a[href] { color:var(--accent); text-decoration:underline dotted; text-underline-offset:3px; }
em a[href]:hover { color:var(--accent2); }
	/* 纯 <em> 来源回退的可点击样式 */
	em.source-em { cursor:pointer; color:var(--accent); font-weight:600; transition:all .15s; }
	em.source-em:hover { background:var(--accent-glow); border-radius:3px; padding:1px 4px; margin:-1px -4px; }
.s-popup-ov { display:none; position:fixed; inset:0; background:rgba(0,0,0,.55); z-index:9999; justify-content:center; align-items:center; }
.s-popup-ov.on { display:flex; }
.s-popup { background:var(--bg); border:1px solid var(--border); border-radius:var(--radius-lg); max-width:800px; width:90vw; max-height:80vh; overflow:hidden; box-shadow:0 20px 60px rgba(0,0,0,.3); display:flex; flex-direction:column; }
.s-popup-hd { display:flex; align-items:center; justify-content:space-between; padding:12px 18px; background:var(--bg2); border-bottom:1px solid var(--border); font-size:13px; font-weight:600; color:var(--accent); }
.s-popup-cl { background:none; border:none; font-size:20px; cursor:pointer; color:var(--text3); padding:4px 8px; line-height:1; }
.s-popup-cl:hover { color:var(--text); }
.s-popup-bd { flex:1; overflow:auto; }
	.s-popup-bd pre { margin:0; border-radius:0; box-shadow:none; border:none; padding:16px 20px; background:var(--pre-bg); color:var(--pre-text); font-size:13px; line-height:1.6; white-space:pre-wrap; }
	/* 语法高亮回退（CDN 不可用时） */
	.s-popup-bd .hljs-keyword,.s-popup-bd .hljs-selector-tag { color:#ff7b72; }
	.s-popup-bd .hljs-string,.s-popup-bd .hljs-addition { color:#a5d6ff; }
	.s-popup-bd .hljs-number { color:#79c0ff; }
	.s-popup-bd .hljs-comment { color:#8b949e; font-style:italic; }
	.s-popup-bd .hljs-function,.s-popup-bd .hljs-title,.s-popup-bd .hljs-section { color:#d2a8ff; }
	.s-popup-bd .hljs-type,.s-popup-bd .hljs-built_in { color:#ffa657; }
	.s-popup-bd .hljs-attr { color:#79c0ff; }
	.s-popup-bd .hljs-params { color:#e6edf3; }
	.s-popup-bd .hljs-meta { color:#8b949e; }
	.s-popup-bd .hljs-literal { color:#79c0ff; }

/* ---- Animations ---- */
@keyframes fadeUp { from{opacity:0;transform:translateY(12px)} to{opacity:1;transform:translateY(0)} }
.content h1,.content h2,.content h3 { animation:fadeUp .4s ease-out; }
@keyframes pulse { 0%,100%{opacity:1} 50%{opacity:.5} }

	/* ---- Chapter grid & cards ---- */
	.chapter-grid { display:grid; grid-template-columns:repeat(auto-fill,minmax(280px,1fr)); gap:16px; margin:20px 0; }
	.chapter-card { display:flex; flex-direction:column; gap:6px; padding:20px; border-radius:var(--radius-lg); border:1px solid var(--border); background:var(--bg); text-decoration:none; color:var(--text); transition:all .2s; }
	.chapter-card:hover { border-color:var(--accent); box-shadow:0 4px 16px var(--accent-glow); transform:translateY(-2px); }
	.chapter-card-title { font-size:1.05em; font-weight:700; color:var(--text); }
	.chapter-card-subtitle { font-size:0.9em; color:var(--text2); line-height:1.4; }
	.chapter-card-meta { display:flex; align-items:center; gap:10px; margin-top:4px; font-size:12px; color:var(--text3); }
	.chapter-card-meta span:first-child { padding:2px 8px; border-radius:12px; background:var(--accent-glow); color:var(--accent); font-weight:600; }
	/* ---- Chapter page header ---- */
	.chapter-header { margin-bottom:32px; padding-bottom:24px; border-bottom:2px solid var(--border); }
	.chapter-header h1 { border:none; -webkit-text-fill-color:unset; background:var(--accent-gradient); -webkit-background-clip:text; background-clip:text; margin-bottom:8px; }
	.chapter-subtitle { font-size:1.1em; color:var(--text2); margin:8px 0; }
	.chapter-meta { display:flex; align-items:center; gap:12px; margin-top:12px; }
	.chapter-diff { display:inline-block; padding:3px 10px; border-radius:20px; font-size:12px; font-weight:600; background:var(--accent-glow); color:var(--accent); }
	.chapter-count { font-size:12px; color:var(--text3); }
	.chapter-intro { margin:20px 0 28px; padding:16px 20px; background:var(--accent-glow); border-radius:var(--radius-lg); border-left:3px solid var(--accent); }
	.chapter-intro p { margin:0; font-size:0.95em; color:var(--text2); line-height:1.7; }

	/* ---- Nav group label ---- */
	.nav-group-label { font-size:11px; font-weight:700; color:var(--text3); text-transform:uppercase; letter-spacing:.8px; }

	/* ---- Section animation ---- */
	@keyframes fadeUp2 { from{opacity:0;transform:translateY(12px)} to{opacity:1;transform:translateY(0)} }
	section { animation:fadeUp2 .4s ease-out; }
`

// wikiPageJS is the shared JavaScript for all wiki pages (serve mode).
const wikiPageJS = `
<script>
/* ---- Theme ---- */
(function(){
  var theme=localStorage.getItem('codewiki-theme')||'light';
  document.documentElement.setAttribute('data-theme',theme);
  document.addEventListener('DOMContentLoaded',function(){
    var btn=document.getElementById('theme-toggle');
    if(!btn)return;
    btn.textContent=theme==='dark'?'☀️':'🌙';
    btn.addEventListener('click',function(){
      var t=document.documentElement.dataset.theme==='dark'?'light':'dark';
      document.documentElement.setAttribute('data-theme',t);
      localStorage.setItem('codewiki-theme',t);
      btn.textContent=t==='dark'?'☀️':'🌙';
    });
  });
})();

/* ---- Copy code ---- */
function copyCode(btn){
  var pre=btn.closest('.code-block').querySelector('pre code');
  if(!pre)return;
  navigator.clipboard.writeText(pre.textContent).then(function(){
    btn.classList.add('copied');
    btn.querySelector('span').textContent='✓ 已复制';
    setTimeout(function(){btn.classList.remove('copied');btn.querySelector('span').textContent='复制';},2000);
  });
}

/* ---- Diagram fullscreen ---- */
function expandDiagram(btn){
  var wrap=btn.closest('.mermaid-wrap');
  var mermaidEl=wrap.querySelector('.mermaid');
  var overlay=document.createElement('div');
  overlay.className='diagram-fullscreen';
  overlay.innerHTML='<button class="diagram-close" title="关闭">✕</button>';
  var clone=mermaidEl.cloneNode(true);
  overlay.insertBefore(clone,overlay.firstChild);
  document.body.appendChild(overlay);
  overlay.querySelector('.diagram-close').addEventListener('click',function(){overlay.remove();});
  overlay.addEventListener('click',function(e){if(e.target===overlay)overlay.remove();});
  document.addEventListener('keydown',function esc(e){if(e.key==='Escape'){overlay.remove();document.removeEventListener('keydown',esc);}});
}

/* ---- Collapsible nav groups ---- */
document.addEventListener('DOMContentLoaded',function(){
  document.querySelectorAll('.nav-group-header').forEach(function(h){
    h.addEventListener('click',function(){
      this.parentElement.classList.toggle('collapsed');
    });
  });
});

/* ---- Reading progress bar ---- */
window.addEventListener('scroll',function(){
  var bar=document.getElementById('reading-progress');
  if(!bar)return;
  var h=document.documentElement.scrollHeight-window.innerHeight;
  bar.style.width=h>0?((window.scrollY/h)*100)+'%':'0';
});

/* ---- Right panel logic ---- */
function togglePanel(tab){
  var p=document.getElementById('right-panel');
  if(!p)return;
  if(p.classList.contains('open')){
    if(tab){
      var cur=p.querySelector('.rp-tab.active');
      if(cur&&cur.dataset.tab===tab){closePanel();return;}
      switchTab(tab);
    } else {closePanel();}
  } else {
    p.classList.add('open');
    document.body.classList.add('rp-open');
    if(tab)switchTab(tab);
    var inp=p.querySelector('.rp-pane.active input');
    if(inp)setTimeout(function(){inp.focus();},100);
  }
}
function closePanel(){
  var p=document.getElementById('right-panel');
  if(p){p.classList.remove('open');document.body.classList.remove('rp-open');}
}
function switchTab(tab){
  var p=document.getElementById('right-panel');
  if(!p)return;
  p.querySelectorAll('.rp-tab').forEach(function(t){t.classList.toggle('active',t.dataset.tab===tab);});
  p.querySelectorAll('.rp-pane').forEach(function(pn){pn.classList.toggle('active',pn.dataset.pane===tab);});
  var inp=p.querySelector('.rp-pane.active input');
  if(inp)setTimeout(function(){inp.focus();},50);
}
function rpFilterSearch(q){
  var r=document.getElementById('rp-search-results');
  if(!r)return;
  q=q.toLowerCase();
  if(!q){r.innerHTML='';return;}
  var html='';
  if(typeof _navIdx!=='undefined'){
    _navIdx.forEach(function(n){
      if(n.t.toLowerCase().indexOf(q)>=0||n.f.toLowerCase().indexOf(q)>=0){
        html+='<a class="rp-result" href="'+n.f+'"><span class="rp-result-title">'+n.t+'</span><span class="rp-result-path">'+n.f+'</span></a>';
      }
    });
  }
  r.innerHTML=html||'<div class="rp-empty">未找到匹配结果</div>';
}
var _rpHistory=[];
function rpAskSend(){
  var inp=document.getElementById('rp-ai-input');
  var btn=document.getElementById('rp-ai-btn');
  var chat=document.getElementById('rp-chat');
  if(!inp||!btn||!chat)return;
  var q=inp.value.trim();
  if(!q)return;
  var welcome=chat.querySelector('.rp-welcome');
  if(welcome)welcome.remove();
  rpAppendMsg('user',q);
  inp.value='';
  btn.disabled=true;
  var fullText='';
  var div=document.createElement('div');
  div.className='rp-msg assistant';
  var bubble=document.createElement('div');
  bubble.className='rp-bubble';
  bubble.innerHTML='<span class="rp-loading" style="display:inline-flex"><span class="rp-loading-dot"></span><span class="rp-loading-dot"></span><span class="rp-loading-dot"></span></span>';
  div.appendChild(bubble);
  chat.appendChild(div);
  chat.scrollTop=chat.scrollHeight;
  fetch('/api/ask',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({question:q,history:_rpHistory,stream:true})})
  .then(function(res){
    if(!res.ok){res.json().then(function(e){bubble.textContent='错误：'+(e.error||'请求失败');});throw new Error('done');}
    var reader=res.body.getReader();
    var decoder=new TextDecoder();
    var buf='';
    function pump(){
      return reader.read().then(function(r){
        if(r.done)return;
        buf+=decoder.decode(r.value,{stream:true});
        var lines=buf.split('\n');
        buf=lines.pop()||'';
        lines.forEach(function(line){
          if(!line.startsWith('data: '))return;
          try{var d=JSON.parse(line.slice(6));}catch(e){return;}
          if(d.type==='token'){
            fullText+=d.text;
            bubble.innerHTML=rpRenderMd(fullText);
          }else if(d.type==='done'){
            if(d.sources&&d.sources.length>0){
              var sd=document.createElement('div');
              sd.className='rp-sources';
              d.sources.forEach(function(s){
                var tag=document.createElement('button');
                tag.className='rp-src-tag';
                tag.textContent=s.Filename+(s.StartLine>0?':'+s.StartLine:'');
                tag.title=s.Type+'：'+s.Name;
                tag.onclick=function(){window.openSource(s.Filename);};
                sd.appendChild(tag);
              });
              div.appendChild(sd);
            }
            _rpHistory.push({question:q,answer:fullText});
          }
        });
        chat.scrollTop=chat.scrollHeight;
        return pump();
      });
    }
    return pump();
  })
  .catch(function(e){if(e.message!=='done')bubble.textContent='错误：'+e.message;})
  .finally(function(){btn.disabled=false;inp.focus();});
}
function rpNavToArticle(filename){
  var parts=filename.split(/[\/\\]/);
  // Try to find matching nav item by scanning sidebar and nav-group links
  var searchTerms=[filename.replace(/\\/g,'/')]; // full path
  if(parts.length>=2)searchTerms.push(parts.slice(-2).join('/')); // last 2 path segments
  if(parts.length>=1)searchTerms.push(parts[parts.length-1].replace(/\.[^.]+$/,'')); // basename
  var best=null,bestScore=0;
  document.querySelectorAll('.sidebar a[href], .nav-group-items a').forEach(function(a){
    var href=(a.getAttribute('href')||'').toLowerCase();
    var text=a.textContent.toLowerCase();
    var score=0;
    searchTerms.forEach(function(t){
      if(t&&href.indexOf(t.toLowerCase())>=0)score+=t.length*2;
      if(t&&text.indexOf(t.toLowerCase())>=0)score+=t.length;
    });
    if(score>bestScore){bestScore=score;best=a;}
  });
  if(best){
    var href=best.getAttribute('href');
    if(href&&href.startsWith('#')){
      window.location.hash=href;
      var el=document.querySelector(href);
      if(el)window.scrollTo({top:el.offsetTop-70,behavior:'smooth'});
    }else if(href){
      window.location.href=href;
    }
  }else if(typeof openSource==='function'){
    openSource(filename);
  }else{
    window.open('/api/source?file='+encodeURIComponent(filename),'_blank');
  }
}
function rpRenderMd(t){
  t=t.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
  t=t.replace(/^---$/gm,'<hr>');
  t=t.replace(/^### (.+)$/gm,'<h4 style="margin:8px 0 4px;font-size:13px">$1</h4>');
  t=t.replace(/^## (.+)$/gm,'<h3 style="margin:10px 0 4px;font-size:14px">$1</h3>');
  t=t.replace(/^# (.+)$/gm,'<h3 style="margin:10px 0 4px;font-size:15px">$1</h3>');
  t=t.replace(/\x60([^\x60]+)\x60/g,'<code style="background:var(--inline-code-bg);padding:1px 4px;border-radius:3px;font-size:12px">$1</code>');
  t=t.replace(/\*\*([^*]+)\*\*/g,'<strong>$1</strong>');
  t=t.replace(/^- (.+)$/gm,'<div style="padding-left:12px">• $1</div>');
  t=t.replace(/\n\n/g,'</p><p style="margin:6px 0">');
  t=t.replace(/\n/g,'<br>');
  return '<p style="margin:6px 0">'+t+'</p>';
}
function rpAppendMsg(role,text,sources){
  var chat=document.getElementById('rp-chat');
  if(!chat)return;
  var div=document.createElement('div');
  div.className='rp-msg '+role;
  var bubble=document.createElement('div');
  bubble.className='rp-bubble';
  if(role==='assistant'){bubble.innerHTML=rpRenderMd(text);}else{bubble.textContent=text;}
  div.appendChild(bubble);
  if(sources&&sources.length>0){
    var sd=document.createElement('div');
    sd.className='rp-sources';
    sources.forEach(function(s){
      var tag=document.createElement('button');
      tag.className='rp-src-tag';
      tag.textContent=s.Filename+(s.StartLine>0?':'+s.StartLine:'');
      tag.title=s.Type+'：'+s.Name;
      tag.onclick=function(){window.openSource(s.Filename);};
      sd.appendChild(tag);
    });
    div.appendChild(sd);
  }
  chat.appendChild(div);
  chat.scrollTop=chat.scrollHeight;
}
document.addEventListener('keydown',function(e){
  if((e.ctrlKey||e.metaKey)&&e.key==='k'){e.preventDefault();togglePanel('search');}
  if(e.key==='Escape')closePanel();
});

	/* ---- Scroll spy for nav ---- */
	document.addEventListener('DOMContentLoaded',function(){
	  var links=document.querySelectorAll('.nav-group-items a');
	  if(!links.length)return;
	  function updateActive(){
	    var best=null;
	    var bestDist=Infinity;
	    var scrollY=window.scrollY+120;
	    links.forEach(function(a){
	      var href=a.getAttribute('href');
	      if(!href||href.charAt(0)!=='#')return;
	      var el=document.getElementById(href.slice(1));
	      if(!el)return;
	      var top=el.getBoundingClientRect().top+window.scrollY;
	      var dist=Math.abs(scrollY-top);
	      if(dist<bestDist){bestDist=dist;best=a;}
	    });
	    links.forEach(function(a){a.classList.remove('active');});
	    if(best)best.classList.add('active');
	  }
	  window.addEventListener('scroll',updateActive);
	  updateActive();
	});

/* ---- Mermaid click navigation ---- */
window.navigateToModule=function(mod){
  var sec=document.getElementById(mod.replace(/\//g,'_'));
  if(sec){sec.scrollIntoView({behavior:'smooth',block:'start'});return;}
  var a=document.querySelector('a[href*="'+mod+'"]');
  if(a)a.click();
};

/* ---- Code highlighting ---- */
document.addEventListener('DOMContentLoaded',function(){if(typeof hljs!=='undefined')hljs.highlightAll();});
</script>
`

// SourcePopupJS is the source-reference popup script shared by all page types.
// It provides openSource(), the popup DOM, and the click handler for .source-ref
// spans and legacy em-tag fallback.
const SourcePopupJS = `<style>
/* 纯 <em> 来源回退的可点击样式（注入旧页面时生效） */
em.source-em { cursor:pointer; color:var(--accent); font-weight:600; transition:all .15s; }
em.source-em:hover { background:var(--accent-glow); border-radius:3px; padding:1px 4px; margin:-1px -4px; }
/* 语法高亮回退（CDN 不可用时弹窗内生效） */
.s-popup-bd .hljs-keyword,.s-popup-bd .hljs-selector-tag { color:#ff7b72; }
.s-popup-bd .hljs-string,.s-popup-bd .hljs-addition { color:#a5d6ff; }
.s-popup-bd .hljs-number { color:#79c0ff; }
.s-popup-bd .hljs-comment { color:#8b949e; font-style:italic; }
.s-popup-bd .hljs-function,.s-popup-bd .hljs-title,.s-popup-bd .hljs-section { color:#d2a8ff; }
.s-popup-bd .hljs-type,.s-popup-bd .hljs-built_in { color:#ffa657; }
.s-popup-bd .hljs-attr { color:#79c0ff; }
.s-popup-bd .hljs-params { color:#e6edf3; }
.s-popup-bd .hljs-meta { color:#8b949e; }
.s-popup-bd .hljs-literal { color:#79c0ff; }
</style>
<script>
(function(){
var popup=null;
function getPopup(){
if(popup)return popup;
popup=document.createElement('div');popup.id='s-popup';popup.className='s-popup-ov';
popup.innerHTML='<div class="s-popup"><div class="s-popup-hd"><span></span><button class="s-popup-cl">&times;</button></div><div class="s-popup-bd"><pre></pre></div></div>';
document.body.appendChild(popup);
popup.querySelector('.s-popup-cl').onclick=function(){popup.classList.remove('on');};
popup.addEventListener('click',function(e){if(e.target===popup)popup.classList.remove('on');});
return popup;}
function openSource(file){
var p=getPopup();p.classList.add('on');
p.querySelector('.s-popup-hd span').textContent=file;
var bd=p.querySelector('.s-popup-bd');
bd.innerHTML='<pre><code class="language-'+l(file)+'">加载中...</code></pre>';
fetch('/api/source?file='+encodeURIComponent(file))
.then(function(r){if(!r.ok)throw Error(r.status);return r.text();})
.then(function(t){var c=bd.querySelector('code');c.textContent=t;if(typeof hljs!=='undefined'){try{hljs.highlightElement(c);}catch(e){}}})
.catch(function(e){bd.querySelector('code').textContent='无法加载: '+e.message;});}
function l(p){var m=p.match(/\.(\w+)$/);if(!m)return'plaintext';
var x=m[1].toLowerCase();var m2={py:'python',go:'go',js:'javascript',ts:'typescript',
rs:'rust',java:'java',cpp:'cpp',c:'c',rb:'ruby',md:'markdown',json:'json',yaml:'yaml',
css:'css',html:'html',xml:'xml',sql:'sql',sh:'bash'};return m2[x]||x;}
// 	// 将旧 <em>来源：file1, file2, ...</em> 拆为独立 .source-ref span
	function upgradeSourceEms(){
	document.querySelectorAll('em').forEach(function(em){
	var txt=em.textContent;
	if(txt.indexOf('来源')!==0)return;
	var content=txt.replace(/^来源：?/,'').trim();
	if(!content)return;
	// 拆分多路径（逗号、顿号分隔）
	var parts=content.split(/[,、，]/);
	var files=[];
	parts.forEach(function(p){
	p=p.trim().replace(/^\[|\]$/g,'').replace(/\/$/,'');
	if(p)files.push(p);
	});
	if(files.length===0)return;
	// 构建独立 .source-ref span
	var html='来源：';
	files.forEach(function(f,i){
	var disp=f.replace(/\\/g,'/').split('/').pop();
	html+='<span class="source-ref" data-file="'+f+'"><strong>'+disp+'</strong></span>';
	if(i<files.length-1)html+='、';
	});
	em.innerHTML=html;
	});
	}
	if(document.readyState!=='loading'){upgradeSourceEms();}
	else{document.addEventListener('DOMContentLoaded',upgradeSourceEms);}
document.addEventListener('click',function(e){
var s=e.target.closest('.source-ref');
if(s&&s.dataset.file){e.preventDefault();openSource(s.dataset.file);return;}
var a=e.target.closest('em a[href]');
if(a&&a.closest('em')&&a.closest('em').textContent.indexOf('来源')===0){
e.preventDefault();openSource(a.getAttribute('href'));return;}
var c=e.target.closest('em code');
if(c&&c.closest('em')&&c.closest('em').textContent.indexOf('来源')===0){
e.preventDefault();openSource(c.textContent.replace(/^\[|\]$/g,'').replace(/\/$/,''));return;}
var em=e.target.closest('em');
if(em&&em.textContent.indexOf('来源')===0){
var t=em.textContent.replace(/^来源：?/,'');
if(t){e.preventDefault();openSource(t.replace(/^\[|\]$/g,'').replace(/\/$/,'').split(/[,、，]/)[0].trim());}}
});
window.openSource=openSource;})();
</script>`

// BuildWikiPage assembles a full HTML page with sidebar navigation from structured sections.
func BuildWikiPage(title, body, currentURL string, sections []NavSection, totalArticles, totalMinutes int) []byte {
	var out strings.Builder
	out.WriteString(`<!DOCTYPE html>
<html lang="zh-CN" data-theme="light">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>`)
	out.WriteString(HTMLEscape(title))
	out.WriteString(`</title>
<style>`)
	out.WriteString(wikiPageCSS)
	out.WriteString(`</style>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/gh/highlightjs/cdn-release@11.9.0/build/styles/github-dark.min.css">
<script src="https://cdn.jsdelivr.net/gh/highlightjs/cdn-release@11.9.0/build/highlight.min.js"></script>
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

	if len(sections) > 0 {
		// Top bar
		out.WriteString(`<div class="topbar">
<div class="topbar-title">`)
		out.WriteString(HTMLEscape(title))
		out.WriteString(`</div>
<div class="topbar-search">
<svg class="search-icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.35-4.35"/></svg>
<input type="text" placeholder="` + "搜索文章、模块..." + `" readonly onclick="togglePanel('search')">
<kbd>Ctrl+K</kbd>
	<button onclick="togglePanel('ai')" class="topbar-btn primary" style="margin-left:4px"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z"/></svg>Ask AI</button>
</div>
<div class="topbar-actions">
<button id="theme-toggle" class="theme-toggle" title="` + "切换主题" + `"></button>
</div>
</div>
`)

		// Right panel (Search + AI Q&A) — build nav index for search tab
		out.WriteString(`<div class="right-panel" id="right-panel">
<div class="rp-header">
<button class="rp-tab active" data-tab="search" onclick="switchTab('search')">` + "\U0001F50D 搜索" + `</button>
<button class="rp-tab" data-tab="ai" onclick="switchTab('ai')">` + "\U0001F916 AI 问答" + `</button>
<button class="rp-close" onclick="closePanel()" title="关闭">✕</button>
</div>
<div class="rp-body">
<div class="rp-pane rp-search-pane active" data-pane="search">
<input class="rp-search-input" type="text" id="rp-search-input" placeholder="` + "搜索文章、模块..." + `" oninput="rpFilterSearch(this.value)">
<div class="rp-results" id="rp-search-results"></div>
</div>
<div class="rp-pane rp-ai-pane" data-pane="ai">
<div class="rp-chat" id="rp-chat">
<div class="rp-welcome">
<div class="rp-welcome-icon">` + "\U0001F916" + `</div>
<div class="rp-welcome-title">` + "CodeWiki AI 助手" + `</div>
<div class="rp-welcome-desc">` + "基于项目代码库的 RAG 智能问答<br>输入问题开始对话" + `</div>
</div>
</div>
<div class="rp-input-area">
<input type="text" id="rp-ai-input" placeholder="` + "向代码库提问..." + `" onkeydown="if(event.key==='Enter')rpAskSend()">
<button id="rp-ai-btn" onclick="rpAskSend()">` + "发送" + `</button>
</div>
</div>
</div>
</div>
<script>
var _navIdx=[`)
		first := true
		for _, sec := range sections {
			for _, item := range sec.Items {
				if !first {
					out.WriteByte(',')
				}
				first = false
				out.WriteString(fmt.Sprintf(`{f:"%s",t:"%s"}`, item.URL, item.Title))
			}
		}
		out.WriteString(`];
</script>
`)

		// Sidebar
		out.WriteString(`<nav class="sidebar">
<div class="sidebar-header"><span class="logo-dot"></span><a href="/" style="color:inherit;text-decoration:none;font-weight:700;">CodeWiki</a></div>
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
				if item.URL == currentURL {
					activeClass = ` class="active"`
				}
				out.WriteString(fmt.Sprintf(`<li><a href="%s"%s><span class="nav-icon">%s</span><span class="nav-title">%s</span>`, item.URL, activeClass, item.Icon, HTMLEscape(item.Title)))
				if item.ReadingTime > 0 || item.Difficulty != "" {
					out.WriteString(`<span class="nav-meta">`)
					if item.ReadingTime > 0 {
						out.WriteString(fmt.Sprintf(`<span class="nav-time">⏱ %dmin</span>`, item.ReadingTime))
					}
					if item.Difficulty != "" {
						out.WriteString(fmt.Sprintf(`<span class="nav-diff">%s</span>`, item.Difficulty))
					}
					out.WriteString(`</span>`)
				}
				out.WriteString("</a></li>\n")
			}
			out.WriteString("</ul>\n</div>\n")
		}
		out.WriteString("</nav>\n")
	} else {
		// No sidebar - still add topbar with theme toggle
		out.WriteString(`<div class="topbar" style="left:0">
<div class="topbar-title">`)
		out.WriteString(HTMLEscape(title))
		out.WriteString(`</div>
<div class="topbar-actions" style="margin-left:auto;">
<button id="theme-toggle" class="theme-toggle" title="` + "切换主题" + `"></button>
</div>
</div>
`)
	}

	contentStyle := ""
	if len(sections) == 0 {
		contentStyle = ` style="margin-left:0;max-width:800px;margin:0 auto;"`
	}
	out.WriteString(fmt.Sprintf(`<div class="content"%s>
`, contentStyle))
	out.WriteString(body)
	out.WriteString(`</div>
`)
	out.WriteString(SourcePopupJS)
	out.WriteString(`
</body>
</html>
`)
	return []byte(out.String())
}


// MarkdownToHTML converts basic Markdown to a complete HTML page.
func MarkdownToHTML(src []byte) []byte {
	return BuildWikiPage("CodeWiki", RenderMarkdownBody(src), "", nil, 0, 0)
}

// RenderInline renders inline Markdown formatting (bold, italic, links, code).
func RenderInline(s string) string {
	s = HTMLEscape(s)
	// 还原 <br> 标签（LLM 常用于表格单元格内换行）
	s = strings.ReplaceAll(s, "&lt;br&gt;", "<br>")
	s = strings.ReplaceAll(s, "&lt;br/&gt;", "<br>")
	// Links: [text](url)
	for {
		start := strings.Index(s, "[")
		if start == -1 {
			break
		}
		mid := strings.Index(s[start:], "](")
		if mid == -1 {
			break
		}
		mid += start
		end := strings.Index(s[mid:], ")")
		if end == -1 {
			break
		}
		end += mid
		text := s[start+1 : mid]
		url := s[mid+2 : end]
		s = s[:start] + `<a href="` + url + `">` + text + `</a>` + s[end+1:]
	}
	// Bold: **text**
	for {
		start := strings.Index(s, "**")
		if start == -1 {
			break
		}
		end := strings.Index(s[start+2:], "**")
		if end == -1 {
			break
		}
		end += start + 2
		s = s[:start] + "<strong>" + s[start+2:end] + "</strong>" + s[end+2:]
	}
	// Italic: *text* (but not in already processed tags)
	var result strings.Builder
	inTag := false
	for i := 0; i < len(s); i++ {
		if s[i] == '<' {
			inTag = true
			result.WriteByte(s[i])
			continue
		}
		if s[i] == '>' {
			inTag = false
			result.WriteByte(s[i])
			continue
		}
		if !inTag && s[i] == '*' && i+1 < len(s) && s[i+1] != '*' && s[i+1] != ' ' {
			end := strings.Index(s[i+1:], "*")
			if end != -1 {
				result.WriteString("<em>")
				result.WriteString(s[i+1 : i+1+end])
				result.WriteString("</em>")
				i += end + 1
				continue
			}
		}
		result.WriteByte(s[i])
	}
	s = result.String()
	// Inline code: `text`
	for {
		start := strings.Index(s, "`")
		if start == -1 {
			break
		}
		end := strings.Index(s[start+1:], "`")
		if end == -1 {
			break
		}
		end += start + 1
		s = s[:start] + "<code>" + s[start+1:end] + "</code>" + s[end+1:]
	}
	return s
}

// HTMLEscape escapes HTML special characters.
func HTMLEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}


func orderedListMatch(s string) bool {
	for i, c := range s {
		if c >= '0' && c <= '9' {
			continue
		}
		if i > 0 && c == '.' && i+1 < len(s) && s[i+1] == ' ' {
			return true
		}
		return false
	}
	return false
}

func orderedListItem(s string) string {
	for i, c := range s {
		if c >= '0' && c <= '9' {
			continue
		}
		if i > 0 && c == '.' && i+1 < len(s) && s[i+1] == ' ' {
			return s[i+2:]
		}
		return s
	}
	return s
}

func splitTableCells(row string) []string {
	row = strings.TrimSpace(row)
	row = strings.TrimPrefix(row, "|")
	row = strings.TrimSuffix(row, "|")
	return strings.Split(row, "|")
}

func isTableSeparator(row string) bool {
	row = strings.TrimSpace(row)
	if !strings.HasPrefix(row, "|") {
		return false
	}
	cells := splitTableCells(row)
	for _, c := range cells {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		ok := true
		for _, ch := range c {
			if ch != '-' && ch != '|' && ch != ' ' && ch != ':' {
				ok = false
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}
