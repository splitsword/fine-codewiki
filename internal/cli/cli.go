package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/splitsword/fine-codewiki/internal/analyzer"
	"github.com/splitsword/fine-codewiki/internal/cache"
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
	SourceDir       string
	OutputDir       string
	Language        string
	ProjectName     string
	MaxLLMFunctions int
	Port            int
	Interactive     bool
	Question        string
	ConfigPath      string
	Force           bool
	Version         string
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

	// Setup AST + graph cache
	cachePath := filepath.Join(cfg.SourceDir, ".codewiki", "cache.json")
	c := cache.New(cachePath)
	_ = c.Load()

	// Walk source files
	paths, err := analyzer.WalkSourceFiles(cfg.SourceDir, cfg.Language)
	if err != nil {
		return fmt.Errorf("walk source files: %w", err)
	}

	// Determine which files need parsing vs cache hit
	type parseJob struct {
		path string
		src  string
	}
	var jobs []parseJob
	var files []*analyzer.FileResult
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !cfg.Force {
			if fr, ok := c.GetAST(p, info.ModTime().Unix(), info.Size()); ok {
				files = append(files, fr)
				continue
			}
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		jobs = append(jobs, parseJob{path: p, src: string(src)})
	}
	c.Prune(paths)

	astChanged := len(jobs) > 0
	if astChanged || cfg.Force {
		docgen.ClearWikiCheckpoint(cfg.SourceDir)
		if cfg.Force {
			fmt.Println("--force 已指定，强制重新解析所有文件并重新生成")
		}
	}
	if astChanged || cfg.Force {
		if cfg.Force {
			fmt.Printf("强制重新解析所有 %d 个源文件\n", len(jobs))
		} else {
			fmt.Printf("发现 %d 个新/变更文件需要解析（%d 个来自缓存）\n", len(jobs), len(files))
		}
		results := make([]*analyzer.FileResult, len(jobs))
		var parseErrs []error
		var mu sync.Mutex
		var wg sync.WaitGroup

		workers := runtime.GOMAXPROCS(0)
		if workers > len(jobs) {
			workers = len(jobs)
		}
		jobCh := make(chan int, len(jobs))
		for i := range jobs {
			jobCh <- i
		}
		close(jobCh)

		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range jobCh {
					job := jobs[i]
					fr, err := analyzer.ParseFile(job.path, job.src)
					if err != nil {
						mu.Lock()
						parseErrs = append(parseErrs, fmt.Errorf("parse %s: %w", job.path, err))
						mu.Unlock()
						continue
					}
					info, _ := os.Stat(job.path)
					if info != nil {
						c.PutAST(job.path, info.ModTime().Unix(), info.Size(), fr)
					}
					results[i] = fr
				}
			}()
		}
		wg.Wait()
		if len(parseErrs) > 0 {
			return parseErrs[0]
		}
		files = append(files, results...)
	} else {
		fmt.Printf("所有 %d 个源文件均来自缓存\n", len(files))
	}

	fmt.Printf("找到 %d 个源文件\n", len(files))

	// Normalize filenames: strip source directory prefix so module names are relative to project root
	absSource, err := filepath.Abs(cfg.SourceDir)
	if err != nil {
		absSource = cfg.SourceDir
	}
	for _, f := range files {
		// If the cached filename is relative, resolve it against the source
		// directory first so filepath.Abs doesn't use the current working dir.
		if !filepath.IsAbs(f.Filename) {
			f.Filename = filepath.Join(cfg.SourceDir, f.Filename)
		}
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
	var graph *grapher.Graph
	if !astChanged {
		graph = c.GetGraph()
	}
	if graph == nil {
		graph = grapher.BuildGraph(files)
		c.PutGraph(graph)
		fmt.Println("图谱已重新构建并缓存")
	}
	fmt.Printf("图谱：%d 个节点，%d 条边\n", len(graph.Nodes), len(graph.Edges))

	if err := c.Save(); err != nil {
		fmt.Printf("警告：保存缓存失败 (%v)\n", err)
	}

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
	var seqDesc string
	if len(sequences) > 0 {
		seqDSL = sequencer.GenerateSequenceDiagram(sequences[0])
		seqDesc = sequences[0].Description
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

	// 整个文档生成总体超时 60 分钟（thinking 模式需要更长推理时间）
	docCtx, docCancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer docCancel()
	wiki, err := docgen.GenerateWikiEnhancedWithMaxFunctions(docCtx, provider, graph, cfg.SourceDir, cfg.ProjectName, archDSL, classDSL, seqDSL, cfg.Language, cfg.MaxLLMFunctions)
	if err != nil {
		return fmt.Errorf("generate wiki: %w", err)
	}
	wiki.SequenceDescription = seqDesc
	wiki.Sequences = sequences
	wiki.CallEdges = callEdges

	fmt.Println("正在嵌入上下文图表和源码片段...")
	docgen.EmbedContextualContent(wiki, graph, cfg.SourceDir, sequences)

	fmt.Printf("正在将 Wiki 写入 %s...\n", cfg.OutputDir)
	// Write serve metadata (source dir + language) for source-reference popup
	writeServeMeta(cfg.OutputDir, cfg.SourceDir, cfg.Language)
	if err := docgen.WriteWikiFiles(cfg.OutputDir, wiki, graph); err != nil {
		return fmt.Errorf("write wiki files: %w", err)
	}

	fmt.Println("完成！")
	return nil
}

// RunServe starts a local HTTP server to preview the generated wiki.
// If cfg.SourceDir is provided, also enables the /ask RAG Q&A endpoint.
func RunServe(cfg *Config) error {
	if cfg.OutputDir == "" {
		cfg.OutputDir = filepath.Join(".", ".codewiki", "wiki")
	}

	// Auto-detect: if default dir doesn't exist, search subdirectories
	if _, err := os.Stat(cfg.OutputDir); os.IsNotExist(err) {
		found := false
		filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
			if found || err != nil {
				return nil
			}
			if info.IsDir() && info.Name() == "wiki" {
				parent := filepath.Dir(path)
				if filepath.Base(parent) == ".codewiki" {
					cfg.OutputDir = path
					found = true
				}
			}
			return nil
		})
		if !found {
			return fmt.Errorf("Wiki 目录未找到：%s\n请先运行 'generate' 或使用 -dir 指定 Wiki 目录", cfg.OutputDir)
		}
	}

	absDir, _ := filepath.Abs(cfg.OutputDir)
	fmt.Printf("Wiki 目录：%s\n", absDir)
	fmt.Printf("访问 http://localhost:%d 开始浏览\n", cfg.Port)

	var engine *rag.Engine
	if cfg.SourceDir != "" {
		var err error
		engine, err = initRAGEngine(cfg.SourceDir, cfg.Language, cfg.ConfigPath)
		if err != nil {
			fmt.Printf("警告：RAG 引擎初始化失败，/ask 端点不可用 (%v)\n", err)
		} else {
			fmt.Printf("RAG 引擎已就绪，访问 http://localhost:%d/ask 进行问答\n", cfg.Port)
		}
	}

	addr := fmt.Sprintf(":%d", cfg.Port)
	fmt.Printf("正在从 %s 提供 Wiki 服务，访问 http://localhost%s\n", cfg.OutputDir, addr)
	fmt.Println("按 Ctrl+C 停止")

	handler := newServerHandler(cfg.OutputDir, engine)
	if v, ok := handler.(*serverHandler); ok {
		v.sourceDir = cfg.SourceDir
		v.language = cfg.Language
		if v.sourceDir == "" {
			if meta := readServeMeta(cfg.OutputDir); meta != nil {
				v.sourceDir = meta.SourceDir
				if v.language == "" {
					v.language = meta.Language
				}
			}
		}
	}
	if engine != nil {
		defer engine.Close()
	}
	return http.ListenAndServe(addr, handler)
}

// initRAGEngine sets up the RAG engine for the web Q&A endpoint.
func initRAGEngine(sourceDir, language, configPath string) (*rag.Engine, error) {
	if configPath == "" {
		configPath = llm.DefaultConfigPath()
	}
	appCfg, err := llm.LoadAppConfig(configPath)
	if err != nil || appCfg == nil {
		return nil, fmt.Errorf("未找到 LLM 配置")
	}

	provider, err := llm.NewEmbeddingProvider(appCfg)
	if err != nil {
		return nil, fmt.Errorf("create provider: %w", err)
	}

	vectorPath := filepath.Join(sourceDir, ".codewiki", "vectors.db")
	store, err := vectorstore.NewSQLite(vectorPath)
	if err != nil {
		return nil, fmt.Errorf("open vector store: %w", err)
	}

	files, err := analyzer.ParseDirectory(sourceDir, language)
	if err != nil {
		return nil, fmt.Errorf("parse directory: %w", err)
	}

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
	store.PruneFiles(currentPaths)

	var allChunks []*chunker.Chunk
	if len(changedFiles) > 0 {
		chunks := chunker.New(sourceDir).ChunkFiles(changedFiles)
		allChunks = append(allChunks, chunks...)
		for _, f := range changedFiles {
			info, err := os.Stat(f.Filename)
			if err != nil {
				continue
			}
			store.MarkFileIndexed(f.Filename, info.ModTime().Unix(), info.Size())
		}
	}

	// Index wiki documents for richer RAG context
	wikiDir := filepath.Join(sourceDir, ".codewiki", "wiki")
	wikiChunks := loadAndChunkWikiDocs(wikiDir)
	allChunks = append(allChunks, wikiChunks...)

	if len(allChunks) > 0 {
		emb := embedder.New(provider, store)
		if err := emb.EmbedChunks(context.Background(), allChunks); err != nil {
			return nil, fmt.Errorf("embed chunks: %w", err)
		}
	}

	engine := rag.NewEngine(provider, store)
	if genProvider, genErr := llm.NewGenerationProvider(appCfg); genErr == nil {
		engine.SetGenProvider(genProvider)
	}
	engine.SetProjectContext(filepath.Base(sourceDir), "")
	engine.SetPinnedContext(loadWikiOverview(sourceDir))
	return engine, nil
}

// loadWikiOverview reads the project overview + key design decisions from the wiki.
func loadWikiOverview(sourceDir string) string {
	wikiDir := filepath.Join(sourceDir, ".codewiki", "wiki")
	var parts []string
	for _, name := range []string{"00-overview.md", "04-key-concepts.md"} {
		data, err := os.ReadFile(filepath.Join(wikiDir, name))
		if err != nil {
			continue
		}
		content := stripFrontmatter(string(data))
		if content != "" {
			parts = append(parts, content)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n---\n"))
}

func stripFrontmatter(s string) string {
	if idx := strings.Index(s, "---\n"); idx >= 0 {
		if idx2 := strings.Index(s[idx+4:], "---\n"); idx2 >= 0 {
			s = s[idx+4+idx2+4:]
		}
	}
	runes := []rune(strings.TrimSpace(s))
	if len(runes) > 2500 {
		return string(runes[:2500]) + "\n..."
	}
	return string(runes)
}

// loadAndChunkWikiDocs reads wiki markdown files from a directory and converts
// them to semantic chunks for RAG indexing.
func loadAndChunkWikiDocs(wikiDir string) []*chunker.Chunk {
	entries, err := os.ReadDir(wikiDir)
	if err != nil {
		return nil
	}

	docs := make(map[string]string)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".md") {
			continue
		}
		// Skip compilation (full-archive) and module README (index, not content)
		if name == "compilation.md" {
			continue
		}
		path := filepath.Join(wikiDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		docName := strings.TrimSuffix(name, ".md")
		docs[docName] = string(data)
	}

	if len(docs) == 0 {
		return nil
	}

	chk := chunker.New("")
	return chk.ChunkWikiDocs(docs)
}

// serverHandler serves wiki files and optionally the /ask RAG Q&A endpoint.
type serverHandler struct {
	root      string
	engine    *rag.Engine
	sourceDir string
	language  string
}

func newServerHandler(root string, engine *rag.Engine) http.Handler {
	return &serverHandler{root: root, engine: engine}
}

func (h *serverHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Route matching uses raw URL path (cross-platform)
	rawPath := r.URL.Path

	// RAG endpoints
	if rawPath == "/ask" && r.Method == http.MethodGet {
		serveAskPage(w, h.engine != nil)
		return
	}
	if rawPath == "/api/ask" && r.Method == http.MethodPost {
		h.handleAskAPI(w, r)
		return
	}

	if rawPath == "/api/source" && h.sourceDir != "" {
		h.handleSourceAPI(w, r)
		return
	}

	if rawPath == "/" {
		// Prefer the pre-generated three-column index.html; fall back to dynamic page.
		indexPath := filepath.Join(h.root, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			http.ServeFile(w, r, indexPath)
		} else {
			serveIndexPage(w, h.root)
		}
		return
	}
	path := strings.TrimPrefix(rawPath, "/")
	path = filepath.FromSlash(path)

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
	sections, totalArts, totalMins := buildNavSections(h.root)

	if ext == ".md" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		body := docgen.RenderMarkdownBody(data)
		title := strings.TrimSuffix(filepath.Base(path), ext)
		readMin := docgen.EstimateReadingTime(string(data))
		difficulty := articleDifficulty(path)
		badge := fmt.Sprintf(`<div style="display:flex;align-items:center;gap:10px;margin-bottom:16px;flex-wrap:wrap"><span style="display:inline-flex;align-items:center;gap:4px;padding:3px 10px;border-radius:20px;font-size:12px;font-weight:600;background:var(--accent-glow);color:var(--accent)">⏱ %d 分钟阅读</span><span style="display:inline-flex;align-items:center;gap:4px;padding:3px 10px;border-radius:20px;font-size:12px;font-weight:600;background:%s;color:%s">%s</span></div>`, readMin, difficulty.bg, difficulty.fg, difficulty.label)
		body = badge + body
		w.Write(docgen.BuildWikiPage(title, body, path, sections, totalArts, totalMins))
		return
	}

	if ext == ".mmd" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		filename := strings.TrimSuffix(filepath.Base(path), ext)
		body := fmt.Sprintf("<h2>%s</h2>\n<div class=\"mermaid\">\n%s\n</div>\n", docgen.HTMLEscape(filename), string(data))
		w.Write(docgen.BuildWikiPage(filename, body, path, sections, totalArts, totalMins))
		return
	}

	w.Header().Set("Content-Type", contentTypeFor(path))
	w.Write(data)
}

// serveAskPage renders the interactive Q&A HTML page.
func serveAskPage(w http.ResponseWriter, enabled bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if !enabled {
		w.Write([]byte(`<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="UTF-8"><title>问答</title></head>
<body><h1>问答终端</h1><p>使用 <code>--source</code> 启动 serve 以启用 RAG 问答。</p></body></html>`))
		return
	}
	w.Write([]byte(askPageHTML))
}

// handleAskAPI processes a Q&A request and returns a JSON answer.
func (h *serverHandler) handleAskAPI(w http.ResponseWriter, r *http.Request) {
	if h.engine == nil {
		http.Error(w, `{"error":"RAG 引擎未启用"}`, http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Question string `json:"question"`
		History  []struct {
			Question string `json:"question"`
			Answer   string `json:"answer"`
		} `json:"history"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"请求格式错误"}`, http.StatusBadRequest)
		return
	}
	if req.Question == "" {
		http.Error(w, `{"error":"问题不能为空"}`, http.StatusBadRequest)
		return
	}

	var session *rag.Session
	if len(req.History) > 0 {
		session = rag.NewSession()
		for _, turn := range req.History {
			session.AddTurn(turn.Question, turn.Answer)
		}
	}

	textCh, ans, err := h.engine.AskStreamWithSession(r.Context(), req.Question, session)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	var textParts []string
	for token := range textCh {
		textParts = append(textParts, token)
	}

	resp := struct {
		Text    string       `json:"text"`
		Sources []rag.Source `json:"sources"`
	}{
		Text:    strings.Join(textParts, ""),
		Sources: ans.Sources,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(resp)
}

// serveMeta stores metadata needed by the serve command for source-reference lookups.
type serveMeta struct {
	SourceDir string `json:"source_dir"`
	Language  string `json:"language"`
}

func serveMetaPath(outputDir string) string {
	return filepath.Join(outputDir, ".serve_meta.json")
}

func writeServeMeta(outputDir, sourceDir, language string) {
	meta := serveMeta{SourceDir: sourceDir, Language: language}
	data, _ := json.Marshal(meta)
	os.WriteFile(serveMetaPath(outputDir), data, 0644)
}

func readServeMeta(outputDir string) *serveMeta {
	data, err := os.ReadFile(serveMetaPath(outputDir))
	if err != nil {
		return nil
	}
	var meta serveMeta
	if json.Unmarshal(data, &meta) != nil {
		return nil
	}
	return &meta
}

// langToExts maps a language name to its primary source file extensions.
var langToExts = map[string][]string{
	"python":     {".py"},
	"go":         {".go"},
	"javascript": {".js", ".jsx", ".mjs", ".cjs"},
	"typescript": {".ts", ".tsx"},
	"rust":       {".rs"},
	"java":       {".java"},
	"cpp":        {".cpp", ".cxx", ".cc", ".c"},
	"c":          {".c", ".h"},
	"ruby":       {".rb"},
	"php":        {".php"},
	"swift":      {".swift"},
	"kotlin":     {".kt", ".kts"},
}

func extsForLang(lang string) []string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if exts, ok := langToExts[lang]; ok {
		return append(exts, ".md")
	}
	return []string{".go", ".py", ".ts", ".tsx", ".js", ".jsx", ".rs", ".java", ".cpp", ".c", ".rb", ".php", ".swift", ".kt", ".md"}
}

// handleSourceAPI serves source file content for the source-reference popup.
func (h *serverHandler) handleSourceAPI(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	if file == "" {
		http.Error(w, "缺少 file 参数", http.StatusBadRequest)
		return
	}

	// Security: prevent directory traversal
	cleanFile := filepath.Clean(file)
	if strings.Contains(cleanFile, "..") {
		http.Error(w, "禁止访问", http.StatusForbidden)
		return
	}

	// Try progressively: exact path, then with extensions, then strip prefix + extensions
	exts := extsForLang(h.language)
	found := false
	basePath := filepath.Join(h.sourceDir, cleanFile)
	rest := ""
	for {
		// Try current base with each extension
		for _, ext := range exts {
			if _, err := os.Stat(basePath + ext); err == nil {
				basePath = basePath + ext
				found = true
				break
			}
		}
		if found {
			break
		}
		// Also try exact (no extension)
		if _, err := os.Stat(basePath); err == nil {
			found = true
			break
		}
		// Strip one more prefix segment
		if rest == "" {
			rest = basePath[len(h.sourceDir)+1:]
		}
		sepIdx := strings.Index(rest, string(filepath.Separator))
		if sepIdx < 0 {
			break
		}
		rest = rest[sepIdx+1:]
		basePath = filepath.Join(h.sourceDir, rest)
	}
	if !found {
		http.Error(w, "源文件不存在: "+cleanFile, http.StatusNotFound)
		return
	}
	fullPath := basePath

	data, err := os.ReadFile(fullPath)
	if err != nil {
		http.Error(w, "源文件不存在: "+cleanFile, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data)
}

// askPageHTML is the interactive Q&A web UI.
const askPageHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>CodeWiki 问答终端</title>
<style>
* { box-sizing: border-box; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; margin: 0; background: #f5f5f5; color: #333; }
.container { max-width: 800px; margin: 0 auto; padding: 20px; display: flex; flex-direction: column; height: 100vh; }
header { text-align: center; margin-bottom: 16px; }
header h1 { margin: 0; font-size: 1.5em; }
header p { margin: 4px 0 0; color: #666; font-size: 0.9em; }
.chat { flex: 1; overflow-y: auto; background: #fff; border-radius: 8px; padding: 16px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
.message { margin-bottom: 12px; }
.message.user { text-align: right; }
.message.user .bubble { background: #0969da; color: #fff; display: inline-block; padding: 10px 14px; border-radius: 16px; max-width: 80%; word-break: break-word; }
.message.assistant .bubble { background: #f0f0f0; color: #333; display: inline-block; padding: 10px 14px; border-radius: 16px; max-width: 80%; word-break: break-word; }
.message.assistant .sources { margin-top: 6px; font-size: 0.85em; color: #555; }
.message.assistant .sources span { display: block; margin: 2px 0; }
.input-area { display: flex; gap: 8px; margin-top: 12px; }
.input-area input { flex: 1; padding: 12px 16px; border: 1px solid #d0d7de; border-radius: 24px; font-size: 1em; outline: none; }
.input-area input:focus { border-color: #0969da; }
.input-area button { padding: 12px 24px; background: #0969da; color: #fff; border: none; border-radius: 24px; font-size: 1em; cursor: pointer; }
.input-area button:hover { background: #0550ae; }
.input-area button:disabled { background: #8ec2f7; cursor: not-allowed; }
.loading { color: #666; font-style: italic; }
</style>
</head>
<body>
<div class="container">
<header><h1>CodeWiki 问答终端</h1><p>基于项目代码库的 RAG 智能问答</p></header>
<div class="chat" id="chat">
  <div class="message assistant"><div class="bubble">你好！我是你的项目代码助手。请提出关于代码库的问题。</div></div>
</div>
<div class="input-area">
  <input type="text" id="question" placeholder="输入你的问题..." onkeydown="if(event.key==='Enter') send()" />
  <button id="sendBtn" onclick="send()">发送</button>
</div>
</div>
<script>
const chat = document.getElementById('chat');
const input = document.getElementById('question');
const btn = document.getElementById('sendBtn');
let history = [];
function appendBubble(role, text) {
  const div = document.createElement('div');
  div.className = 'message ' + role;
  const bubble = document.createElement('div');
  bubble.className = 'bubble';
  bubble.textContent = text;
  div.appendChild(bubble);
  chat.appendChild(div);
  chat.scrollTop = chat.scrollHeight;
  return div;
}
function appendSources(div, sources) {
  if (!sources || sources.length === 0) return;
  const sdiv = document.createElement('div');
  sdiv.className = 'sources';
  sdiv.innerHTML = '<strong>引用来源：</strong>';
  sources.forEach(s => {
    const span = document.createElement('span');
    span.textContent = s.filename + (s.startLine > 0 ? ':' + s.startLine : '') + '（' + s.type + '：' + s.name + '）';
    sdiv.appendChild(span);
  });
  div.appendChild(sdiv);
  chat.scrollTop = chat.scrollHeight;
}
async function send() {
  const q = input.value.trim();
  if (!q) return;
  appendBubble('user', q);
  input.value = '';
  btn.disabled = true;
  const loading = appendBubble('assistant', '正在思考...');
  try {
    const res = await fetch('/api/ask', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ question: q, history: history })
    });
    loading.remove();
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: '请求失败' }));
      appendBubble('assistant', '错误：' + err.error);
      return;
    }
    const data = await res.json();
    const div = appendBubble('assistant', data.text);
    appendSources(div, data.sources);
    history.push({ question: q, answer: data.text });
  } catch (e) {
    loading.remove();
    appendBubble('assistant', '网络错误：' + e.message);
  } finally {
    btn.disabled = false;
    input.focus();
  }
}
</script>
</body>
</html>`

// serveIndexPage renders the wiki index/landing page.
func serveIndexPage(w http.ResponseWriter, root string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Read overview content if available for embedding
	var overviewPreview string
	if data, err := os.ReadFile(filepath.Join(root, "00-overview.md")); err == nil {
		lines := strings.Split(string(data), "\n")
		var previewLines []string
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "-") {
				continue
			}
			previewLines = append(previewLines, trimmed)
			if len(previewLines) >= 3 {
				break
			}
		}
		overviewPreview = strings.Join(previewLines, " ")
	}

	var b strings.Builder
	b.WriteString(`<div class="index-page">
<h1>📚 代码百科</h1>
<p class="tagline">交互式代码库学习指南 — 从理解到深入</p>
`)

	if overviewPreview != "" {
		b.WriteString(fmt.Sprintf(`<div class="index-preview"><p>%s</p></div>`, docgen.HTMLEscape(overviewPreview)))
	}

	// Search box
	b.WriteString(`<div style="margin:20px 0;position:relative">
	<input type="text" id="wiki-search" placeholder="搜索文章、模块..." autocomplete="off"
	style="width:100%;padding:10px 16px;font-size:15px;border:2px solid #ddd;border-radius:8px;outline:none;box-sizing:border-box"
	onfocus="this.style.borderColor='#2196f3'" onblur="this.style.borderColor='#ddd'">
	<div id="search-results" style="position:absolute;top:100%;left:0;right:0;background:#fff;border:1px solid #ddd;border-top:none;border-radius:0 0 8px 8px;max-height:300px;overflow-y:auto;z-index:100;display:none"></div>
	</div>
	`)

	// Learning Guide section
	b.WriteString(`<div class="index-section">
<h2>📖 学习指南</h2>
<p>如果你是第一次接触这个项目，建议按顺序阅读以下文档：</p>
<ul>
`)
	learningFiles := []struct{ file, title, desc string }{
		{"00-overview.md", "项目概述", "项目定位、规模统计、模块概览"},
		{"01-what-it-does.md", "项目能做什么", "核心能力、使用场景、目标用户"},
		{"02-architecture.md", "架构说明", "系统分层、设计模式、模块关系"},
		{"03-project-structure.md", "项目结构", "目录组织、模块职责、依赖关系"},
		{"04-key-concepts.md", "核心概念", "关键设计决策与架构思想"},
		{"05-learning-path.md", "学习路径", "按目标选择阅读路径"},
	}
	for _, item := range learningFiles {
		if _, err := os.Stat(filepath.Join(root, item.file)); err == nil {
			b.WriteString(fmt.Sprintf(`<li><a href="%s"><strong>%s</strong></a> — %s</li>`, item.file, item.title, item.desc))
			b.WriteByte('\n')
		}
	}
	b.WriteString(`</ul>
</div>
`)

	// Reference section
	b.WriteString(`<div class="index-section">
<h2>📋 参考手册</h2>
<ul>
`)
	if _, err := os.Stat(filepath.Join(root, "api-reference.md")); err == nil {
		b.WriteString(`<li><a href="api-reference.md"><strong>API 参考</strong></a> — 全部类、函数、方法的详细说明</li>` + "\n")
	}
	b.WriteString(`</ul>
</div>
`)

	// Diagrams are now embedded inside their thematic articles
	// (architecture, key-concepts, learning-path) via inline mermaid blocks.
	// No standalone diagram links needed.

	// Ask AI hint
	b.WriteString(`<div class="index-section index-ask">
<h2>💬 有具体问题？</h2>
<p>在终端运行 <code>codewiki ask "你的问题"</code> 与 AI 对话，获取带源码引用的答案。</p>
</div>
</div>
`)

	// Build search index and inject search JS
	searchJS := `<script>
(function(){
	var idx=[
		{file:"00-overview.md",title:"项目概述",desc:"项目定位、规模统计、模块概览"},
		{file:"01-what-it-does.md",title:"项目能做什么",desc:"核心能力、使用场景、目标用户"},
		{file:"02-architecture.md",title:"架构说明",desc:"系统分层、设计模式、模块关系"},
		{file:"03-project-structure.md",title:"项目结构",desc:"目录组织、模块职责、依赖关系"},
		{file:"04-key-concepts.md",title:"核心概念",desc:"关键设计决策与架构思想"},
		{file:"05-learning-path.md",title:"学习路径",desc:"按目标选择阅读路径"},
		{file:"api-reference.md",title:"API 参考",desc:"全部类、函数、方法的详细说明"}
	];
	var inp=document.getElementById("wiki-search");
	var res=document.getElementById("search-results");
	inp.addEventListener("input",function(){
		var q=inp.value.toLowerCase();
		res.innerHTML="";
		if(!q){res.style.display="none";return;}
		var hits=[];
		for(var i=0;i<idx.length;i++){
			var s=idx[i].title+" "+idx[i].desc;
			var p=s.toLowerCase().indexOf(q);
			if(p>=0){hits.push({item:idx[i],pos:p});}
		}
		if(hits.length==0){
			res.innerHTML='<div style="padding:10px;color:#999">未找到匹配结果</div>';
			res.style.display="block";
			return;
		}
		hits.sort(function(a,b){return a.pos-b.pos;});
		var html="";
		for(var i=0;i<hits.length;i++){
			var h=hits[i].item;
			html+='<a href="'+h.file+'" style="display:block;padding:10px 16px;text-decoration:none;color:#333;border-bottom:1px solid #eee">';
			html+='<strong>'+h.title+'</strong></a>';
		}
		res.innerHTML=html;
		res.style.display="block";
	});
	document.addEventListener("click",function(e){
		if(e.target!==inp){res.style.display="none";}
	});
})();
</script>`
	// Inject search JS before the closing index-page div
	pageHTML := strings.Replace(b.String(), "</div>\n</div>", "</div>\n"+searchJS+"\n</div>", 1)

	sections, totalArts, totalMins := buildNavSections(root)
	w.Write(docgen.BuildWikiPage("CodeWiki", pageHTML, "", sections, totalArts, totalMins))
}

func buildIndexLink(file, title, desc string) string {
	return fmt.Sprintf(`<li><a href="%s"><strong>%s</strong></a> — %s</li>`, file, title, desc)
}

type difficultyInfo struct {
	label string
	bg    string
	fg    string
}

func articleDifficulty(path string) difficultyInfo {
	base := strings.ToLower(filepath.Base(path))
	switch {
	case strings.HasPrefix(base, "00"), strings.HasPrefix(base, "01"):
		return difficultyInfo{"⭐ 入门", "rgba(16,185,129,.12)", "#059669"}
	case strings.HasPrefix(base, "02"), strings.HasPrefix(base, "03"), strings.HasPrefix(base, "05"):
		return difficultyInfo{"⭐⭐ 进阶", "rgba(245,158,11,.12)", "#d97706"}
	case strings.HasPrefix(base, "04"), strings.HasPrefix(base, "api"):
		return difficultyInfo{"⭐⭐⭐ 高级", "rgba(239,68,68,.12)", "#dc2626"}
	default:
		return difficultyInfo{"📖 参考", "rgba(99,102,241,.12)", "#6366f1"}
	}
}

// buildNavSections categorizes wiki files into 4 navigation sections.
func buildNavSections(root string) ([]docgen.NavSection, int, int) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, 0, 0
	}

	// Known file icons
	fileIcons := map[string]string{
		"00-overview.md":          "📊",
		"01-what-it-does.md":      "🎯",
		"02-architecture.md":      "🏗️",
		"03-project-structure.md": "📁",
		"04-key-concepts.md":      "💡",
		"05-learning-path.md":     "🗺️",
		"api-reference.md":        "📋",
	}

	// Section definitions: label, icon, and which file prefixes belong
	type sectionDef struct {
		label  string
		icon   string
		prefix []string
	}
	defs := []sectionDef{
		{"认识项目", "📘", []string{"00-", "01-"}},
		{"开始阅读", "📗", []string{"02-", "03-"}},
		{"深入剖析", "📕", []string{"04-", "05-"}},
		{"速查", "📓", nil},
	}

	sections := make([]docgen.NavSection, len(defs))
	for i, d := range defs {
		sections[i] = docgen.NavSection{Label: d.label, Icon: d.icon}
	}

	matchSection := func(name string) int {
		for i, d := range defs {
			for _, p := range d.prefix {
				if strings.HasPrefix(name, p) {
					return i
				}
			}
		}
		return 3 // default to 速查
	}

	var fileNames []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".md" && ext != ".mmd" {
			continue
		}
		// Skip compilation and index artifacts
		if name == "compilation.md" || name == "index.html" {
			continue
		}
		fileNames = append(fileNames, name)
	}
	sort.Strings(fileNames)

	totalArticles := 0
	totalMinutes := 0

	for _, name := range fileNames {
		ext := strings.ToLower(filepath.Ext(name))
		secIdx := matchSection(name)

		// Determine icon
		icon := "📄"
		if ext == ".mmd" {
			icon = "📊"
		}
		if ic, ok := fileIcons[name]; ok {
			icon = ic
		}

		// Title: strip extension, replace hyphens/underscores for readability
		title := strings.TrimSuffix(name, ext)
		title = strings.ReplaceAll(title, "-", " ")
		title = strings.ReplaceAll(title, "_", " ")

		// Reading time and difficulty (for .md files only)
		readingTime := 0
		difficulty := ""
		if ext == ".md" {
			if data, err := os.ReadFile(filepath.Join(root, name)); err == nil {
				readingTime = docgen.EstimateReadingTime(string(data))
			}
			diff := articleDifficulty(name)
			difficulty = diff.label
			totalArticles++
			totalMinutes += readingTime
		}

		sections[secIdx].Items = append(sections[secIdx].Items, docgen.NavItem{
			URL:         name,
			Title:       title,
			Icon:        icon,
			ReadingTime: readingTime,
			Difficulty:  difficulty,
		})
	}

	// Also collect module files from modules/ subdirectory
	modulesDir := filepath.Join(root, "modules")
	if modEntries, err := os.ReadDir(modulesDir); err == nil {
		var modNames []string
		for _, e := range modEntries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.ToLower(filepath.Ext(name)) == ".md" {
				modNames = append(modNames, name)
			}
		}
		sort.Strings(modNames)
		for _, name := range modNames {
			title := strings.TrimSuffix(name, ".md")
			title = strings.ReplaceAll(title, "-", " ")
			title = strings.ReplaceAll(title, "_", " ")

			readingTime := 0
			if data, err := os.ReadFile(filepath.Join(modulesDir, name)); err == nil {
				readingTime = docgen.EstimateReadingTime(string(data))
			}
			diff := inferModuleDiffFromName(name)

			sections[2].Items = append(sections[2].Items, docgen.NavItem{ // 深入剖析
				URL:         "modules/" + name,
				Title:       title,
				Icon:        "📦",
				ReadingTime: readingTime,
				Difficulty:  diff,
			})
			totalArticles++
			totalMinutes += readingTime
		}
	}

	// Remove empty sections
	var result []docgen.NavSection
	for _, sec := range sections {
		if len(sec.Items) > 0 {
			result = append(result, sec)
		}
	}

	return result, totalArticles, totalMinutes
}

// inferModuleDiffFromName assigns a difficulty label based on module name complexity.
func inferModuleDiffFromName(name string) string {
	parts := strings.Split(strings.TrimSuffix(name, ".md"), "_")
	depth := len(parts)
	switch {
	case depth <= 1:
		return "⭐ 入门"
	case depth == 2:
		return "⭐⭐ 进阶"
	default:
		return "⭐⭐⭐ 深入"
	}
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
	var allChunks []*chunker.Chunk
	if len(changedFiles) > 0 {
		fmt.Printf("正在索引 %d 个变更文件...\n", len(changedFiles))
		chunks := chunker.New(cfg.SourceDir).ChunkFiles(changedFiles)
		allChunks = append(allChunks, chunks...)

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

	// Index wiki documents for richer RAG context
	wikiDir := filepath.Join(cfg.SourceDir, ".codewiki", "wiki")
	wikiChunks := loadAndChunkWikiDocs(wikiDir)
	allChunks = append(allChunks, wikiChunks...)

	if len(allChunks) > 0 {
		fmt.Printf("创建了 %d 个分块（含 %d 个文档分块）\n", len(allChunks), len(wikiChunks))
		emb := embedder.New(provider, store)
		if err := emb.EmbedChunks(context.Background(), allChunks); err != nil {
			return fmt.Errorf("embed chunks: %w", err)
		}
		fmt.Printf("嵌入了 %d 个分块\n", store.Count())
	}

	engine := rag.NewEngine(provider, store)
	if genProvider, genErr := llm.NewGenerationProvider(appCfg); genErr == nil {
		engine.SetGenProvider(genProvider)
	}
	engine.SetProjectContext(filepath.Base(cfg.SourceDir), "")
	engine.SetPinnedContext(loadWikiOverview(cfg.SourceDir))

	if cfg.Interactive {
		return runInteractiveAsk(engine)
	}

	return runSingleAsk(engine, cfg.Question)
}

func runSingleAsk(engine *rag.Engine, question string) error {
	fmt.Printf("\n> %s\n\n", question)
	textCh, ans, err := engine.AskStream(context.Background(), question)
	if err != nil {
		return err
	}
	for token := range textCh {
		fmt.Print(token)
	}
	fmt.Println()
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
		textCh, ans, err := engine.AskStreamWithSession(context.Background(), line, session)
		if err != nil {
			fmt.Fprintf(os.Stderr, "错误：%v\n", err)
			continue
		}
		fmt.Println()
		var fullText strings.Builder
		for token := range textCh {
			fmt.Print(token)
			fullText.WriteString(token)
		}
		fmt.Println()
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
		session.AddTurn(line, fullText.String())
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

// RunUpdate checks GitHub Releases for a newer version and self-updates.
func RunUpdate(cfg *Config) error {
	current := cfg.Version
	if current == "" || current == "dev" {
		fmt.Println("当前为开发版本（dev），无法自动更新。请通过 go install 或手动下载更新。")
		return nil
	}

	fmt.Printf("当前版本：%s\n", current)
	fmt.Println("正在检查更新...")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/splitsword/fine-codewiki/releases/latest")
	if err != nil {
		return fmt.Errorf("检查更新失败：无法访问 GitHub API (%w)", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("检查更新失败：GitHub API 返回 %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return fmt.Errorf("解析 GitHub 响应失败 (%w)", err)
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	if latest == "" {
		return fmt.Errorf("无法解析远程版本号")
	}

	if latest == current {
		fmt.Printf("已是最新版本 (%s)。\n", current)
		return nil
	}

	fmt.Printf("发现新版本：%s → %s\n", current, latest)

	osName := runtime.GOOS
	arch := runtime.GOARCH
	ext := ".tar.gz"
	binaryName := "codewiki"
	if osName == "windows" {
		ext = ".zip"
		binaryName = "codewiki.exe"
	}
	assetName := "codewiki-v" + latest + "-" + osName + "-" + arch + ext

	var downloadURL string
	for _, a := range release.Assets {
		if a.Name == assetName {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		return fmt.Errorf("未找到 %s/%s 平台的新版本资产 (%s)", osName, arch, assetName)
	}

	tmpDir, err := os.MkdirTemp("", "codewiki-update")
	if err != nil {
		return fmt.Errorf("创建临时目录失败 (%w)", err)
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, assetName)
	fmt.Printf("正在下载 %s...\n", assetName)

	dlResp, err := client.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("下载失败 (%w)", err)
	}
	defer dlResp.Body.Close()

	f, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("创建临时文件失败 (%w)", err)
	}
	if _, err := io.Copy(f, dlResp.Body); err != nil {
		f.Close()
		return fmt.Errorf("写入下载内容失败 (%w)", err)
	}
	f.Close()

	fmt.Println("正在解压...")
	if ext == ".zip" {
		if err := extractZip(archivePath, tmpDir); err != nil {
			return fmt.Errorf("解压失败 (%w)", err)
		}
	} else {
		if err := extractTarGz(archivePath, tmpDir); err != nil {
			return fmt.Errorf("解压失败 (%w)", err)
		}
	}

	newBinary := filepath.Join(tmpDir, binaryName)
	if _, err := os.Stat(newBinary); err != nil {
		return fmt.Errorf("在解压内容中未找到 %s", binaryName)
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前可执行文件路径失败 (%w)", err)
	}

	if runtime.GOOS == "windows" {
		newPath := execPath + ".new"
		newData, err := os.ReadFile(newBinary)
		if err != nil {
			return fmt.Errorf("读取新二进制失败 (%w)", err)
		}
		if err := os.WriteFile(newPath, newData, 0755); err != nil {
			return fmt.Errorf("写入新版本失败 (%w)", err)
		}
		swapScript := execPath + ".swap.bat"
		script := "@echo off\r\ntimeout /t 2 /nobreak >nul\r\nmove /Y \"" + execPath + "\" \"" + execPath + ".old\"\r\nmove /Y \"" + execPath + ".new\" \"" + execPath + "\"\r\ndel \"" + swapScript + "\"\r\n"
		if err := os.WriteFile(swapScript, []byte(script), 0644); err != nil {
			return fmt.Errorf("创建替换脚本失败 (%w)", err)
		}
		fmt.Printf("更新已下载到 %s.new\n", execPath)
		fmt.Printf("请运行以下命令完成更新：\n  start %s\n", swapScript)
	} else {
		oldPath := execPath + ".old"
		os.Remove(oldPath)
		if err := os.Rename(execPath, oldPath); err != nil {
			return fmt.Errorf("备份当前版本失败 (%w)", err)
		}
		newData, err := os.ReadFile(newBinary)
		if err != nil {
			os.Rename(oldPath, execPath)
			return fmt.Errorf("读取新二进制失败 (%w)", err)
		}
		if err := os.WriteFile(execPath, newData, 0755); err != nil {
			os.Rename(oldPath, execPath)
			return fmt.Errorf("写入新版本失败 (%w)", err)
		}
		os.Remove(oldPath)
		fmt.Printf("更新完成：%s → %s\n", current, latest)
	}

	return nil
}

func extractZip(path, dest string) error {
	if runtime.GOOS == "windows" {
		cmd := exec.Command("powershell", "-Command",
			"Expand-Archive -Path \""+path+"\" -DestinationPath \""+dest+"\" -Force")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("Expand-Archive: %s", string(out))
		}
		return nil
	}
	cmd := exec.Command("unzip", "-q", path, "-d", dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("unzip: %s", string(out))
	}
	return nil
}

func extractTarGz(path, dest string) error {
	cmd := exec.Command("tar", "-xzf", path, "-C", dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tar: %s", string(out))
	}
	return nil
}

// RunBrowse generates the wiki if needed and opens it in a browser.
func RunBrowse(cfg *Config) error {
	if cfg.OutputDir == "" {
		if cfg.SourceDir == "" || cfg.SourceDir == "." {
			cfg.SourceDir = "."
		}
		cfg.OutputDir = filepath.Join(cfg.SourceDir, ".codewiki", "wiki")
	}
	indexPath := filepath.Join(cfg.OutputDir, "index.html")

	// Auto-generate if wiki doesn't exist
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		fmt.Println("Wiki 尚未生成，正在自动生成...")
		genCfg := &Config{
			SourceDir:   cfg.SourceDir,
			OutputDir:   cfg.OutputDir,
			ProjectName: cfg.ProjectName,
		}
		if err := RunGenerate(genCfg); err != nil {
			return fmt.Errorf("生成 wiki 失败: %w", err)
		}
	}

	absPath, _ := filepath.Abs(indexPath)
	fmt.Printf("正在浏览器中打开 %s\n", absPath)
	return openBrowser("file://" + absPath)
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
