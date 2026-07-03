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
	PDFOutputPath   string // for export pdf command
	Concurrency     int // A6: LLM concurrency for function-description stage (0 = default 10)
	MaxModules      int // B1: max modules for LLM responsibility cards: -1=auto, 0=skip, N=cap
}

// RunGenerate executes the full generate pipeline: parse → graph → diagram → doc.
func RunGenerate(cfg *Config) error {
	if cfg.ProjectName == "" {
		cfg.ProjectName = filepath.Base(cfg.SourceDir)
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = filepath.Join(cfg.SourceDir, ".codewiki", "wiki")
	}
	// A6: allow caller to tune LLM concurrency for the function-description stage.
	if cfg.Concurrency > 0 {
		docgen.FuncDescConcurrency = cfg.Concurrency
	}
	// B1: -max-modules controls LLM module responsibility cards.
	if cfg.MaxModules != 0 {
		docgen.MaxLLMModules = cfg.MaxModules
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
	if cfg.Language == "" {
		cfg.Language = detectLanguageFromPaths(paths)
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
	// A3: only an explicit --force discards the wiki checkpoint. Incremental
	// file changes are resumed fine-grained inside docgen (A2: only functions
	// without a cached description are re-requested). Clearing on every change
	// would discard hundreds of cached descriptions in large repos and force a
	// full LLM re-run on every single-file edit.
	if cfg.Force {
		docgen.ClearWikiCheckpoint(cfg.SourceDir)
		fmt.Println("--force 已指定，强制重新解析所有文件并重新生成")
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
		// Auto-detect language from source files when still unknown
		if v.language == "" && v.sourceDir != "" {
			if srcPaths, err := analyzer.WalkSourceFiles(v.sourceDir, ""); err == nil {
				v.language = detectLanguageFromPaths(srcPaths)
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

	// RAG API endpoint (used by the right-panel AI tab)
	if rawPath == "/api/ask" && r.Method == http.MethodPost {
		h.handleAskAPI(w, r)
		return
	}

	// Semantic search endpoint (B2): hybrid retrieval over wiki chunks.
	if rawPath == "/api/search" && r.Method == http.MethodGet {
		h.handleSearchAPI(w, r)
		return
	}

	if rawPath == "/api/source" && h.sourceDir != "" {
		h.handleSourceAPI(w, r)
		return
	}

	if rawPath == "/" {
		indexPath := filepath.Join(h.root, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			data, readErr := os.ReadFile(indexPath)
			if readErr != nil {
				http.Error(w, "读取文件出错", http.StatusInternalServerError)
				return
			}
			// Inject right panel into pre-generated index.html.
			// Ensure window.openSource exists (old static exports have openSource
			// inside an IIFE but not exposed globally).
			html := string(data)
			if !strings.Contains(html, "window.openSource") {
				html = strings.Replace(html, "</body>", docgen.SourcePopupJS+"\n</body>", 1)
			}
			w.Write(injectRightPanel([]byte(html)))
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
		body := docgen.RenderMarkdownWithSources(data, h.language)
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

	if ext == ".html" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		injected := string(data)
		if !strings.Contains(injected, "openSource") {
			injected = strings.Replace(injected, "</body>", docgen.SourcePopupJS+"\n</body>", 1)
		}
		w.Write(injectRightPanel([]byte(injected)))
		return
	}

	w.Header().Set("Content-Type", contentTypeFor(path))
	w.Write(data)
}

// handleSearchAPI serves /api/search (B2): hybrid retrieval over wiki chunks.
// Semantic recall (0.6) + literal match (0.4) + exact-symbol boost (0.2).
// Returns []searchHit as JSON. On engine/embedding failure returns an empty
// list so the front-end falls back to client-side indexOf.
func (h *serverHandler) handleSearchAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, []searchHit{})
		return
	}
	if h.engine == nil {
		writeJSON(w, []searchHit{})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	hits, err := h.engine.Search(ctx, q, 20)
	if err != nil {
		writeJSON(w, []searchHit{})
		return
	}

	ql := strings.ToLower(q)
	out := make([]searchHit, 0, len(hits))
	for _, s := range hits {
		titleLower := strings.ToLower(s.Title)
		pathLower := strings.ToLower(s.Path)
		literal := 0.0
		if strings.Contains(titleLower, ql) || strings.Contains(pathLower, ql) {
			literal = 1.0
		}
		exact := 0.0
		if titleLower == ql {
			exact = 1.0
		}
		mixed := mixScore(s.Score, literal, exact)
		out = append(out, searchHit{
			Type:    s.Type,
			Title:   s.Title,
			Path:    s.Path,
			Snippet: s.Snippet,
			Score:   mixed,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	// Cap to 20 after re-sort.
	if len(out) > 20 {
		out = out[:20]
	}
	writeJSON(w, out)
}

type searchHit struct {
	Type    string  `json:"type"`
	Title   string  `json:"title"`
	Path    string  `json:"path"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score"`
}

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, `{"error":"encode failed"}`, http.StatusInternalServerError)
	}
}

// mixScore combines the three retrieval signals for /api/search (B2):
//   semantic (vector similarity)  weight 0.6
//   literal  (title/path contains query)  weight 0.4
//   exact    (title == query, precise symbol hit)  weight 0.2 boost
func mixScore(semantic, literal, exact float64) float64 {
	return 0.6*semantic + 0.4*literal + 0.2*exact
}

// handleAskAPI processes a Q&A request and returns a JSON answer (or SSE stream).
func (h *serverHandler) handleAskAPI(w http.ResponseWriter, r *http.Request) {
	if h.engine == nil {
		http.Error(w, `{"error":"RAG 引擎未启用"}`, http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Question string `json:"question"`
		Stream   bool   `json:"stream"`
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

	if req.Stream {
		// SSE streaming response
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, `{"error":"不支持流式传输"}`, http.StatusInternalServerError)
			return
		}
		for token := range textCh {
			data, _ := json.Marshal(map[string]string{"type": "token", "text": token})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
		sourcesJSON, _ := json.Marshal(ans.Sources)
		fmt.Fprintf(w, "data: {\"type\":\"done\",\"sources\":%s}\n\n", sourcesJSON)
		flusher.Flush()
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
	absSource, _ := filepath.Abs(sourceDir)
	meta := serveMeta{SourceDir: absSource, Language: language}
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

// detectLanguageFromPaths returns all languages with ≥15% file share,
// joined by "、" for prompt neutrality. For single-language projects (one
// language ≥85%) the sole language name is returned. Returns "" if no
// supported source extension is found.
func detectLanguageFromPaths(paths []string) string {
	extToLang := map[string]string{
		".py": "python", ".go": "go",
		".ts": "typescript", ".tsx": "typescript",
		".js": "javascript", ".jsx": "javascript",
		".rs": "rust", ".java": "java",
		".cpp": "cpp", ".c": "c",
		".rb": "ruby", ".php": "php",
		".swift": "swift", ".kt": "kotlin",
	}
	// Count by language name (merge .ts/.tsx → typescript, etc.)
	langCounts := map[string]int{}
	total := 0
	for _, p := range paths {
		ext := strings.ToLower(filepath.Ext(p))
		lang, ok := extToLang[ext]
		if !ok {
			continue
		}
		langCounts[lang]++
		total++
	}
	if total == 0 {
		return ""
	}

	type lc struct {
		lang  string
		count int
	}
	var ranked []lc
	for lang, count := range langCounts {
		ranked = append(ranked, lc{lang, count})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].count > ranked[j].count })

	// If one language dominates (≥85%), return just that name — the project is
	// effectively single-language.
	if pct := float64(ranked[0].count) * 100 / float64(total); pct >= 85 {
		return ranked[0].lang
	}

	// Multi-language: include every language ≥15%.
	var parts []string
	for _, r := range ranked {
		if pct := float64(r.count) * 100 / float64(total); pct >= 15 {
			parts = append(parts, fmt.Sprintf("%s（%d 个文件）", r.lang, r.count))
		}
	}
	if len(parts) == 0 {
		// Degenerate case: no language reaches 15% — return the top one.
		return ranked[0].lang
	}
	return strings.Join(parts, "、")
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
	// Try expanding "dir/name.ext" → "dir/name/name.ext" (LLM often
	// abbreviates package paths, e.g. "internal/analyzer.go" instead of
	// "internal/analyzer/analyzer.go").
	if !found {
		dir := filepath.Dir(cleanFile)
		base := filepath.Base(cleanFile)
		nameOnly := strings.TrimSuffix(base, filepath.Ext(base))
		if nameOnly != "" {
			candidate := filepath.Join(h.sourceDir, dir, nameOnly, base)
			if _, err := os.Stat(candidate); err == nil {
				basePath = candidate
				found = true
			}
		}
	}
	// Last resort: try just the base filename
	if !found {
		baseName := filepath.Base(cleanFile)
		basePath = filepath.Join(h.sourceDir, baseName)
		for _, ext := range exts {
			if _, err := os.Stat(basePath + ext); err == nil {
				basePath = basePath + ext
				found = true
				break
			}
		}
		if !found {
			if _, err := os.Stat(basePath); err == nil {
				found = true
			}
		}
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


// injectRightPanel injects the right-side search/AI panel into a pre-generated
// static HTML page so that it gains the same Ask AI experience as dynamically
// rendered pages. The original page structure is preserved.
func injectRightPanel(data []byte) []byte {
	html := string(data)

	// 1. CSS — inject before </style> (inside the existing <style> block)
	rpCSS := `
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
.topbar-search { max-width:560px; }
.topbar-search kbd { right:92px; }
.topbar-search .topbar-btn { position:absolute; right:10px; top:50%; transform:translateY(-50%); }
`
	html = strings.Replace(html, "</style>", rpCSS+"</style>", 1)

	// 2. Topbar — add Ask AI button inside the search bar
	askBtn := `<button onclick="togglePanel('ai')" class="topbar-btn primary" style="margin-left:4px"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z"/></svg>Ask AI</button>`
	html = strings.Replace(html, `<kbd>Ctrl+K</kbd>`, `<kbd>Ctrl+K</kbd>`+"\n"+askBtn, 1)

	// 3. Search box — make it open the right panel instead of the old overlay
	html = strings.Replace(html,
		`<input type="text" id="topbar-search-trigger" placeholder="搜索文章、模块..." readonly>`,
		`<input type="text" id="topbar-search-trigger" placeholder="搜索文章、模块..." readonly onclick="togglePanel('search')">`,
		1)

	// 4. Right panel HTML + JS — inject before </body>
	rpHTML := `<div class="right-panel" id="right-panel">
<div class="rp-header">
<button class="rp-tab active" data-tab="search" onclick="switchTab('search')">🔍 搜索</button>
<button class="rp-tab" data-tab="ai" onclick="switchTab('ai')">🤖 AI 问答</button>
<button class="rp-close" onclick="closePanel()" title="关闭">✕</button>
</div>
<div class="rp-body">
<div class="rp-pane rp-search-pane active" data-pane="search">
<input class="rp-search-input" type="text" id="rp-search-input" placeholder="搜索文章、模块..." oninput="rpFilterSearch(this.value)">
<div class="rp-results" id="rp-search-results"></div>
</div>
<div class="rp-pane rp-ai-pane" data-pane="ai">
<div class="rp-chat" id="rp-chat">
<div class="rp-welcome">
<div class="rp-welcome-icon">🤖</div>
<div class="rp-welcome-title">CodeWiki AI 助手</div>
<div class="rp-welcome-desc">基于项目代码库的 RAG 智能问答<br>输入问题开始对话</div>
</div>
</div>
<div class="rp-input-area">
<input type="text" id="rp-ai-input" placeholder="向代码库提问..." onkeydown="if(event.key==='Enter')rpAskSend()">
<button id="rp-ai-btn" onclick="rpAskSend()">发送</button>
</div>
</div>
</div>
</div>
<script>
// Build _navIdx from sidebar links for search
var _navIdx=[];
document.querySelectorAll('.nav-group-items a').forEach(function(a){
  var t=a.querySelector('.nav-title');
  if(t)_navIdx.push({f:a.getAttribute('href'),t:t.textContent});
});

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
  _navIdx.forEach(function(n){
    if(n.t.toLowerCase().indexOf(q)>=0||(n.f&&n.f.toLowerCase().indexOf(q)>=0)){
      html+='<a class="rp-result" href="'+n.f+'"><span class="rp-result-title">'+n.t+'</span></a>';
    }
  });
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
function rpNavToArticle(filename){
  var parts=filename.split(/[\/\\]/);
  var searchTerms=[filename.replace(/\\/g,'/')];
  if(parts.length>=2)searchTerms.push(parts.slice(-2).join('/'));
  if(parts.length>=1)searchTerms.push(parts[parts.length-1].replace(/\.[^.]+$/,''));
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

// Override Ctrl+K to open right panel instead of old overlay
document.addEventListener('keydown',function(e){
  if((e.ctrlKey||e.metaKey)&&e.key==='k'){e.preventDefault();togglePanel('search');}
  if(e.key==='Escape')closePanel();
},true);
</script>
`
	html = strings.Replace(html, "</body>", rpHTML+"</body>", 1)

	return []byte(html)
}

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
	current := strings.TrimPrefix(cfg.Version, "v")
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
		if err := exec.Command("cmd", "/c", "start", "/min", swapScript).Start(); err != nil {
			return fmt.Errorf("启动替换脚本失败 (%w)", err)
		}
		fmt.Println("更新文件已准备就绪，当前进程退出后将自动完成替换。")
		fmt.Println("请重新运行 codewiki 以使用新版本。")
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

// RunExportPDF exports the generated wiki as a PDF file.
// If the wiki has not been generated yet, it runs the generate pipeline first.
func RunExportPDF(cfg *Config) error {
	if cfg.SourceDir == "" {
		cfg.SourceDir = "."
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = filepath.Join(cfg.SourceDir, ".codewiki", "wiki")
	}
	if cfg.ProjectName == "" {
		cfg.ProjectName = filepath.Base(cfg.SourceDir)
	}

	indexPath := filepath.Join(cfg.OutputDir, "index.html")
	srcPDF := filepath.Join(cfg.OutputDir, "wiki.pdf")

	// If index.html missing, run full generate pipeline
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		fmt.Println("Wiki 尚未生成，正在自动生成...")
		genCfg := &Config{
			SourceDir:   cfg.SourceDir,
			OutputDir:   cfg.OutputDir,
			ProjectName: cfg.ProjectName,
			Language:    cfg.Language,
		}
		if err := RunGenerate(genCfg); err != nil {
			return fmt.Errorf("生成 wiki 失败: %w", err)
		}
	} else if _, err := os.Stat(srcPDF); os.IsNotExist(err) {
		// Wiki exists but PDF was deleted → regenerate PDF only from existing files
		fmt.Println("Wiki 已存在但 PDF 缺失，正在从已有文档重新生成 PDF...")
		wiki, err := docgen.LoadWikiFromDir(cfg.OutputDir)
		if err != nil {
			return fmt.Errorf("读取已有 wiki 失败: %w", err)
		}
		if wiki.ProjectName == "" {
			wiki.ProjectName = cfg.ProjectName
		}
		if err := docgen.GeneratePDFViaChrome(wiki, nil, srcPDF); err != nil {
			return fmt.Errorf("生成 PDF 失败: %w", err)
		}
	}

	data, err := os.ReadFile(srcPDF)
	if err != nil {
		return fmt.Errorf("读取 PDF 失败: %w", err)
	}

	outPath := cfg.PDFOutputPath
	if outPath == "" {
		outPath = cfg.ProjectName + ".pdf"
	}

	if err := os.WriteFile(outPath, data, 0644); err != nil {
		return fmt.Errorf("写入 PDF 失败: %w", err)
	}

	absOut, _ := filepath.Abs(outPath)
	fmt.Printf("PDF 已导出: %s\n", absOut)
	return nil
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
