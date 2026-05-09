package docgen

import (
	"fmt"
	"path/filepath"
	"strings"
)

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
			body.WriteString("<div class=\"mermaid\">\n")
			for _, cl := range codeLines {
				body.WriteString(cl)
				body.WriteByte('\n')
			}
			body.WriteString("</div>\n")
		} else {
			body.WriteString("<pre><code>")
			for _, cl := range codeLines {
				body.WriteString(htmlEscape(cl))
				body.WriteByte('\n')
			}
			body.WriteString("</code></pre>\n")
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
			body.WriteString("<h1>")
			body.WriteString(RenderInline(strings.TrimPrefix(trimmed, "# ")))
			body.WriteString("</h1>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			body.WriteString("<h2>")
			body.WriteString(RenderInline(strings.TrimPrefix(trimmed, "## ")))
			body.WriteString("</h2>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "### ") {
			body.WriteString("<h3>")
			body.WriteString(RenderInline(strings.TrimPrefix(trimmed, "### ")))
			body.WriteString("</h3>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "#### ") {
			body.WriteString("<h4>")
			body.WriteString(RenderInline(strings.TrimPrefix(trimmed, "#### ")))
			body.WriteString("</h4>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "##### ") {
			body.WriteString("<h5>")
			body.WriteString(RenderInline(strings.TrimPrefix(trimmed, "##### ")))
			body.WriteString("</h5>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "###### ") {
			body.WriteString("<h6>")
			body.WriteString(RenderInline(strings.TrimPrefix(trimmed, "###### ")))
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

// BuildWikiPage assembles a full HTML page with optional sidebar navigation.
func BuildWikiPage(title, body string, navItems []string, current string) []byte {
	var out strings.Builder
	out.WriteString(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>`)
	out.WriteString(htmlEscape(title))
	out.WriteString(`</title>
<style>
* { box-sizing: border-box; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; margin: 0; line-height: 1.6; color: #24292f; background: #ffffff; display: flex; }
.sidebar { width: 260px; min-width: 260px; background: #f6f8fa; border-right: 1px solid #d0d7de; height: 100vh; position: fixed; overflow-y: auto; }
.sidebar-header { padding: 16px; font-weight: 600; font-size: 16px; border-bottom: 1px solid #d0d7de; background: #ffffff; }
.sidebar ul { list-style: none; padding: 0; margin: 0; }
.sidebar li a { display: block; padding: 8px 16px; color: #24292f; text-decoration: none; font-size: 14px; border-bottom: 1px solid #eaeef2; }
.sidebar li a:hover { background: #eaeef2; }
.sidebar li a.active { background: #0969da; color: white; }
.sidebar .nav-section { padding: 8px 16px; font-size: 11px; font-weight: 600; color: #57606a; text-transform: uppercase; letter-spacing: 0.05em; background: #ffffff; border-bottom: 1px solid #eaeef2; }
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
hr { height: .25em; padding: 0; margin: 24px 0; background: #d0d7de; border: 0; }
.mermaid { background: #f6f8fa; padding: 16px; border-radius: 6px; overflow: auto; }
/* Index page styles */
.index-page h1 { border: none; font-size: 2.5em; margin-bottom: 8px; }
.index-page .tagline { color: #57606a; font-size: 1.1em; margin-bottom: 32px; }
.index-page .index-preview { background: #f6f8fa; border-left: 4px solid #0969da; padding: 16px 20px; margin-bottom: 32px; border-radius: 0 6px 6px 0; }
.index-page .index-section { margin-bottom: 32px; }
.index-page .index-section h2 { border: none; font-size: 1.3em; margin-bottom: 12px; }
.index-page .index-section ul { list-style: none; padding-left: 0; }
.index-page .index-section li { padding: 8px 0; border-bottom: 1px solid #eaeef2; }
.index-page .index-section li:last-child { border-bottom: none; }
.index-page .index-ask { background: #ddf4ff; padding: 20px; border-radius: 6px; }
.index-page .index-ask h2 { border: none; margin-top: 0; }
</style>
<script type="module">
  import mermaid from 'https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.esm.min.mjs';
  mermaid.initialize({ startOnLoad: true, securityLevel: 'loose' });
  window.navigateToModule = function(name) {
    var safe = name.replace(/[\/\\\\:]/g, '_');
    window.location.href = 'modules/' + safe + '.md';
  };
</script>
</head>
<body>
`)

	if len(navItems) > 0 {
		out.WriteString(`<nav class="sidebar">
<div class="sidebar-header"><a href="/" style="color:inherit;text-decoration:none;">CodeWiki</a></div>
`)
		var learningItems, refItems, diagramItems []string
		for _, item := range navItems {
			base := strings.ToLower(item)
			switch {
			case strings.HasPrefix(base, "0") || base == "learning-path.md":
				learningItems = append(learningItems, item)
			case base == "api-reference.md":
				refItems = append(refItems, item)
			case strings.HasSuffix(base, ".mmd"):
				diagramItems = append(diagramItems, item)
			default:
				refItems = append(refItems, item)
			}
		}
		if len(learningItems) > 0 {
			out.WriteString(`<div class="nav-section">学习指南</div>
<ul>
`)
			for _, item := range learningItems {
				activeClass := ""
				if item == current {
					activeClass = ` class="active"`
				}
				display := navDisplayName(item)
				out.WriteString(fmt.Sprintf(`<li><a href="%s"%s>%s</a></li>
`, item, activeClass, display))
			}
			out.WriteString(`</ul>
`)
		}
		if len(refItems) > 0 {
			out.WriteString(`<div class="nav-section">参考手册</div>
<ul>
`)
			for _, item := range refItems {
				activeClass := ""
				if item == current {
					activeClass = ` class="active"`
				}
				display := navDisplayName(item)
				out.WriteString(fmt.Sprintf(`<li><a href="%s"%s>%s</a></li>
`, item, activeClass, display))
			}
			out.WriteString(`</ul>
`)
		}
		if len(diagramItems) > 0 {
			out.WriteString(`<div class="nav-section">图表</div>
<ul>
`)
			for _, item := range diagramItems {
				activeClass := ""
				if item == current {
					activeClass = ` class="active"`
				}
				display := navDisplayName(item)
				out.WriteString(fmt.Sprintf(`<li><a href="%s"%s>%s</a></li>
`, item, activeClass, display))
			}
			out.WriteString(`</ul>
`)
		}
		out.WriteString(`</nav>
`)
	}

	out.WriteString(`<div class="content">
`)
	out.WriteString(body)
	out.WriteString(`</div>
</body>
</html>
`)
	return []byte(out.String())
}

// navDisplayName converts a filename to a human-readable navigation label.
func navDisplayName(filename string) string {
	base := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	switch base {
	case "00-overview":
		return "项目概述"
	case "01-what-it-does":
		return "项目能做什么"
	case "02-architecture":
		return "架构说明"
	case "03-project-structure":
		return "项目结构"
	case "04-key-concepts":
		return "核心概念"
	case "05-learning-path":
		return "学习路径"
	case "api-reference":
		return "API 参考"
	case "architecture":
		return "架构图"
	case "class-diagram":
		return "类图"
	case "sequence-diagram":
		return "时序图"
	case "compilation":
		return "文档合辑"
	default:
		return base
	}
}

// MarkdownToHTML converts basic Markdown to a complete HTML page.
func MarkdownToHTML(src []byte) []byte {
	return BuildWikiPage("CodeWiki", RenderMarkdownBody(src), nil, "")
}

// RenderInline renders inline Markdown formatting (bold, italic, links, code).
func RenderInline(s string) string {
	s = htmlEscape(s)
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

func htmlEscape(s string) string {
	return HTMLEscape(s)
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
