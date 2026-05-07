package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/splitsword/fine-codewiki/internal/analyzer"
	"github.com/splitsword/fine-codewiki/internal/chunker"
	"github.com/splitsword/fine-codewiki/internal/diagram"
	"github.com/splitsword/fine-codewiki/internal/docgen"
	"github.com/splitsword/fine-codewiki/internal/embedder"
	"github.com/splitsword/fine-codewiki/internal/grapher"
	"github.com/splitsword/fine-codewiki/internal/llm"
	"github.com/splitsword/fine-codewiki/internal/rag"
	"github.com/splitsword/fine-codewiki/internal/sequencer"
	"github.com/splitsword/fine-codewiki/internal/vectorstore"
)

// Config holds CLI configuration.
type Config struct {
	SourceDir   string
	OutputDir   string
	Language    string
	ProjectName string
	Port        int
	Interactive bool
	Question    string
	ConfigPath  string
}

// RunGenerate executes the full generate pipeline: parse → graph → diagram → doc.
func RunGenerate(cfg *Config) error {
	if cfg.ProjectName == "" {
		cfg.ProjectName = filepath.Base(cfg.SourceDir)
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = filepath.Join(cfg.SourceDir, ".codewiki", "wiki")
	}

	fmt.Printf("正在解析 %s 中的源文件...\n", cfg.SourceDir)
	files, err := analyzer.ParseDirectory(cfg.SourceDir, cfg.Language)
	if err != nil {
		return fmt.Errorf("parse directory: %w", err)
	}
	fmt.Printf("找到 %d 个源文件\n", len(files))

	// Normalize filenames: strip source directory prefix so module names are relative to project root
	absSource, err := filepath.Abs(cfg.SourceDir)
	if err != nil {
		absSource = cfg.SourceDir
	}
	for _, f := range files {
		absFile, err := filepath.Abs(f.Filename)
		if err == nil {
			f.Filename = absFile
		}
		if strings.HasPrefix(f.Filename, absSource) {
			rel := strings.TrimPrefix(f.Filename, absSource)
			rel = strings.TrimPrefix(rel, string(filepath.Separator))
			rel = strings.TrimPrefix(rel, "/")
			f.Filename = rel
		}
	}

	fmt.Println("正在构建依赖图...")
	graph := grapher.BuildGraph(files)
	fmt.Printf("图谱：%d 个节点，%d 条边\n", len(graph.Nodes), len(graph.Edges))

	fmt.Println("正在生成图表...")
	archDSL, err := diagram.GenerateArchitectureDiagram(graph)
	if err != nil {
		return fmt.Errorf("generate architecture diagram: %w", err)
	}

	classDSL, err := diagram.GenerateClassDiagram(graph)
	if err != nil {
		return fmt.Errorf("generate class diagram: %w", err)
	}

	fmt.Println("正在分析调用链...")
	callEdges, _ := sequencer.BuildCallGraph(cfg.SourceDir, files)
	fmt.Printf("找到 %d 个函数间调用\n", len(callEdges))

	sequences := sequencer.FindSequences(callEdges, 2)
	fmt.Printf("找到 %d 个序列模式\n", len(sequences))

	var seqDSL string
	if len(sequences) > 0 {
		seqDSL = sequencer.GenerateSequenceDiagram(sequences[0])
	} else {
		seqDSL = "sequenceDiagram\n"
	}

	fmt.Println("正在生成文档...")

	// Attempt to load LLM config for optional enhancement
	var provider llm.Provider
	appCfg, err := llm.LoadAppConfig("")
	if err != nil {
		fmt.Printf("警告：加载 LLM 配置失败 (%v)，将使用静态生成\n", err)
	} else {
		p, err := llm.NewGenerationProvider(appCfg)
		if err != nil {
			fmt.Printf("警告：创建文档生成 Provider 失败 (%v)，将使用静态生成\n", err)
		} else {
			provider = p
			fmt.Println("LLM 增强已启用")
		}
	}

	wiki, err := docgen.GenerateWikiEnhanced(context.Background(), provider, graph, cfg.SourceDir, cfg.ProjectName, archDSL, classDSL, seqDSL)
	if err != nil {
		return fmt.Errorf("generate wiki: %w", err)
	}

	fmt.Printf("正在将 Wiki 写入 %s...\n", cfg.OutputDir)
	if err := docgen.WriteWikiFiles(cfg.OutputDir, wiki); err != nil {
		return fmt.Errorf("write wiki files: %w", err)
	}

	fmt.Println("完成！")
	return nil
}

// RunServe starts a local HTTP server to preview the generated wiki.
func RunServe(cfg *Config) error {
	if cfg.OutputDir == "" {
		cfg.OutputDir = filepath.Join(".", ".codewiki", "wiki")
	}

	if _, err := os.Stat(cfg.OutputDir); os.IsNotExist(err) {
		return fmt.Errorf("Wiki 目录未找到：%s（请先运行 'generate'）", cfg.OutputDir)
	}

	addr := fmt.Sprintf(":%d", cfg.Port)
	fmt.Printf("正在从 %s 提供 Wiki 服务，访问 http://localhost%s\n", cfg.OutputDir, addr)
	fmt.Println("按 Ctrl+C 停止")

	handler := newWikiHandler(cfg.OutputDir)
	return http.ListenAndServe(addr, handler)
}

// wikiHandler serves wiki files with appropriate content types.
type wikiHandler struct {
	root string
}

func newWikiHandler(root string) http.Handler {
	return &wikiHandler{root: root}
}

func (h *wikiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := filepath.FromSlash(r.URL.Path)
	if path == "/" || path == "\\" {
		path = "overview.md"
	} else {
		path = strings.TrimPrefix(path, "/")
		path = strings.TrimPrefix(path, "\\")
	}

	fullPath := filepath.Join(h.root, path)
	fullPath = filepath.Clean(fullPath)

	// Security: prevent directory traversal
	absRoot, _ := filepath.Abs(h.root)
	absPath, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absPath, absRoot) {
		http.Error(w, "禁止访问", http.StatusForbidden)
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		http.Error(w, "未找到", http.StatusNotFound)
		return
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		http.Error(w, "读取文件出错", http.StatusInternalServerError)
		return
	}

	ext := strings.ToLower(filepath.Ext(path))
	navItems := listWikiFiles(h.root)

	if ext == ".md" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		body := renderMarkdownBody(data)
		title := strings.TrimSuffix(filepath.Base(path), ext)
		w.Write(buildWikiPage(title, body, navItems, path))
		return
	}

	if ext == ".mmd" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		filename := strings.TrimSuffix(filepath.Base(path), ext)
		body := fmt.Sprintf("<h2>%s</h2>\n<div class=\"mermaid\">\n%s\n</div>\n", htmlEscape(filename), string(data))
		w.Write(buildWikiPage(filename, body, navItems, path))
		return
	}

	w.Header().Set("Content-Type", contentTypeFor(path))
	w.Write(data)
}

// listWikiFiles returns sorted .md and .mmd filenames in the directory.
func listWikiFiles(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext == ".md" || ext == ".mmd" {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	return files
}

// renderMarkdownBody converts Markdown to HTML body content (no html/head/body wrapper).
func renderMarkdownBody(src []byte) string {
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
					body.WriteString(renderInline(cell))
					body.WriteString("</th>\n")
				} else if isTableSeparator(row) {
					continue
				} else {
					body.WriteString("<td>")
					body.WriteString(renderInline(cell))
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
			body.WriteString(renderInline(strings.TrimPrefix(trimmed, "> ")))
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
			body.WriteString(renderInline(item))
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
			body.WriteString(renderInline(item))
			body.WriteString("</li>\n")
			continue
		}

		// Flush any open list if line is not a list item
		flushList()

		// Headers
		if strings.HasPrefix(trimmed, "# ") {
			body.WriteString("<h1>")
			body.WriteString(renderInline(strings.TrimPrefix(trimmed, "# ")))
			body.WriteString("</h1>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			body.WriteString("<h2>")
			body.WriteString(renderInline(strings.TrimPrefix(trimmed, "## ")))
			body.WriteString("</h2>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "### ") {
			body.WriteString("<h3>")
			body.WriteString(renderInline(strings.TrimPrefix(trimmed, "### ")))
			body.WriteString("</h3>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "#### ") {
			body.WriteString("<h4>")
			body.WriteString(renderInline(strings.TrimPrefix(trimmed, "#### ")))
			body.WriteString("</h4>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "##### ") {
			body.WriteString("<h5>")
			body.WriteString(renderInline(strings.TrimPrefix(trimmed, "##### ")))
			body.WriteString("</h5>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "###### ") {
			body.WriteString("<h6>")
			body.WriteString(renderInline(strings.TrimPrefix(trimmed, "###### ")))
			body.WriteString("</h6>\n")
			continue
		}

		// Paragraph (or continuation of previous paragraph)
		if i > 0 && body.Len() > 0 && !strings.HasSuffix(body.String(), "\n") {
			body.WriteByte(' ')
			body.WriteString(renderInline(trimmed))
		} else {
			body.WriteString("<p>")
			body.WriteString(renderInline(trimmed))
			body.WriteString("</p>\n")
		}
	}

	flushList()
	flushTable()
	flushCode()

	return body.String()
}

// buildWikiPage assembles a full HTML page with optional sidebar navigation.
func buildWikiPage(title, body string, navItems []string, current string) []byte {
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
</style>
<script type="module">
  import mermaid from 'https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.esm.min.mjs';
  mermaid.initialize({ startOnLoad: true });
</script>
</head>
<body>
`)

	if len(navItems) > 0 {
		out.WriteString(`<nav class="sidebar">
<div class="sidebar-header">CodeWiki</div>
<ul>
`)
		for _, item := range navItems {
			activeClass := ""
			if item == current {
				activeClass = ` class="active"`
			}
			out.WriteString(fmt.Sprintf(`<li><a href="%s"%s>%s</a></li>
`, item, activeClass, item))
		}
		out.WriteString(`</ul>
</nav>
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

// markdownToHTML converts basic Markdown to a complete HTML page.
func markdownToHTML(src []byte) []byte {
	return buildWikiPage("CodeWiki", renderMarkdownBody(src), nil, "")
}

func renderInline(s string) string {
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
	// Simple approach: process outside of tags
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

func htmlEscape(s string) string {
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

func contentTypeFor(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".md":
		return "text/markdown; charset=utf-8"
	case ".mmd":
		return "text/plain; charset=utf-8"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".svg":
		return "image/svg+xml"
	default:
		return "text/plain; charset=utf-8"
	}
}

// RunAsk executes the RAG Q&A flow.
func RunAsk(cfg *Config) error {
	// Load LLM config
	configPath := cfg.ConfigPath
	if configPath == "" {
		configPath = llm.DefaultConfigPath()
	}
	appCfg, err := llm.LoadAppConfig(configPath)
	if err != nil || appCfg == nil {
		return fmt.Errorf("未找到 LLM 配置；运行 'codewiki config' 或设置 CODEWIKI_API_KEY")
	}

	provider, err := llm.NewEmbeddingProvider(appCfg)
	if err != nil {
		return fmt.Errorf("create LLM provider: %w", err)
	}

	// Setup vector store
	vectorPath := filepath.Join(cfg.SourceDir, ".codewiki", "vectors.db")
	store, err := vectorstore.NewSQLite(vectorPath)
	if err != nil {
		return fmt.Errorf("open vector store: %w", err)
	}
	defer store.Close()
	if store.Count() > 0 {
		fmt.Printf("从 %s 加载了 %d 个向量\n", vectorPath, store.Count())
	}

	// Parse source files
	files, err := analyzer.ParseDirectory(cfg.SourceDir, cfg.Language)
	if err != nil {
		return fmt.Errorf("parse directory: %w", err)
	}

	// Determine which files need re-indexing
	var changedFiles []*analyzer.FileResult
	currentPaths := make([]string, 0, len(files))
	for _, f := range files {
		currentPaths = append(currentPaths, f.Filename)
		info, err := os.Stat(f.Filename)
		if err != nil {
			continue
		}
		if store.ShouldIndexFile(f.Filename, info.ModTime().Unix(), info.Size()) {
			changedFiles = append(changedFiles, f)
		}
	}

	// Remove vectors for deleted files
	pruned := store.PruneFiles(currentPaths)
	if pruned > 0 {
		fmt.Printf("为已删除文件清理了 %d 个向量\n", pruned)
	}

	// Index changed files
	if len(changedFiles) > 0 {
		fmt.Printf("正在索引 %d 个变更文件...\n", len(changedFiles))
		chunks := chunker.New().ChunkFiles(changedFiles)
		fmt.Printf("创建了 %d 个分块\n", len(chunks))

		emb := embedder.New(provider, store)
		if err := emb.EmbedChunks(context.Background(), chunks); err != nil {
			return fmt.Errorf("embed chunks: %w", err)
		}
		fmt.Printf("嵌入了 %d 个分块\n", store.Count())

		// Mark files as indexed
		for _, f := range changedFiles {
			info, err := os.Stat(f.Filename)
			if err != nil {
				continue
			}
			store.MarkFileIndexed(f.Filename, info.ModTime().Unix(), info.Size())
		}
	} else if store.Count() == 0 {
		fmt.Println("向量存储为空且未找到源文件。")
	} else {
		fmt.Println("向量索引已是最新。")
	}

	engine := rag.NewEngine(provider, store)

	if cfg.Interactive {
		return runInteractiveAsk(engine)
	}

	return runSingleAsk(engine, cfg.Question)
}

func runSingleAsk(engine *rag.Engine, question string) error {
	fmt.Printf("\n> %s\n\n", question)
	ans, err := engine.Ask(context.Background(), question)
	if err != nil {
		return err
	}
	fmt.Println(ans.Text)
	if len(ans.Sources) > 0 {
		fmt.Println("\n--- 引用来源 ---")
		for _, s := range ans.Sources {
			loc := s.Filename
			if s.StartLine > 0 {
				loc = fmt.Sprintf("%s:%d", s.Filename, s.StartLine)
			}
			fmt.Printf("- %s（%s：%s）\n", loc, s.Type, s.Name)
		}
	}
	fmt.Println()
	return nil
}

func runInteractiveAsk(engine *rag.Engine) error {
	scanner := bufio.NewScanner(os.Stdin)
	session := rag.NewSession()
	fmt.Println("CodeWiki 交互式问答")
	fmt.Println("输入问题后按回车。输入 'exit' 或 'quit' 退出。")
	fmt.Println()
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" {
			fmt.Println("再见！")
			break
		}
		ans, err := engine.AskWithSession(context.Background(), line, session)
		if err != nil {
			fmt.Fprintf(os.Stderr, "错误：%v\n", err)
			continue
		}
		fmt.Println()
		fmt.Println(ans.Text)
		if len(ans.Sources) > 0 {
			fmt.Println("\n--- 引用来源 ---")
			for _, s := range ans.Sources {
				loc := s.Filename
				if s.StartLine > 0 {
					loc = fmt.Sprintf("%s:%d", s.Filename, s.StartLine)
				}
				fmt.Printf("- %s（%s：%s）\n", loc, s.Type, s.Name)
			}
		}
		fmt.Println()
		session.AddTurn(line, ans.Text)
	}
	return nil
}

// RunConfig launches an interactive configuration wizard.
func RunConfig(cfg *Config) error {
	return runConfigInteractive(cfg, os.Stdin)
}

func runConfigInteractive(cfg *Config, input io.Reader) error {
	scanner := bufio.NewScanner(input)

	// Load existing config as defaults
	appExisting, _ := llm.LoadAppConfig(cfg.ConfigPath)
	if appExisting == nil {
		appExisting = &llm.AppConfig{
			Generation: llm.Config{
				Provider:   "ollama",
				BaseURL:    "http://localhost:11434",
				Model:      "qwen:14b",
				MaxRetries: 3,
				Timeout:    60,
			},
			Embedding: llm.Config{
				Provider:   "ollama",
				BaseURL:    "http://localhost:11434",
				Model:      "nomic-embed-text",
				MaxRetries: 3,
				Timeout:    60,
			},
		}
	}

	fmt.Println("CodeWiki 配置")
	fmt.Println("----------------------")
	fmt.Println()

	// --- Generation config ---
	fmt.Println("【文档生成模型】（用于生成 Wiki 文档、图表描述）")
	gen := appExisting.Generation
	fmt.Printf("提供商（openai/ollama）[%s]：", gen.Provider)
	genProvider := readLine(scanner)
	if genProvider == "" {
		genProvider = gen.Provider
	}
	genProvider = strings.ToLower(genProvider)

	var genAPIKey string
	if genProvider == "openai" {
		fmt.Printf("API 密钥 [%s]：", maskKey(gen.APIKey))
		genAPIKey = readLine(scanner)
		if genAPIKey == "" {
			genAPIKey = gen.APIKey
		}
	}

	fmt.Printf("基础 URL [%s]：", gen.BaseURL)
	genBaseURL := readLine(scanner)
	if genBaseURL == "" {
		genBaseURL = gen.BaseURL
	}

	fmt.Printf("模型 [%s]：", gen.Model)
	genModel := readLine(scanner)
	if genModel == "" {
		genModel = gen.Model
	}

	genCfg := llm.Config{
		Provider:   genProvider,
		APIKey:     genAPIKey,
		BaseURL:    genBaseURL,
		Model:      genModel,
		MaxRetries: gen.MaxRetries,
		Timeout:    gen.Timeout,
	}

	// --- Embedding config ---
	fmt.Println()
	fmt.Println("【RAG 向量模型】（用于代码检索与问答）")
	fmt.Printf("使用与文档生成模型相同的配置？（y/n）[y]：")
	useSame := readLine(scanner)
	var embCfg llm.Config
	if useSame == "" || strings.ToLower(useSame) == "y" || strings.ToLower(useSame) == "yes" {
		embCfg = genCfg
	} else {
		emb := appExisting.Embedding
		fmt.Printf("提供商（openai/ollama）[%s]：", emb.Provider)
		embProvider := readLine(scanner)
		if embProvider == "" {
			embProvider = emb.Provider
		}
		embProvider = strings.ToLower(embProvider)

		var embAPIKey string
		if embProvider == "openai" {
			fmt.Printf("API 密钥 [%s]：", maskKey(emb.APIKey))
			embAPIKey = readLine(scanner)
			if embAPIKey == "" {
				embAPIKey = emb.APIKey
			}
		}

		fmt.Printf("基础 URL [%s]：", emb.BaseURL)
		embBaseURL := readLine(scanner)
		if embBaseURL == "" {
			embBaseURL = emb.BaseURL
		}

		fmt.Printf("模型 [%s]：", emb.Model)
		embModel := readLine(scanner)
		if embModel == "" {
			embModel = emb.Model
		}

		embCfg = llm.Config{
			Provider:   embProvider,
			APIKey:     embAPIKey,
			BaseURL:    embBaseURL,
			Model:      embModel,
			MaxRetries: emb.MaxRetries,
			Timeout:    emb.Timeout,
		}
	}

	newCfg := &llm.AppConfig{
		Generation: genCfg,
		Embedding:  embCfg,
	}

	path := cfg.ConfigPath
	if path == "" {
		path = llm.DefaultConfigPath()
	}

	if err := llm.SaveAppConfig(newCfg, path); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("\n配置已保存到 %s\n", path)
	fmt.Println("你可以通过环境变量覆盖设置：")
	fmt.Println("  文档生成：CODEWIKI_GEN_API_KEY, CODEWIKI_GEN_MODEL, CODEWIKI_GEN_BASE_URL")
	fmt.Println("  RAG 向量：CODEWIKI_EMB_API_KEY, CODEWIKI_EMB_MODEL, CODEWIKI_EMB_BASE_URL")
	fmt.Println("  统一覆盖：CODEWIKI_API_KEY, CODEWIKI_MODEL, CODEWIKI_BASE_URL")
	return nil
}

func readLine(scanner *bufio.Scanner) string {
	if !scanner.Scan() {
		return ""
	}
	return strings.TrimSpace(scanner.Text())
}

func maskKey(key string) string {
	if key == "" {
		return "(none)"
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}
