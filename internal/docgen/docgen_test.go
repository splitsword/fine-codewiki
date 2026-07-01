package docgen

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/splitsword/fine-codewiki/internal/analyzer"
	"github.com/splitsword/fine-codewiki/internal/grapher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateOverviewMarkdown(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "models/user.py", Classes: []analyzer.ClassInfo{{Name: "User"}}},
		{Filename: "services/user_service.py", Classes: []analyzer.ClassInfo{{Name: "UserService"}}},
		{Filename: "utils/logger.py", Functions: []analyzer.FunctionInfo{{Name: "get_logger"}}},
	}
	graph := grapher.BuildGraph(files)

	md, err := GenerateOverviewMarkdown(graph, "python-basic")
	require.NoError(t, err)
	require.NotEmpty(t, md)

	// Should contain project name
	assert.Contains(t, md, "# python-basic")

	// Should contain stats
	assert.Contains(t, md, "模块")
	assert.Contains(t, md, "类")
	assert.Contains(t, md, "函数")

	// Should NOT contain flat module list (that belongs in project-structure.md)
	assert.NotContains(t, md, "## 模块")
}

func TestGenerateOverviewMarkdownWithRoles(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "main.py", Functions: []analyzer.FunctionInfo{{Name: "main"}}, Imports: []analyzer.ImportInfo{
			{Module: "services.api", Name: "Api"},
		}},
		{Filename: "services/api.py", Classes: []analyzer.ClassInfo{{Name: "Api"}}, Imports: []analyzer.ImportInfo{
			{Module: "models.user", Name: "User"},
			{Module: "utils.logger", Name: "get_logger"},
		}},
		{Filename: "models/user.py", Classes: []analyzer.ClassInfo{{Name: "User"}}},
		{Filename: "utils/logger.py", Functions: []analyzer.FunctionInfo{{Name: "get_logger"}}},
	}
	graph := grapher.BuildGraph(files)

	md, err := GenerateOverviewMarkdown(graph, "role-test")
	require.NoError(t, err)
	require.NotEmpty(t, md)

	// Should contain entry-point description
	assert.Contains(t, md, "项目入口点为")
	assert.Contains(t, md, "`main`")

	// Should NOT contain flat module list
	assert.NotContains(t, md, "## 模块")
}

func TestGenerateOverviewMarkdownEmptyProject(t *testing.T) {
	graph := grapher.BuildGraph([]*analyzer.FileResult{})
	md, err := GenerateOverviewMarkdown(graph, "empty-project")
	require.NoError(t, err)
	assert.Contains(t, md, "# empty-project")
	assert.Contains(t, md, "未在项目中找到模块")
}

func TestGenerateAPIReferenceMarkdown(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "models/user.py",
			Classes: []analyzer.ClassInfo{
				{
					Name:    "User",
					Bases:   []string{"BaseModel"},
					Methods: []analyzer.FunctionInfo{{Name: "authenticate", Params: []string{"self", "password"}, ReturnType: "bool"}},
				},
			},
		},
		{
			Filename:  "utils/logger.py",
			Functions: []analyzer.FunctionInfo{{Name: "get_logger", Params: []string{"name"}, ReturnType: "Logger"}},
		},
	}
	graph := grapher.BuildGraph(files)

	md, err := GenerateAPIReferenceMarkdown(graph, nil)
	require.NoError(t, err)
	require.NotEmpty(t, md)

	// Should contain API reference heading
	assert.Contains(t, md, "# API 参考")

	// Should contain class documentation
	assert.Contains(t, md, "## User")
	assert.Contains(t, md, "BaseModel")
	assert.Contains(t, md, "authenticate")

	// Should contain function documentation
	assert.Contains(t, md, "## get_logger")
	assert.Contains(t, md, "name")
	assert.Contains(t, md, "Logger")
}

func TestGenerateArchitectureMarkdown(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "models/user.py",
			Imports:  []analyzer.ImportInfo{{Module: ".base", IsRelative: true}},
		},
		{Filename: "models/base.py"},
		{
			Filename: "services/user_service.py",
			Imports: []analyzer.ImportInfo{
				{Module: "..models.user", IsRelative: true},
				{Module: "..utils.logger", IsRelative: true},
			},
		},
		{Filename: "utils/logger.py"},
	}
	graph := grapher.BuildGraph(files)

	md, err := GenerateArchitectureMarkdown(graph, "")
	require.NoError(t, err)
	require.NotEmpty(t, md)

	assert.Contains(t, md, "# 架构")
	assert.NotContains(t, md, "## 关键设计决策") // LLM 叙事生成，非静态追加
	assert.NotContains(t, md, "## 模块概览")
	assert.NotContains(t, md, "## 依赖图")
}

func TestWriteWikiFiles(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".codewiki", "wiki")

	wiki := &Wiki{
		ProjectName:         "test-project",
		Overview:            "# test-project\n\n项目概述。\n",
		APIReference:        "# API 参考\n\nAPI 内容。\n",
		Architecture:        "# 架构\n\n架构说明。\n\n```mermaid\ngraph TD\n    A --> B\n```\n",
		ClassDiagram:        "classDiagram\n",
		ArchitectureDiagram: "graph TD\n",
	}

	err := WriteWikiFiles(wikiDir, wiki, nil)
	require.NoError(t, err)

	// Verify files exist
	assert.FileExists(t, filepath.Join(wikiDir, "00-overview.md"))
	assert.FileExists(t, filepath.Join(wikiDir, "api-reference.md"))
	assert.FileExists(t, filepath.Join(wikiDir, "02-architecture.md"))
	assert.FileExists(t, filepath.Join(wikiDir, "compilation.md"))
	assert.FileExists(t, filepath.Join(wikiDir, "index.html"))
	assert.FileExists(t, filepath.Join(wikiDir, "wiki.pdf"))
	// Diagrams are embedded inside thematic articles; no standalone .mmd files
	assert.NoFileExists(t, filepath.Join(wikiDir, "class-diagram.mmd"))
	assert.NoFileExists(t, filepath.Join(wikiDir, "architecture.mmd"))
	assert.NoFileExists(t, filepath.Join(wikiDir, "sequence-diagram.mmd"))

	// Verify content with frontmatter
	content, err := os.ReadFile(filepath.Join(wikiDir, "00-overview.md"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "---")
	assert.Contains(t, string(content), `title: "项目概述"`)
	assert.Contains(t, string(content), `project: "test-project"`)
	assert.Contains(t, string(content), "source_modules:")
	assert.Contains(t, string(content), "# test-project\n\n项目概述。\n")

	// Verify compilation contains all sections
	compContent, err := os.ReadFile(filepath.Join(wikiDir, "compilation.md"))
	require.NoError(t, err)
	comp := string(compContent)
	assert.Contains(t, comp, "# test-project Wiki 合辑")
	assert.Contains(t, comp, "## 目录")
	assert.Contains(t, comp, "## test-project")
	assert.Contains(t, comp, "## 架构")
	assert.Contains(t, comp, "## API 参考")

	// Verify static HTML
	htmlContent, err := os.ReadFile(filepath.Join(wikiDir, "index.html"))
	require.NoError(t, err)
	html := string(htmlContent)
	assert.Contains(t, html, "<!DOCTYPE html>")
	assert.Contains(t, html, "test-project Wiki")
	assert.Contains(t, html, "<section id=\"overview\">")
	assert.Contains(t, html, "<section id=\"architecture\">")
	assert.Contains(t, html, "<section id=\"api-reference\">")
	assert.Contains(t, html, `href="#overview"`)
	assert.Contains(t, html, "项目概述")
	assert.Contains(t, html, `<div class="mermaid">`)
	assert.Contains(t, html, "graph TD")
}

func TestMarkdownEscape(t *testing.T) {
	assert.Equal(t, "foo\\_bar", markdownEscape("foo_bar"))
	assert.Equal(t, "foo\\*bar\\*", markdownEscape("foo*bar*"))
	assert.Equal(t, "foo\\_bar\\_", markdownEscape("foo_bar_"))
}

func TestGenerateWikiFromGraph(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "models/user.py",
			Classes: []analyzer.ClassInfo{
				{Name: "User", Bases: []string{"BaseModel"}},
			},
			Imports: []analyzer.ImportInfo{{Module: ".base", IsRelative: true}},
		},
		{Filename: "models/base.py", Classes: []analyzer.ClassInfo{{Name: "BaseModel"}}},
	}
	graph := grapher.BuildGraph(files)
	archDSL := "graph TD\n    models_user --> models_base\n"
	classDSL := "classDiagram\n    class User\n    class BaseModel\n    User --|> BaseModel\n"

	wiki, err := GenerateWiki(graph, "test-project", archDSL, classDSL, "sequenceDiagram\n")
	require.NoError(t, err)
	require.NotNil(t, wiki)

	assert.Equal(t, "test-project", wiki.ProjectName)
	assert.NotEmpty(t, wiki.Overview)
	assert.NotEmpty(t, wiki.APIReference)
	assert.NotEmpty(t, wiki.Architecture)
	assert.Equal(t, classDSL, wiki.ClassDiagram)
	assert.Equal(t, archDSL, wiki.ArchitectureDiagram)

	// Verify overview contains key info
	assert.Contains(t, wiki.Overview, "test-project")
	assert.Contains(t, wiki.Overview, "models/user")

	// Verify architecture doc contains top-level diagram and sub-system diagrams
	assert.Contains(t, wiki.Architecture, "```mermaid")
	assert.NotContains(t, wiki.Architecture, "## 关键设计决策") // LLM 叙事生成，非静态追加
	assert.NotContains(t, wiki.Architecture, "## 模块概览")
	assert.NotContains(t, wiki.Architecture, "## 依赖图")
}

// ---------- Mock LLM Tests ----------

type mockProvider struct {
	response  string
	err       error
	streamErr error
}

func (m *mockProvider) Complete(ctx context.Context, prompt string) (string, error) {
	return m.response, m.err
}

func (m *mockProvider) CompleteStream(ctx context.Context, prompt string) (<-chan string, error) {
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	ch := make(chan string, 1)
	go func() {
		defer close(ch)
		if m.response != "" {
			ch <- m.response
		}
	}()
	return ch, nil
}

func (m *mockProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, nil
}

func TestGenerateWikiWithLLM(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "models/user.py", Classes: []analyzer.ClassInfo{{Name: "User"}}},
	}
	graph := grapher.BuildGraph(files)

	mock := &mockProvider{response: "This is an AI-enhanced project overview."}
	wiki, err := GenerateWikiEnhanced(context.Background(), mock, graph, "", "ai-project", "graph TD\n", "classDiagram\n", "sequenceDiagram\n", "")

	require.NoError(t, err)
	require.NotNil(t, wiki)

	// Should contain LLM enhancement
	assert.Contains(t, wiki.Overview, "AI-enhanced project overview")
	// Should also contain static content
	assert.Contains(t, wiki.Overview, "ai-project")
}

func TestGenerateWikiWithLLMFallback(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "models/user.py", Classes: []analyzer.ClassInfo{{Name: "User"}}},
	}
	graph := grapher.BuildGraph(files)

	// LLM returns error — should fall back to static generation
	mock := &mockProvider{err: errors.New("llm unavailable")}
	wiki, err := GenerateWikiEnhanced(context.Background(), mock, graph, "", "fallback-project", "graph TD\n", "classDiagram\n", "sequenceDiagram\n", "")

	require.NoError(t, err)
	require.NotNil(t, wiki)

	// Should still contain static content, no panic
	assert.Contains(t, wiki.Overview, "fallback-project")
	// Should NOT start with YAML frontmatter (addFrontmatter is applied later in WriteWikiFiles)
	assert.False(t, strings.HasPrefix(wiki.Overview, "---"))
}

func TestGenerateWikiEnhancedWithMaxFunctions(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "models/user.py", Classes: []analyzer.ClassInfo{{Name: "User"}}},
	}
	graph := grapher.BuildGraph(files)

	mock := &mockProvider{response: "This is an AI-enhanced project overview."}

	// maxLLMFunctions = -1 (auto) should behave the same as GenerateWikiEnhanced
	wiki, err := GenerateWikiEnhancedWithMaxFunctions(context.Background(), mock, graph, "", "test-project", "graph TD\n", "classDiagram\n", "sequenceDiagram\n", "", -1)
	require.NoError(t, err)
	require.NotNil(t, wiki)
	assert.Contains(t, wiki.Overview, "AI-enhanced project overview")

	// maxLLMFunctions = 0 should skip function-level LLM enhancement but still allow overview enhancement
	wiki2, err := GenerateWikiEnhancedWithMaxFunctions(context.Background(), mock, graph, "", "test-project", "graph TD\n", "classDiagram\n", "sequenceDiagram\n", "", 0)
	require.NoError(t, err)
	require.NotNil(t, wiki2)
	assert.Contains(t, wiki2.Overview, "AI-enhanced project overview")
}

// recordingProvider captures every prompt sent to the LLM and delegates
// response generation to a configurable function. Used to verify which
// functions the description stage actually requests.
type recordingProvider struct {
	mu      sync.Mutex
	prompts []string
	respFn  func(prompt string) string
}

func (r *recordingProvider) Complete(_ context.Context, prompt string) (string, error) {
	r.mu.Lock()
	r.prompts = append(r.prompts, prompt)
	r.mu.Unlock()
	if r.respFn != nil {
		return r.respFn(prompt), nil
	}
	return "", nil
}

func (r *recordingProvider) CompleteStream(_ context.Context, prompt string) (<-chan string, error) {
	r.mu.Lock()
	r.prompts = append(r.prompts, prompt)
	r.mu.Unlock()
	ch := make(chan string, 1)
	go func() {
		defer close(ch)
		if r.respFn != nil {
			if s := r.respFn(prompt); s != "" {
				ch <- s
			}
		}
	}()
	return ch, nil
}

func (r *recordingProvider) Embed(_ context.Context, _ []string) ([][]float32, error) {
	return nil, nil
}

// funcDescPrompts returns only the prompts that target function descriptions.
func (r *recordingProvider) funcDescPrompts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, p := range r.prompts {
		if strings.Contains(p, "撰写深度语义分析") {
			out = append(out, p)
		}
	}
	return out
}

func TestPendingFuncs(t *testing.T) {
	top := []funcRef{
		{Module: "mod1", Name: "fnA"},
		{Module: "mod1", Name: "fnB"},
		{Module: "mod2", Name: "fnC"},
		{Module: "mod2", Name: "fnD"},
	}

	tests := []struct {
		name        string
		checkpoint  map[string]string
		wantPending []string // expected module#name keys in pending
	}{
		{
			name:        "empty checkpoint returns all",
			checkpoint:  nil,
			wantPending: []string{"mod1#fnA", "mod1#fnB", "mod2#fnC", "mod2#fnD"},
		},
		{
			name:        "fully covered returns none",
			checkpoint:  map[string]string{"mod1#fnA": "x", "mod1#fnB": "x", "mod2#fnC": "x", "mod2#fnD": "x"},
			wantPending: nil,
		},
		{
			name:        "partial checkpoint returns only missing",
			checkpoint:  map[string]string{"mod1#fnA": "cached", "mod2#fnC": "cached"},
			wantPending: []string{"mod1#fnB", "mod2#fnD"},
		},
		{
			name:        "stale checkpoint entries (not in topFuncs) are ignored",
			checkpoint:  map[string]string{"mod1#fnA": "x", "mod9#ghost": "stale", "mod1#renamed": "stale"},
			wantPending: []string{"mod1#fnB", "mod2#fnC", "mod2#fnD"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pendingFuncs(top, tc.checkpoint)
			var gotKeys []string
			for _, f := range got {
				gotKeys = append(gotKeys, funcDescKey(f))
			}
			assert.ElementsMatch(t, tc.wantPending, gotKeys)
		})
	}
}

// TestFunctionDescCheckpointPartialResume verifies A2: when a checkpoint
// already holds descriptions for some functions, only the remaining (pending)
// functions are sent to the LLM, and cached descriptions are preserved.
func TestFunctionDescCheckpointPartialResume(t *testing.T) {
	tmpDir := t.TempDir()

	files := []*analyzer.FileResult{
		{Filename: "mod1.go", Functions: []analyzer.FunctionInfo{
			{Name: "ckptFuncA"},
			{Name: "ckptFuncB"},
			{Name: "newFuncC"},
			{Name: "newFuncD"},
		}},
	}
	graph := grapher.BuildGraph(files)

	// Pre-seed checkpoint: ckptFuncA/ckptFuncB already have descriptions.
	cpDir := filepath.Join(tmpDir, ".codewiki", "checkpoint")
	require.NoError(t, os.MkdirAll(cpDir, 0755))
	cpJSON := `{
		"func_desc_map": {
			"mod1#ckptFuncA": "CACHED_DESC_A",
			"mod1#ckptFuncB": "CACHED_DESC_B"
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(cpDir, "wiki.json"), []byte(cpJSON), 0644))

	rec := &recordingProvider{
		respFn: func(prompt string) string {
			if strings.Contains(prompt, "撰写深度语义分析") {
				// Respond for whichever pending functions are in this batch.
				return "newFuncC: NEW_DESC_C\nnewFuncD: NEW_DESC_D"
			}
			return "" // other prompts (overview etc.) → static fallback
		},
	}

	wiki, err := GenerateWikiEnhancedWithMaxFunctions(context.Background(), rec, graph, tmpDir, "test-ckpt", "graph TD\n", "classDiagram\n", "sequenceDiagram\n", "", -1)
	require.NoError(t, err)
	require.NotNil(t, wiki)

	// Only pending functions should appear in function-description prompts.
	fdPrompts := rec.funcDescPrompts()
	require.NotEmpty(t, fdPrompts, "function-description stage should still run for pending funcs")
	combined := strings.Join(fdPrompts, "\n")
	assert.Contains(t, combined, "newFuncC")
	assert.Contains(t, combined, "newFuncD")
	assert.NotContains(t, combined, "ckptFuncA", "checkpointed func must not be re-requested")
	assert.NotContains(t, combined, "ckptFuncB", "checkpointed func must not be re-requested")

	// Final API reference must contain BOTH cached and freshly-generated descriptions.
	apiRef := wiki.APIReference
	assert.Contains(t, apiRef, "CACHED_DESC_A", "cached description must be preserved")
	assert.Contains(t, apiRef, "CACHED_DESC_B")
	assert.Contains(t, apiRef, "NEW_DESC_C", "newly generated description must appear")
	assert.Contains(t, apiRef, "NEW_DESC_D")
}

func TestGenerateKeyConceptsFallback(t *testing.T) {
	// Build graph with edges so InferModuleRoles produces meaningful roles.
	files := []*analyzer.FileResult{
		{Filename: "core/engine.py", Classes: []analyzer.ClassInfo{{Name: "Engine"}}, Imports: []analyzer.ImportInfo{{Module: "utils.helpers"}}},
		{Filename: "api/server.py", Functions: []analyzer.FunctionInfo{{Name: "run_server"}}, Imports: []analyzer.ImportInfo{{Module: "utils.helpers"}, {Module: "core.engine"}}},
		{Filename: "utils/helpers.py", Functions: []analyzer.FunctionInfo{{Name: "helper"}}},
	}
	graph := grapher.BuildGraph(files)

	md := GenerateKeyConceptsFallback(graph, "test-project")
	require.NotEmpty(t, md)
	assert.Contains(t, md, "# test-project 关键设计决策")
	assert.Contains(t, md, "模块职责分层")
	assert.Contains(t, md, "核心领域模块")
	assert.Contains(t, md, "`utils/helpers`")
	assert.Contains(t, md, "入口层模块")
	assert.Contains(t, md, "`api/server`")
	assert.Contains(t, md, "关键抽象与依赖流向")
	assert.Contains(t, md, "入口与交互模式")
}

func TestGenerateKeyConceptsFallbackEmpty(t *testing.T) {
	graph := grapher.BuildGraph([]*analyzer.FileResult{})
	md := GenerateKeyConceptsFallback(graph, "empty")
	assert.Empty(t, md)
}

func TestGenerateWikiWithLLMKeyConceptsFallback(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "core/engine.py", Classes: []analyzer.ClassInfo{{Name: "Engine"}}},
		{Filename: "api/server.py", Functions: []analyzer.FunctionInfo{{Name: "run_server"}}, Imports: []analyzer.ImportInfo{{Module: "core.engine"}}},
	}
	graph := grapher.BuildGraph(files)

	// LLM returns error — keyConcepts should fall back to static analysis
	mock := &mockProvider{err: errors.New("llm unavailable")}
	wiki, err := GenerateWikiEnhanced(context.Background(), mock, graph, "", "fallback-project", "graph TD\n", "classDiagram\n", "sequenceDiagram\n", "")

	require.NoError(t, err)
	require.NotNil(t, wiki)
	assert.NotEmpty(t, wiki.KeyConcepts)
	assert.Contains(t, wiki.KeyConcepts, "关键设计决策")
	assert.Contains(t, wiki.KeyConcepts, "模块职责分层")
}

func TestGenerateWikiWithLLMLearningPathSuccess(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "app.py", Functions: []analyzer.FunctionInfo{{Name: "main"}}},
		{Filename: "lib/utils.py", Functions: []analyzer.FunctionInfo{{Name: "helper"}}},
		{Filename: "lib/config.py", Functions: []analyzer.FunctionInfo{{Name: "load"}}},
	}
	graph := grapher.BuildGraph(files)

	mock := &mockProvider{response: "## 快速上手\n\n第一步，阅读 app.py 了解入口。\n\n## 深度理解\n\n第二步，理解整体架构。"}
	wiki, err := GenerateWikiEnhanced(context.Background(), mock, graph, "", "test-project", "graph TD\n", "classDiagram\n", "sequenceDiagram\n", "")

	require.NoError(t, err)
	require.NotNil(t, wiki)
	assert.Contains(t, wiki.LearningPath, "test-project 学习路径")
	assert.Contains(t, wiki.LearningPath, "快速上手")
	assert.Contains(t, wiki.LearningPath, "app.py")
}

func TestGenerateWikiWithLLMLearningPathFallback(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "app.py", Functions: []analyzer.FunctionInfo{{Name: "main"}}},
	}
	graph := grapher.BuildGraph(files)

	mock := &mockProvider{err: errors.New("llm unavailable")}
	wiki, err := GenerateWikiEnhanced(context.Background(), mock, graph, "", "fallback-project", "graph TD\n", "classDiagram\n", "sequenceDiagram\n", "")

	require.NoError(t, err)
	require.NotNil(t, wiki)
	assert.Contains(t, wiki.LearningPath, "fallback-project 学习路径")
	assert.Contains(t, wiki.LearningPath, "快速上手")
}

func TestGenerateOverviewMarkdownSingleFile(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "app.py",
			Classes: []analyzer.ClassInfo{
				{Name: "App", Methods: []analyzer.FunctionInfo{{Name: "run", Params: []string{"self"}}}},
			},
			Functions: []analyzer.FunctionInfo{{Name: "main", Params: []string{}, ReturnType: "int"}},
		},
	}
	graph := grapher.BuildGraph(files)

	md, err := GenerateOverviewMarkdown(graph, "single-file-app")
	require.NoError(t, err)

	assert.Contains(t, md, "# single-file-app")
	assert.Contains(t, md, "**模块**")
	assert.Contains(t, md, "**类**")
	assert.Contains(t, md, "**函数**")
	assert.Contains(t, md, "app")
}

func TestGenerateWikiEmptyRepo(t *testing.T) {
	graph := grapher.BuildGraph([]*analyzer.FileResult{})
	wiki, err := GenerateWiki(graph, "empty", "graph TD\n", "classDiagram\n", "sequenceDiagram\n")

	require.NoError(t, err)
	require.NotNil(t, wiki)

	assert.Contains(t, wiki.Overview, "empty")
	assert.Contains(t, wiki.Overview, "未在项目中找到模块")
	assert.Contains(t, wiki.APIReference, "未找到 API 符号")
	assert.Contains(t, wiki.Architecture, "# 架构")
}

func TestDescribeFunction(t *testing.T) {
	tests := []struct {
		name       string
		params     []string
		returnType string
		want       string
	}{
		{"get_user", []string{"user_id"}, "User", "获取user，参数：user_id，返回 User"},
		{"create_order", []string{"items", "user_id"}, "Order", "创建order，参数：items, user_id，返回 Order"},
		{"validate_email", []string{"email"}, "bool", "验证email，参数：email，返回 bool"},
		{"parse_config", []string{"path"}, "dict", "解析config，参数：path，返回 dict"},
		{"send_email", []string{"to", "subject", "body"}, "None", "发送email，参数：to, subject, body"},
		{"authenticate", []string{"username", "password"}, "bool", "用户认证，验证身份凭据"},
		{"__init__", []string{"self", "name"}, "None", "构造函数，初始化对象属性"},
		{"to_dict", []string{}, "dict", "序列化对象为结构化数据"},
		{"is_active", []string{}, "bool", "判断active 状态，返回 bool"},
		{"process_payment", []string{"amount"}, "Receipt", "处理payment，参数：amount，返回 Receipt"},
		{"main", []string{}, "int", "程序入口，执行主逻辑"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describeFunction(tt.name, tt.params, tt.returnType)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDescribeFunctionUnknown(t *testing.T) {
	// 有参数时走默认推断路径
	desc := describeFunction("foobar", []string{"x"}, "")
	assert.Contains(t, desc, "foobar")
	assert.Contains(t, desc, "执行")
}

func TestDescribeFunctionEmpty(t *testing.T) {
	desc := describeFunction("placeholder", []string{}, "")
	assert.Equal(t, "占位函数，待实现具体逻辑", desc)

	desc = describeFunction("placeholder", []string{}, "None")
	assert.Equal(t, "占位函数，待实现具体逻辑", desc)

	desc = describeFunction("placeholder", []string{}, "void")
	assert.Equal(t, "占位函数，待实现具体逻辑", desc)
}

func TestDescribeFunctionAbstract(t *testing.T) {
	desc := describeFunction("abstract_process", []string{}, "")
	assert.Equal(t, "抽象方法，需子类实现", desc)

	desc = describeFunction("AbstractHandler", []string{}, "void")
	assert.Equal(t, "抽象方法，需子类实现", desc)
}

func TestGenerateAPIReferenceWithDescriptions(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "utils/logger.py",
			Functions: []analyzer.FunctionInfo{
				{Name: "get_logger", Params: []string{"name"}, ReturnType: "Logger"},
				{Name: "configure", Params: []string{"level"}, ReturnType: "None"},
			},
		},
	}
	graph := grapher.BuildGraph(files)

	md, err := GenerateAPIReferenceMarkdown(graph, nil)
	require.NoError(t, err)

	// 应该包含函数签名
	assert.Contains(t, md, "get_logger(name)")
	assert.Contains(t, md, "configure(level)")

	// 应该包含静态语义描述
	assert.Contains(t, md, "获取logger")
	assert.Contains(t, md, "初始化系统或配置环境")
}

func TestAdjustHeadingLevel(t *testing.T) {
	input := "# 标题\n## 子标题\n### 三级\n正文内容"

	// shift=0 应该保持不变
	assert.Equal(t, input, adjustHeadingLevel(input, 0))

	// shift=1 应该将所有标题降级一级
	shifted := adjustHeadingLevel(input, 1)
	assert.Contains(t, shifted, "## 标题")
	assert.Contains(t, shifted, "### 子标题")
	assert.Contains(t, shifted, "#### 三级")
	assert.Contains(t, shifted, "正文内容")

	// 不超过六级标题
	six := "###### 最深"
	assert.Equal(t, "###### 最深", adjustHeadingLevel(six, 1))
}

func TestGenerateMarkdownCompilation(t *testing.T) {
	wiki := &Wiki{
		ProjectName:         "demo",
		Overview:            "# demo\n\n项目概述内容。\n",
		Architecture:        "# 架构\n\n架构说明内容。\n\n```mermaid\ngraph TD\n    A --> B\n```\n",
		KeyConcepts:         "核心概念内容。\n\n## 类型关系图\n\n下图展示了项目中核心类与接口的继承和组合关系：\n\n```mermaid\nclassDiagram\n    class A\n```\n",
		LearningPath:        "学习路径内容。\n\n## 关键调用流程\n\n下图展示了系统中一条典型调用链的交互顺序：\n\n> 触发条件：调用 A 的主流程执行；最终由 B 完成数据查询\n\n```mermaid\nsequenceDiagram\n    A->>B: msg\n```\n",
		APIReference:        "# API 参考\n\nAPI 内容。\n",
		ArchitectureDiagram: "graph TD\n    A --> B\n",
		ClassDiagram:        "classDiagram\n    class A\n",
		SequenceDiagram:     "sequenceDiagram\n    A->>B: msg\n",
		SequenceDescription: "触发条件：调用 A 的主流程执行；最终由 B 完成数据查询",
	}

	comp := GenerateMarkdownCompilation(wiki)

	// 应该包含合辑标题
	assert.Contains(t, comp, "# demo Wiki 合辑")

	// 应该包含目录
	assert.Contains(t, comp, "## 目录")
	assert.Contains(t, comp, "1. [项目概述](#项目概述)")
	assert.Contains(t, comp, "2. [项目能做什么](#项目能做什么)")
	assert.Contains(t, comp, "3. [架构说明](#架构说明)")
	assert.Contains(t, comp, "4. [项目结构](#项目结构)")
	assert.Contains(t, comp, "6. [学习路径](#学习路径)")
	assert.Contains(t, comp, "7. [API 参考](#api-参考)")

	// 子文档标题应该被降级一级
	assert.Contains(t, comp, "## demo")
	assert.Contains(t, comp, "## 架构")
	assert.Contains(t, comp, "## API 参考")

	// 不应该还有一级标题（因为已经被降级）
	// 只允许合辑标题是 # 级别
	lines := strings.Split(comp, "\n")
	level1Count := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "# ") {
			level1Count++
		}
	}
	assert.Equal(t, 1, level1Count, "合辑中只允许一个一级标题")

	// 图表已嵌入到对应主题文章中，不再作为独立章节
	assert.NotContains(t, comp, "## 架构图")
	assert.NotContains(t, comp, "## 类图")
	assert.NotContains(t, comp, "## 时序图")
	// 但 mermaid 代码块应通过主题文章内嵌出现
	assert.Contains(t, comp, "```mermaid")

	// 时序图场景描述已嵌入 learning-path
	assert.Contains(t, comp, "> 触发条件：调用 A 的主流程执行；最终由 B 完成数据查询")
}

func TestGenerateStaticHTML(t *testing.T) {
	wiki := &Wiki{
		ProjectName:         "html-demo",
		Overview:            "# html-demo\n\n项目概述内容。\n",
		Architecture:        "# 架构\n\n架构说明内容。\n\n```mermaid\ngraph TD\n    A --> B\n```\n",
		KeyConcepts:         "核心概念内容。\n\n## 类型关系图\n\n下图展示了项目中核心类与接口的继承和组合关系：\n\n```mermaid\nclassDiagram\n    class A\n```\n",
		LearningPath:        "学习路径内容。\n\n## 关键调用流程\n\n下图展示了系统中一条典型调用链的交互顺序：\n\n> 触发条件：调用 A 的主流程执行；最终由 B 完成数据查询\n\n```mermaid\nsequenceDiagram\n    A->>B: msg\n```\n",
		APIReference:        "# API 参考\n\nAPI 内容。\n",
		ArchitectureDiagram: "graph TD\n    A --> B\n",
		ClassDiagram:        "classDiagram\n    class A\n",
		SequenceDiagram:     "sequenceDiagram\n    A->>B: msg\n",
		SequenceDescription: "触发条件：调用 A 的主流程执行；最终由 B 完成数据查询",
	}

	html := GenerateStaticHTML(wiki, nil)

	// 基本结构
	assert.Contains(t, html, "<!DOCTYPE html>")
	assert.Contains(t, html, `<html lang="zh-CN" data-theme="light">`)
	assert.Contains(t, html, "html-demo Wiki")

	// 导航锚点（新版含图标 span）
	assert.Contains(t, html, `href="#overview"`)
	assert.Contains(t, html, "项目概述")
	assert.Contains(t, html, `href="#architecture"`)
	assert.Contains(t, html, "架构说明")
	assert.Contains(t, html, `href="#api-reference"`)
	assert.Contains(t, html, "API 参考")
	// 图表已嵌入对应主题，不再作为独立导航项
	assert.NotContains(t, html, `<a href="#architecture-diagram">架构图</a>`)
	assert.NotContains(t, html, `<a href="#class-diagram">类图</a>`)
	assert.NotContains(t, html, `<a href="#sequence-diagram">时序图</a>`)

	// 内容区块
	assert.Contains(t, html, `<section id="overview">`)
	assert.Contains(t, html, `<section id="architecture">`)
	assert.Contains(t, html, `<section id="api-reference">`)
	// 图表已嵌入对应主题 section，不再作为独立 section
	assert.NotContains(t, html, `<section id="architecture-diagram">`)
	assert.NotContains(t, html, `<section id="class-diagram">`)
	assert.NotContains(t, html, `<section id="sequence-diagram">`)

	// Markdown 已渲染为 HTML
	assert.Contains(t, html, `<h1 id="html-demo">html-demo</h1>`)
	assert.Contains(t, html, `<h1 id="架构">架构</h1>`)
	assert.Contains(t, html, `<h1 id="API-参考">API 参考</h1>`)

	// Mermaid 图表嵌入
	assert.Contains(t, html, `<div class="mermaid">`)
	assert.Contains(t, html, "graph TD")
	assert.Contains(t, html, "classDiagram")
	assert.Contains(t, html, "sequenceDiagram")

	// Mermaid.js CDN
	assert.Contains(t, html, "cdn.jsdelivr.net/npm/mermaid")

	// Mermaid interactive configuration
	assert.Contains(t, html, "securityLevel:'loose'", "should enable loose security for click handlers")
	assert.Contains(t, html, "window.navigateToModule", "should inject navigateToModule helper")

	// 时序图场景描述已嵌入 learning-path section（通过 Markdown blockquote 渲染）
	assert.Contains(t, html, "<blockquote>触发条件：调用 A 的主流程执行；最终由 B 完成数据查询</blockquote>")
}

func TestGeneratePDF(t *testing.T) {
	wiki := &Wiki{
		ProjectName:         "pdf-test",
		Overview:            "# 概述\n\n这是一个测试项目。\n",
		Architecture:        "## 架构\n\n模块A -> 模块B\n",
		APIReference:        "## API\n\n`getUser()`\n",
		ArchitectureDiagram: "graph TD\n    A --> B\n",
		ClassDiagram:        "classDiagram\n    class A\n",
		SequenceDiagram:     "sequenceDiagram\n    A->>B: msg\n",
		SequenceDescription: "触发条件：调用 A 的主流程执行；最终由 B 完成数据查询",
	}

	pdfBytes, err := GeneratePDF(wiki)
	if err != nil {
		if strings.Contains(err.Error(), "无法加载中文字体") || strings.Contains(err.Error(), "未找到系统 CJK 字体") {
			t.Skip("跳过 PDF 测试：系统未安装 CJK 字体")
		}
		t.Fatalf("生成 PDF 失败: %v", err)
	}

	require.NotNil(t, pdfBytes)
	assert.Greater(t, len(pdfBytes), 0)
	assert.True(t, bytes.HasPrefix(pdfBytes, []byte("%PDF")))
}

func TestLanguagePromptHint(t *testing.T) {
	tests := []struct {
		lang string
		want string
	}{
		{"python", "Python"},
		{"go", "Go"},
		{"golang", "Go"},
		{"javascript", "JavaScript"},
		{"js", "JavaScript"},
		{"typescript", "JavaScript"},
		{"ts", "JavaScript"},
		{"java", "Java"},
		{"rust", "Rust"},
		{"c++", "C++"},
		{"cpp", "C++"},
		{"", ""},
		{"unknown", ""},
	}
	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			got := languagePromptHint(tt.lang)
			if tt.want == "" {
				assert.Equal(t, "", got)
			} else {
				assert.Contains(t, got, tt.want)
			}
		})
	}
}

func TestSelectTopFunctions(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "main.py",
			Functions: []analyzer.FunctionInfo{
				{Name: "main", Params: []string{}, ReturnType: "int"},
				{Name: "setup", Params: []string{}, ReturnType: "None"},
			},
			Imports: []analyzer.ImportInfo{{Module: "services.api", Name: "Api"}},
		},
		{
			Filename: "services/api.py",
			Classes: []analyzer.ClassInfo{
				{
					Name:    "Api",
					Methods: []analyzer.FunctionInfo{{Name: "get", Params: []string{"self", "path"}, ReturnType: "Response"}},
				},
			},
			Imports: []analyzer.ImportInfo{{Module: "models.user", Name: "User"}},
		},
		{
			Filename:  "utils/logger.py",
			Functions: []analyzer.FunctionInfo{{Name: "get_logger", Params: []string{"name"}, ReturnType: "Logger"}},
		},
	}
	graph := grapher.BuildGraph(files)

	// maxN=0 should return empty
	assert.Len(t, selectTopFunctions(graph, nil, 0), 0)

	// maxN=2 should cap at 2
	top2 := selectTopFunctions(graph, nil, 2)
	assert.Len(t, top2, 2)

	// maxN=10 with 4 total funcs should return all (target auto-computed as 4)
	all := selectTopFunctions(graph, nil, 10)
	assert.Len(t, all, 4)
}

func TestGenerateModuleDocs(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "services/api.py",
			Classes: []analyzer.ClassInfo{
				{
					Name:    "Api",
					Bases:   []string{"BaseHandler"},
					Methods: []analyzer.FunctionInfo{{Name: "get", Params: []string{"self", "path"}, ReturnType: "Response"}},
				},
			},
			Functions: []analyzer.FunctionInfo{{Name: "create_app", Params: []string{}, ReturnType: "App"}},
			Imports:   []analyzer.ImportInfo{{Module: "models.user", Name: "User"}},
		},
		{
			Filename:  "models/user.py",
			Classes:   []analyzer.ClassInfo{{Name: "User", Methods: []analyzer.FunctionInfo{{Name: "save", Params: []string{"self"}, ReturnType: "bool"}}}},
			Imports:   []analyzer.ImportInfo{{Module: "db.connection", Name: "Connection"}},
		},
	}
	graph := grapher.BuildGraph(files)

	docs := GenerateModuleDocs(graph, "")
	require.NotNil(t, docs)
	require.Len(t, docs, 2)

	// api.py doc (moduleNameFromFilename strips extension)
	apiDoc := docs["services/api"]
	assert.Contains(t, apiDoc, "# services/api")
	assert.Contains(t, apiDoc, "**难度级别**")
	assert.Contains(t, apiDoc, "## 功能说明")
	assert.Contains(t, apiDoc, "## 类定义")
	assert.Contains(t, apiDoc, "### Api")
	assert.Contains(t, apiDoc, "BaseHandler")
	assert.Contains(t, apiDoc, "## 函数列表")
	assert.Contains(t, apiDoc, "create_app")
	assert.Contains(t, apiDoc, "## 依赖模块")
	assert.Contains(t, apiDoc, "models/user")
	// user.py doc — models/user is depended on by services/api
	userDoc := docs["models/user"]
	assert.Contains(t, userDoc, "# models/user")
	assert.Contains(t, userDoc, "**难度级别**")
	assert.Contains(t, userDoc, "### User")
	assert.Contains(t, userDoc, "## 被依赖模块")
	assert.Contains(t, userDoc, "services/api")
}

func TestInferModuleDifficulty(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename:  "services/api.py",
			Classes:   []analyzer.ClassInfo{{Name: "Api", Methods: []analyzer.FunctionInfo{{Name: "get"}}}},
			Functions: []analyzer.FunctionInfo{{Name: "create_app"}},
			Imports:   []analyzer.ImportInfo{{Module: "models.user", Name: "User"}},
		},
		{
			Filename: "models/user.py",
			Classes:  []analyzer.ClassInfo{{Name: "User", Methods: []analyzer.FunctionInfo{{Name: "save"}}}},
			Imports:  []analyzer.ImportInfo{{Module: "db.connection", Name: "Connection"}},
		},
		{
			Filename:  "utils/logger.py",
			Functions: []analyzer.FunctionInfo{{Name: "log"}},
		},
	}
	graph := grapher.BuildGraph(files)

	// models/user is depended on by services/api → higher centrality → deeper difficulty
	userDiff := inferModuleDifficulty(graph.Nodes[1], graph)
	assert.True(t, userDiff == "⭐⭐⭐ 深入" || userDiff == "⭐⭐ 进阶", "models/user should be intermediate or advanced due to being depended on")

	// utils/logger is isolated → should be on the easier side
	loggerDiff := inferModuleDifficulty(graph.Nodes[2], graph)
	assert.True(t, loggerDiff == "⭐ 入门" || loggerDiff == "⭐⭐ 进阶", "isolated utility should not be advanced")
}

func TestWriteWikiFilesModuleDocs(t *testing.T) {
	tmpDir := t.TempDir()
	wiki := &Wiki{
		ProjectName:      "test",
		Overview:         "# Overview\n",
		WhatItDoes:       "# What\n",
		ProjectStructure: "# Structure\n",
		LearningPath:     "# Path\n",
		APIReference:     "# API\n",
		Architecture:     "# Arch\n",
		ModuleDocs: map[string]string{
			"app.py":      "# app.py\n",
			"models/user": "# models/user\n",
		},
	}

	err := WriteWikiFiles(tmpDir, wiki, nil)
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(tmpDir, "00-overview.md"))
	assert.FileExists(t, filepath.Join(tmpDir, "modules", "app.py.md"))
	assert.FileExists(t, filepath.Join(tmpDir, "modules", "models_user.md"))

	content, err := os.ReadFile(filepath.Join(tmpDir, "modules", "models_user.md"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "# models/user")
}

func TestGroupModulesByTheme(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "cmd/main.py", Functions: []analyzer.FunctionInfo{{Name: "main"}}},
		{Filename: "api/router.py", Functions: []analyzer.FunctionInfo{{Name: "route"}}},
		{Filename: "services/user_service.py", Functions: []analyzer.FunctionInfo{{Name: "get_user"}}},
		{Filename: "models/user.py", Classes: []analyzer.ClassInfo{{Name: "User"}}},
		{Filename: "repositories/user_repo.py", Functions: []analyzer.FunctionInfo{{Name: "find"}}},
		{Filename: "config/settings.py", Functions: []analyzer.FunctionInfo{{Name: "load"}}},
		{Filename: "utils/helpers.py", Functions: []analyzer.FunctionInfo{{Name: "helper"}}},
		{Filename: "tests/test_user.py", Functions: []analyzer.FunctionInfo{{Name: "test_user"}}},
		{Filename: "views/index.html", Functions: []analyzer.FunctionInfo{{Name: "render"}}},
		{Filename: "legacy/old.py", Functions: []analyzer.FunctionInfo{{Name: "old"}}},
	}
	graph := grapher.BuildGraph(files)

	groups := groupModulesByTheme(graph)

	assert.Contains(t, groups, "入口与命令行")
	assert.Contains(t, groups, "接口与路由")
	assert.Contains(t, groups, "业务逻辑与服务")
	assert.Contains(t, groups, "数据模型与实体")
	assert.Contains(t, groups, "数据访问与存储")
	assert.Contains(t, groups, "配置与基础设施")
	assert.Contains(t, groups, "工具与辅助函数")
	assert.Contains(t, groups, "测试与验证")
	assert.Contains(t, groups, "视图与 UI")
	assert.Contains(t, groups, "其他")

	// Verify specific assignments
	var cmdNames []string
	for _, n := range groups["入口与命令行"] {
		cmdNames = append(cmdNames, n.Name)
	}
	assert.Contains(t, cmdNames, "cmd/main")

	var modelNames []string
	for _, n := range groups["数据模型与实体"] {
		modelNames = append(modelNames, n.Name)
	}
	assert.Contains(t, modelNames, "models/user")

	// Verify stable sorting within groups
	for _, nodes := range groups {
		for i := 1; i < len(nodes); i++ {
			assert.True(t, nodes[i-1].Name <= nodes[i].Name, "nodes within theme should be sorted by name")
		}
	}
}

func TestWriteWikiFilesModuleIndex(t *testing.T) {
	tmpDir := t.TempDir()
	files := []*analyzer.FileResult{
		{Filename: "cmd/main.py", Functions: []analyzer.FunctionInfo{{Name: "main"}}},
		{Filename: "models/user.py", Classes: []analyzer.ClassInfo{{Name: "User"}}},
		{Filename: "utils/helpers.py", Functions: []analyzer.FunctionInfo{{Name: "helper"}}},
	}
	graph := grapher.BuildGraph(files)

	wiki := &Wiki{
		ProjectName:      "test",
		Overview:         "# Overview\n",
		WhatItDoes:       "# What\n",
		ProjectStructure: "# Structure\n",
		LearningPath:     "# Path\n",
		APIReference:     "# API\n",
		Architecture:     "# Arch\n",
		ModuleDocs: map[string]string{
			"cmd/main":    "# cmd/main\n",
			"models/user": "# models/user\n",
			"utils/helpers": "# utils/helpers\n",
		},
		ModuleThemes: map[string][]string{
			"入口与命令行":   {"cmd/main"},
			"数据模型与实体":  {"models/user"},
			"工具与辅助函数": {"utils/helpers"},
		},
	}

	err := WriteWikiFiles(tmpDir, wiki, graph)
	require.NoError(t, err)

	// Should generate modules/README.md
	idxPath := filepath.Join(tmpDir, "modules", "README.md")
	assert.FileExists(t, idxPath)

	content, err := os.ReadFile(idxPath)
	require.NoError(t, err)
	idx := string(content)

	assert.Contains(t, idx, "# 模块索引")
	assert.Contains(t, idx, "## 入口与命令行")
	assert.Contains(t, idx, "## 数据模型与实体")
	assert.Contains(t, idx, "## 工具与辅助函数")
	assert.Contains(t, idx, "| 模块 | 难度 | 职责 |")
	assert.Contains(t, idx, "[cmd/main](cmd_main.md)")
	assert.Contains(t, idx, "[models/user](models_user.md)")
	assert.Contains(t, idx, "[utils/helpers](utils_helpers.md)")
}

func TestGenerateStaticHTMLWithThemes(t *testing.T) {
	wiki := &Wiki{
		ProjectName:         "html-demo",
		Overview:            "# html-demo\n\n项目概述内容。\n",
		Architecture:        "# 架构\n\n架构说明内容。\n",
		APIReference:        "# API 参考\n\nAPI 内容。\n",
		ModuleDocs: map[string]string{
			"cmd/main":    "# cmd/main\n",
			"models/user": "# models/user\n",
		},
		ModuleThemes: map[string][]string{
			"入口与命令行":  {"cmd/main"},
			"数据模型与实体": {"models/user"},
		},
	}

	html := GenerateStaticHTML(wiki, nil)

	// Should contain theme sections in sidebar as chapter links
	assert.Contains(t, html, "入口与命令行")
	assert.Contains(t, html, "数据模型与实体")
	assert.Contains(t, html, `chapters/入口与命令行.html`)
	assert.Contains(t, html, `chapters/数据模型与实体.html`)
	// Should still contain inline module sections via the old module docs path
	// (module docs are now replaced by chapter listing, so module anchors no longer appear)
	assert.NotContains(t, html, `href="#module-cmd_main"`)
}

func TestGenerateMarkdownCompilationWithThemes(t *testing.T) {
	wiki := &Wiki{
		ProjectName:         "demo",
		Overview:            "# demo\n\n项目概述内容。\n",
		Architecture:        "# 架构\n\n架构说明内容。\n",
		APIReference:        "# API 参考\n\nAPI 内容。\n",
		ModuleDocs: map[string]string{
			"cmd/main":    "# cmd/main\n",
			"models/user": "# models/user\n",
		},
		ModuleThemes: map[string][]string{
			"入口与命令行":  {"cmd/main"},
			"数据模型与实体": {"models/user"},
		},
	}

	comp := GenerateMarkdownCompilation(wiki)

	assert.Contains(t, comp, "## 模块主题索引")
	assert.Contains(t, comp, "### 入口与命令行")
	assert.Contains(t, comp, "### 数据模型与实体")
	assert.Contains(t, comp, "- `cmd/main`")
	assert.Contains(t, comp, "- `models/user`")
}

func TestBuildFunctionDescriptionPrompt(t *testing.T) {
	funcs := []funcRef{
		{Module: "main", Name: "main", Params: []string{}, ReturnType: "int"},
		{Module: "services/api", Name: "Api.get", Params: []string{"self", "path"}, ReturnType: "Response", IsMethod: true},
	}
	prompt := buildFunctionDescriptionPrompt(funcs)
	assert.Contains(t, prompt, "2 个关键函数")
	assert.Contains(t, prompt, "main()")
	assert.Contains(t, prompt, "services/api")
	assert.Contains(t, prompt, "Api.get")
}

func TestParseFunctionDescriptions(t *testing.T) {
	funcs := []funcRef{
		{Module: "main", Name: "main"},
		{Module: "services/api", Name: "Api.get"},
	}

	// Normal response
	response := "main: 程序入口，执行主逻辑\nApi.get: 处理 HTTP GET 请求"
	result := parseFunctionDescriptions(response, funcs)
	assert.Equal(t, "程序入口，执行主逻辑", result["main#main"])
	assert.Equal(t, "处理 HTTP GET 请求", result["services/api#Api.get"])

	// Response with dash prefix and extra spaces
	response2 := "- main: 描述1\n  - Api.get: 描述2"
	result2 := parseFunctionDescriptions(response2, funcs)
	assert.Equal(t, "描述1", result2["main#main"])
	assert.Equal(t, "描述2", result2["services/api#Api.get"])

	// Response with missing colon should be ignored
	response3 := "main 没有冒号\nApi.get: 描述2"
	result3 := parseFunctionDescriptions(response3, funcs)
	assert.NotContains(t, result3, "main#main")
	assert.Equal(t, "描述2", result3["services/api#Api.get"])

	// Chinese colon
	responseCn := "main：程序入口\nApi.get：处理 GET"
	resultCn := parseFunctionDescriptions(responseCn, funcs)
	assert.Equal(t, "程序入口", resultCn["main#main"])
	assert.Equal(t, "处理 GET", resultCn["services/api#Api.get"])

	// Numbered list with markdown bold/code
	responseNum := "1. **main**: 描述1\n2. `Api.get`: 描述2"
	resultNum := parseFunctionDescriptions(responseNum, funcs)
	assert.Equal(t, "描述1", resultNum["main#main"])
	assert.Equal(t, "描述2", resultNum["services/api#Api.get"])

	// Empty response
	result4 := parseFunctionDescriptions("", funcs)
	assert.Empty(t, result4)
}

func TestDescribeFunctionMorePatterns(t *testing.T) {
	tests := []struct {
		name       string
		params     []string
		returnType string
		want       string
	}{
		{"__str__", []string{"self"}, "str", "返回对象的字符串表示"},
		{"__repr__", []string{"self"}, "str", "返回对象的正式字符串表示"},
		{"register", []string{"username", "password"}, "User", "用户注册，创建新账户"},
		{"signup", []string{"email"}, "User", "用户注册，创建新账户"},
		{"logout", []string{}, "None", "用户登出，终止当前会话"},
		{"start", []string{}, "None", "程序入口，执行主逻辑"},
		{"execute", []string{"cmd"}, "int", "程序入口，执行主逻辑"},
		{"set_config", []string{"key", "value"}, "None", "设置config，参数：key, value"},
		{"add_item", []string{"item"}, "None", "创建item，参数：item"},
		{"new_user", []string{"name"}, "User", "创建user，参数：name，返回 User"},
		{"modify_data", []string{"data"}, "None", "更新data，参数：data"},
		{"remove_item", []string{"id"}, "bool", "删除item，参数：id，返回 bool"},
		{"drop_table", []string{}, "None", "删除table"},
		{"check_status", []string{}, "bool", "验证status，返回 bool"},
		{"verify_token", []string{"token"}, "bool", "验证token，参数：token，返回 bool"},
		{"extract_data", []string{"source"}, "dict", "解析data，参数：source，返回 dict"},
		{"format_string", []string{"s"}, "str", "格式化string，参数：s，返回 str"},
		{"recv_msg", []string{}, "bytes", "接收msg，返回 bytes"},
		{"handle_request", []string{"req"}, "Response", "处理request，参数：req，返回 Response"},
		{"read_file", []string{"path"}, "str", "读取file，参数：path，返回 str"},
		{"store_data", []string{"data"}, "None", "保存data，参数：data"},
		{"write_log", []string{"msg"}, "None", "保存log，参数：msg"},
		{"search_user", []string{"query"}, "[]User", "查找user，参数：query，返回 []User"},
		{"lookup_key", []string{"key"}, "string", "查找key，参数：key，返回 string"},
		{"build_image", []string{"config"}, "Image", "生成image，参数：config，返回 Image"},
		{"make_request", []string{"url"}, "Response", "生成request，参数：url，返回 Response"},
		{"generate_report", []string{}, "Report", "生成report，返回 Report"},
		{"transform_data", []string{"input"}, "Output", "转换data，参数：input，返回 Output"},
		{"serialize", []string{"obj"}, "str", "序列化对象为结构化数据"},
		{"from_json", []string{"s"}, "Obj", "从结构化数据反序列化对象"},
		{"to_xml", []string{"obj"}, "str", "转换为xml，参数：obj，返回 str"},
		{"from_yaml", []string{"s"}, "Obj", "从yaml 解析/构建，参数：s，返回 Obj"},
		{"is_active", []string{}, "bool", "判断active 状态，返回 bool"},
		{"has_permission", []string{"user"}, "bool", "判断是否具备 permission，参数：user，返回 bool"},
		{"can_edit", []string{"user"}, "bool", "判断是否可 edit，参数：user，返回 bool"},
		{"render_page", []string{"template"}, "HTML", "渲染page，参数：template，返回 HTML"},
		{"draw_circle", []string{"x", "y", "r"}, "None", "渲染circle，参数：x, y, r"},
		{"display_info", []string{}, "None", "渲染info"},
		{"encode_base64", []string{"data"}, "str", "编解码base64，参数：data，返回 str"},
		{"decode_base64", []string{"s"}, "bytes", "编解码base64，参数：s，返回 bytes"},
		{"user_service", []string{"user_id"}, "User", "处理 user_service 相关逻辑"},
		{"api_handler", []string{"req"}, "Response", "处理 api_handler 相关逻辑"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describeFunction(tt.name, tt.params, tt.returnType)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDetectHallucination(t *testing.T) {
	graph := grapher.BuildGraph([]*analyzer.FileResult{
		{
			Filename: "models/user.py",
			Classes:  []analyzer.ClassInfo{{Name: "User"}},
			Functions: []analyzer.FunctionInfo{
				{Name: "get_user", Params: []string{"id"}, ReturnType: "User"},
			},
		},
		{
			Filename: "services/api.py",
			Classes: []analyzer.ClassInfo{
				{
					Name:    "Api",
					Methods: []analyzer.FunctionInfo{{Name: "get", Params: []string{"self", "path"}}},
				},
			},
		},
	})

	// No hallucination — all quoted names exist
	text1 := "项目使用 `User` 类并通过 `Api` 提供服务。"
	hallucinated, hasIt := detectHallucination(text1, graph)
	assert.False(t, hasIt, "所有引用的标识符均存在，不应判定为幻觉")
	assert.Empty(t, hallucinated)

	// 2 hallucinations out of 3 quoted ids (66%) — but sample ≤3, so NOT triggered
	text2 := "项目使用 `User` 类并通过 `FakeService` 和 `FakeModule` 提供服务。"
	hallucinated, hasIt = detectHallucination(text2, graph)
	assert.False(t, hasIt, "3 个样本以内不应触发，避免单个缩写误报")
	assert.Contains(t, hallucinated, "FakeService")
	assert.Contains(t, hallucinated, "FakeModule")

	// 3 hallucinations out of 5 quoted ids (60%) — not >60%, so NOT triggered
	text2b := "`User`、`Api`、`get_user`、`FakeService` 和 `FakeModule`。"
	hallucinated, hasIt = detectHallucination(text2b, graph)
	assert.False(t, hasIt, "3/5 幻觉占比 60% 不超过 60% 阈值，不应触发")
	assert.Contains(t, hallucinated, "FakeService")

	// 4 hallucinations out of 5 quoted ids (80%) > 60% → triggers
	text2c := "`User`、`FakeA`、`FakeB`、`FakeC` 和 `FakeD`。"
	hallucinated, hasIt = detectHallucination(text2c, graph)
	assert.True(t, hasIt, "4/5 幻觉占比 80% 超过 60% 阈值，应触发")

	// 2 hallucinations out of 5 quoted ids (40%) < 60% → NOT triggered
	text3 := "项目使用 `User`、`Api`、`FakeA` 和 `FakeB`。"
	hallucinated, hasIt = detectHallucination(text3, graph)
	assert.False(t, hasIt, "2/4 幻觉占比 50% 不超过 60% 阈值，不应触发")
	assert.Len(t, hallucinated, 2)

	// Hallucination — 8 fake identifiers should trigger absolute threshold
	text3b := "`FakeA`、`FakeB`、`FakeC`、`FakeD`、`FakeE`、`FakeF`、`FakeG` 和 `FakeH`。"
	hallucinated, hasIt = detectHallucination(text3b, graph)
	assert.True(t, hasIt, "8 处幻觉应达到阈值")
	assert.Len(t, hallucinated, 8)

	// Bold markdown is intentionally ignored (design-pattern terms are OK)
	text4 := "**User** 是核心类，**NonExistent** 负责调度。"
	hallucinated, hasIt = detectHallucination(text4, graph)
	assert.False(t, hasIt, "加粗文本不再参与幻觉检测")
	assert.Empty(t, hallucinated)

	// Empty graph should not panic and return no hallucination
	hallucinated, hasIt = detectHallucination("`Foo`", nil)
	assert.False(t, hasIt)
	assert.Empty(t, hallucinated)
}

func TestExtractQuotedIdentifiers(t *testing.T) {
	// Backticks
	ids1 := extractQuotedIdentifiers("使用 `User` 和 `get_user` 方法")
	assert.ElementsMatch(t, []string{"User", "get_user"}, ids1)

	// Bold is intentionally ignored
	ids2 := extractQuotedIdentifiers("**Api** 类包含 **get** 方法")
	assert.Empty(t, ids2)

	// Mixed — only backticks count
	ids3 := extractQuotedIdentifiers("`User` 和 **Api** 以及 `OrderService`")
	assert.ElementsMatch(t, []string{"User", "OrderService"}, ids3)

	// Empty
	ids4 := extractQuotedIdentifiers("没有任何标识符")
	assert.Empty(t, ids4)
}

func TestCollectRealIdentifiers(t *testing.T) {
	graph := grapher.BuildGraph([]*analyzer.FileResult{
		{
			Filename: "models/user.py",
			Classes:  []analyzer.ClassInfo{{Name: "User"}},
			Functions: []analyzer.FunctionInfo{
				{Name: "get_user", Params: []string{"id"}},
			},
		},
	})
	ids := collectRealIdentifiers(graph)
	assert.True(t, ids["models/user"])
	assert.True(t, ids["User"])
	assert.True(t, ids["get_user"])
	assert.False(t, ids["Fake"])
}

func TestCollectRealIdentifiersWithExtensionlessPaths(t *testing.T) {
	graph := grapher.BuildGraph([]*analyzer.FileResult{
		{Filename: "src/commands/chat.ts", Classes: []analyzer.ClassInfo{{Name: "Chat"}}},
		{Filename: "utils/logger.js", Functions: []analyzer.FunctionInfo{{Name: "log"}}},
	})
	ids := collectRealIdentifiers(graph)
	// Node.Name is already extension-less (moduleNameFromFilename strips it)
	assert.True(t, ids["src/commands/chat"], "extension-less TS path should be registered")
	assert.True(t, ids["utils/logger"], "extension-less JS path should be registered")
	// Class and function names still present
	assert.True(t, ids["Chat"])
	assert.True(t, ids["log"])
}

func TestExtractQuotedIdentifiersFiltersChinese(t *testing.T) {
	// Chinese technical terms wrapped in backticks should be ignored
	ids := extractQuotedIdentifiers("项目采用 `命令模式` 和 `依赖注入` 设计")
	assert.Empty(t, ids, "纯中文概念不应被当作代码标识符")

	// Mixed Chinese and code should only keep the code part
	ids2 := extractQuotedIdentifiers("`User` 类实现 `单例模式`")
	assert.ElementsMatch(t, []string{"User"}, ids2)

	// English identifiers and paths still work
	ids3 := extractQuotedIdentifiers("调用 `get_user` 和 `services/api`")
	assert.ElementsMatch(t, []string{"get_user", "services/api"}, ids3)
}

func TestDetectHallucinationWithPathFragments(t *testing.T) {
	graph := grapher.BuildGraph([]*analyzer.FileResult{
		{Filename: "src/commands/chat.ts", Classes: []analyzer.ClassInfo{{Name: "Chat"}}},
		{Filename: "src/utils/logger.ts", Functions: []analyzer.FunctionInfo{{Name: "log"}}},
	})

	// LLM uses a shorter path suffix — should NOT be flagged
	text := "`commands/chat` 是核心模块，通过 `log` 记录日志。"
	hallucinated, hasIt := detectHallucination(text, graph)
	assert.False(t, hasIt, "路径后缀匹配不应触发幻觉")
	assert.Empty(t, hallucinated)

	// Genuine hallucination should still be caught (4 fakes out of 4 = 100% > 60%)
	text2 := "`FakeModule`、`FakeService`、`FakeController` 和 `FakeHelper` 提供服务。"
	hallucinated2, hasIt2 := detectHallucination(text2, graph)
	assert.True(t, hasIt2, "真实幻觉应被检测到")
	assert.Contains(t, hallucinated2, "FakeModule")
	assert.Contains(t, hallucinated2, "FakeService")
}

func TestIsRealIdentifierWithPaths(t *testing.T) {
	realIDs := map[string]bool{
		"D:/project/src/commands/chat": true,
		"D:/project/examples/demo":     true,
		"User":                         true,
	}

	// Exact match
	assert.True(t, isRealIdentifier("User", realIDs))

	// Path substring
	assert.True(t, isRealIdentifier("src/commands/chat", realIDs))
	assert.True(t, isRealIdentifier("commands/chat", realIDs))
	assert.True(t, isRealIdentifier("examples/demo", realIDs))

	// Wildcard pattern matching prefix
	assert.True(t, isRealIdentifier("examples/*", realIDs))

	// Non-existent path
	assert.False(t, isRealIdentifier("src/admin", realIDs))

	// Non-existent identifier
	assert.False(t, isRealIdentifier("FakeUser", realIDs))
}

func TestGenerateWhatItDoesMarkdown(t *testing.T) {
	graph := grapher.BuildGraph([]*analyzer.FileResult{
		{Filename: "main.py", Functions: []analyzer.FunctionInfo{{Name: "main"}}, Imports: []analyzer.ImportInfo{{Module: "services.api", Name: "Api"}}},
		{Filename: "services/api.py", Classes: []analyzer.ClassInfo{{Name: "Api", Methods: []analyzer.FunctionInfo{{Name: "get"}}}}, Imports: []analyzer.ImportInfo{{Module: "models.user", Name: "User"}}},
		{Filename: "models/user.py", Classes: []analyzer.ClassInfo{{Name: "User", Methods: []analyzer.FunctionInfo{{Name: "authenticate"}}}}, Imports: []analyzer.ImportInfo{{Module: "utils.logger", Name: "get_logger"}}},
		{Filename: "utils/logger.py", Functions: []analyzer.FunctionInfo{{Name: "get_logger"}}},
	})

	got := GenerateWhatItDoesMarkdown(graph, "test-project")

	// Should contain narrative sections, not just module lists
	assert.Contains(t, got, "## 项目定位")
	assert.Contains(t, got, "## 核心能力")
	assert.Contains(t, got, "| 能力 | 说明 | 涉及模块 |")

	// Should NOT contain old-style flat module lists
	assert.NotContains(t, got, "### 核心领域")
	assert.NotContains(t, got, "### 基础设施")

	// Should contain some inferred capabilities (exact names depend on PageRank roles)
	assert.Contains(t, got, "test-project")
}

func TestInferCapabilityFromName(t *testing.T) {
	tests := []struct {
		name, role, wantCap, wantDesc string
	}{
		// 入口层
		{"api_router", "入口层", "API 服务", "接收并分发外部请求"},
		{"cli_tool", "入口层", "命令行交互", "提供终端命令接口"},
		{"web_server", "入口层", "Web 服务", "运行 HTTP 服务"},
		{"gui_view", "入口层", "界面交互", "提供图形或 Web 界面"},
		{"main", "入口层", "程序入口", "系统启动入口"},
		// 核心领域
		{"models/user", "核心领域", "用户与认证", "管理用户身份"},
		{"services/order", "核心领域", "交易与订单", "处理订单生命周期"},
		{"product/catalog", "核心领域", "商品与目录", "维护商品信息"},
		{"chat/message", "核心领域", "消息通知", "负责消息发送"},
		{"storage/file", "核心领域", "文件与存储", "管理文件上传"},
		{"models/entity", "核心领域", "数据模型", "定义数据实体"},
		{"config/setting", "核心领域", "配置管理", "管理系统配置"},
		{"search/index", "核心领域", "搜索查询", "提供全文检索"},
		{"job/worker", "核心领域", "异步任务", "处理后台任务"},
		{"analytics/report", "核心领域", "报表分析", "生成统计报表"},
		{"cache/redis", "核心领域", "缓存加速", "提供数据缓存"},
		{"log/monitor", "核心领域", "日志监控", "记录运行日志"},
		// 工具库
		{"utils/helpers", "工具库", "通用工具", "提供跨模块复用"},
		{"tests/mock", "工具库", "测试辅助", "提供测试固件"},
		{"format/parse", "工具库", "数据转换", "负责格式解析"},
		{"valid/check", "工具库", "校验验证", "提供输入校验"},
		{"net/client", "工具库", "网络通信", "封装网络请求"},
		{"crypto/hash", "工具库", "安全加密", "提供加密"},
		{"i18n/locale", "工具库", "国际化", "支持多语言"},
		// Fallback patterns
		{"core/business", "核心领域", "业务处理", "承载项目核心业务逻辑"},
		// "router/gateway" contains "route" and role is 入口层 → matches "API 服务"
		{"entry/startup", "入口层", "程序入口", "系统启动入口"},
		{"gateway/proxy", "入口层", "请求处理", "接收外部输入"},
		{"unknown_module", "", "功能模块", "提供特定领域的功能实现"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capName, capDesc := inferCapabilityFromName(tt.name, tt.role)
			assert.Equal(t, tt.wantCap, capName)
			assert.Contains(t, capDesc, tt.wantDesc)
		})
	}
}

func TestBuildWhereToGoNext(t *testing.T) {
	assert.Contains(t, buildWhereToGoNext("00-overview.md", true), "项目能做什么")
	assert.Contains(t, buildWhereToGoNext("01-what-it-does.md", true), "架构说明")
	assert.Contains(t, buildWhereToGoNext("02-architecture.md", true), "项目结构")

	// With key concepts
	assert.Contains(t, buildWhereToGoNext("03-project-structure.md", true), "核心概念")
	// Without key concepts
	assert.Contains(t, buildWhereToGoNext("03-project-structure.md", false), "学习路径")

	assert.Contains(t, buildWhereToGoNext("04-key-concepts.md", true), "学习路径")
	assert.Contains(t, buildWhereToGoNext("05-learning-path.md", true), "API 参考")
	assert.Contains(t, buildWhereToGoNext("api-reference.md", true), "阅读完成")

	// Unknown file returns empty
	assert.Empty(t, buildWhereToGoNext("unknown.md", true))
}

func TestGenerateWikiEnhancedWithWhereToGoNext(t *testing.T) {
	graph := grapher.BuildGraph([]*analyzer.FileResult{
		{Filename: "main.py", Functions: []analyzer.FunctionInfo{{Name: "main"}}},
	})

	wiki, err := generateWikiEnhanced(context.Background(), nil, graph, "", "test-project", "", "", "", "", 0)
	require.NoError(t, err)

	// Each narrative article should end with a "next reading" navigation
	assert.Contains(t, wiki.Overview, "下一步阅读", "overview should have where-to-go-next")
	assert.Contains(t, wiki.WhatItDoes, "下一步阅读", "what-it-does should have where-to-go-next")
	assert.Contains(t, wiki.Architecture, "下一步阅读", "architecture should have where-to-go-next")
	assert.Contains(t, wiki.ProjectStructure, "下一步阅读", "project-structure should have where-to-go-next")
	assert.Contains(t, wiki.LearningPath, "下一步阅读", "learning-path should have where-to-go-next")
	assert.Contains(t, wiki.APIReference, "阅读完成", "api-reference should have completion message")
}

func TestBuildSourcesFooterFileLevel(t *testing.T) {
	graph := grapher.BuildGraph([]*analyzer.FileResult{
		{
			Filename:  "models/user.py",
			Classes:   []analyzer.ClassInfo{{Name: "User", StartLine: 5}},
			Functions: []analyzer.FunctionInfo{{Name: "get_user", StartLine: 12}},
		},
		{
			Filename:  "services/api.py",
			Functions: []analyzer.FunctionInfo{{Name: "handle", StartLine: 3}},
		},
	})

	footer := buildSourcesFooter(graph, 10)
	// Node with both functions and classes uses function line (functions checked first)
	assert.Contains(t, footer, "`models/user.py:12`", "footer should cite filename with function line number when functions exist")
	assert.Contains(t, footer, "`services/api.py:3`", "footer should cite filename with function line number")
	assert.Contains(t, footer, "models/user）", "footer should still include module name in parentheses")
	assert.Contains(t, footer, "services/api）", "footer should still include module name in parentheses")

	// Node with only classes (no functions) uses class line
	graph2 := grapher.BuildGraph([]*analyzer.FileResult{
		{Filename: "models/order.py", Classes: []analyzer.ClassInfo{{Name: "Order", StartLine: 7}}},
	})
	footer2 := buildSourcesFooter(graph2, 10)
	assert.Contains(t, footer2, "`models/order.py:7`", "footer should cite filename with class line when no functions")
}

func TestWriteWikiFilesChapterPages(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "cmd/main.go", Functions: []analyzer.FunctionInfo{{Name: "main"}}},
		{Filename: "models/user.go", Imports: []analyzer.ImportInfo{{Module: "cmd/main"}}, Classes: []analyzer.ClassInfo{{Name: "User"}}},
	}
	graph := grapher.BuildGraph(files)

	wiki := &Wiki{
		ProjectName: "chapter-test",
		Overview:    "# overview\n",
		Architecture: "# arch\n",
		ModuleDocs: map[string]string{
			"cmd/main":    "# cmd/main\n\nentry point module.\n",
			"models/user": "# models/user\n\ndata model.\n",
		},
		ModuleThemes: map[string][]string{
			"入口与命令行":  {"cmd/main"},
			"数据模型与实体": {"models/user"},
		},
		ChapterTitles: map[string]ChapterTitle{
			"入口与命令行":  {Title: "入口与命令行", Difficulty: "⭐"},
			"数据模型与实体": {Title: "数据模型", Difficulty: "⭐⭐"},
		},
		APIReference: "# api ref\n",
	}

	// Pre-generate chapter pages (normally done via LLM pipeline)
	wiki.ChapterPages = GenerateChapterPages(wiki, graph)

	tmpDir := t.TempDir()
	outDir := filepath.Join(tmpDir, "wiki")

	err := WriteWikiFiles(outDir, wiki, graph)
	require.NoError(t, err)

	// Verify chapter directory and files
	chaptersDir := filepath.Join(outDir, "chapters")
	assert.DirExists(t, chaptersDir)

	chap1 := filepath.Join(chaptersDir, "入口与命令行.html")
	assert.FileExists(t, chap1)
	content1, err := os.ReadFile(chap1)
	require.NoError(t, err)
	assert.Contains(t, string(content1), "入口与命令行")
	assert.Contains(t, string(content1), "⭐")

	chap2 := filepath.Join(chaptersDir, "数据模型与实体.html")
	assert.FileExists(t, chap2)
	content2, err := os.ReadFile(chap2)
	require.NoError(t, err)
	assert.Contains(t, string(content2), "数据模型")
	assert.Contains(t, string(content2), "⭐⭐")

	// Verify index.html has chapter links in sidebar
	indexHTML, err := os.ReadFile(filepath.Join(outDir, "index.html"))
	require.NoError(t, err)
	assert.Contains(t, string(indexHTML), `chapters/入口与命令行.html`)
	assert.Contains(t, string(indexHTML), `chapters/数据模型与实体.html`)
	assert.Contains(t, string(indexHTML), "深入剖析")

	// Verify index.html does NOT have inline module links in nav sidebar
	// (file tree links in right sidebar are expected and correct)
	assert.NotContains(t, string(indexHTML), `"nav-icon">📦<`)
}

func TestWriteWikiFilesNoThemes(t *testing.T) {
	wiki := &Wiki{
		ProjectName: "no-themes",
		Overview:    "# overview\n",
		ModuleDocs: map[string]string{
			"pkg/util": "# util\n",
		},
	}
	tmpDir := t.TempDir()
	outDir := filepath.Join(tmpDir, "wiki")

	err := WriteWikiFiles(outDir, wiki, nil)
	require.NoError(t, err)

	// Without graph and themes, chapters dir should not exist
	chaptersDir := filepath.Join(outDir, "chapters")
	_, err = os.Stat(chaptersDir)
	assert.True(t, os.IsNotExist(err))
}

func TestKeywordOverlap(t *testing.T) {
	tests := []struct {
		source, target string
		expected       int
	}{
		{"services/user_service", "业务逻辑与服务", 0},   // Chinese vs English
		{"api/handler/auth", "接口与路由", 0},            // no overlap
		{"models/user", "models", 1},                     // "models" matches
		{"cmd/main", "cmd", 1},                           // "cmd" matches
		{"utils/helper/common", "util", 1},               // "util" found in "utils"
		{"config/server/settings", "config", 1},          // matches
	}
	for _, tt := range tests {
		t.Run(tt.source+"_"+tt.target, func(t *testing.T) {
			assert.Equal(t, tt.expected, keywordOverlap(tt.source, tt.target))
		})
	}
}

func TestFindClosestTheme(t *testing.T) {
	// Use English theme names where keywordOverlap gives deterministic results
	themes := map[string][]*grapher.Node{
		"cmd-and-cli":   {},
		"data-models":   {},
		"business-logic": {},
	}
	// "cmd/main" contains "cmd" → should match "cmd-and-cli"
	assert.Equal(t, "cmd-and-cli", findClosestTheme("cmd/main", themes))

	// "models/user" contains "models" → should match "data-models"
	assert.Equal(t, "data-models", findClosestTheme("models/user", themes))

	// No overlap with any theme → returns any (map iteration order is random)
	result := findClosestTheme("services/user_service", themes)
	assert.Contains(t, []string{"cmd-and-cli", "data-models", "business-logic"}, result)
}

func TestTakeFirstN(t *testing.T) {
	tests := []struct {
		ss       []string
		n        int
		expected []string
	}{
		{[]string{"a", "b", "c"}, 2, []string{"a", "b"}},
		{[]string{"a"}, 2, []string{"a"}},
		{[]string{"a", "b"}, 0, []string{}},
		{nil, 5, nil},
	}
	for _, tt := range tests {
		result := takeFirstN(tt.ss, tt.n)
		assert.Equal(t, tt.expected, result)
	}
}

func TestFirstOf(t *testing.T) {
	assert.Equal(t, "a", firstOf([]string{"a", "b", "c"}))
	assert.Equal(t, "", firstOf(nil))
	assert.Equal(t, "", firstOf([]string{}))
}

func TestFirstOr(t *testing.T) {
	assert.Equal(t, "a", firstOr([]string{"a", "b"}, "default"))
	assert.Equal(t, "default", firstOr(nil, "default"))
	assert.Equal(t, "default", firstOr([]string{}, "default"))
}

func TestSortedKeys(t *testing.T) {
	// Root entries come first
	m := map[string][]*grapher.Node{
		"services": {},
		".":        {},
		"models":   {},
	}
	keys := sortedKeys(m)
	assert.Equal(t, ".", keys[0])
	assert.Equal(t, []string{".", "models", "services"}, keys)

	// No root entry — pure alphabetical
	m2 := map[string][]*grapher.Node{
		"b": {},
		"a": {},
		"c": {},
	}
	assert.Equal(t, []string{"a", "b", "c"}, sortedKeys(m2))

	// Empty map
	assert.Empty(t, sortedKeys(map[string][]*grapher.Node{}))
}

func TestInferLanguageFromFilename(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{"main.py", "python"},
		{"main.go", "go"},
		{"app.js", "javascript"},
		{"app.ts", "typescript"},
		{"app.tsx", "typescript"},
		{"App.java", "java"},
		{"lib.rs", "rust"},
		{"main.cpp", "cpp"},
		{"main.cc", "cpp"},
		{"main.c", "cpp"},
		{"README.md", ""},
		{"Dockerfile", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			assert.Equal(t, tt.expected, inferLanguageFromFilename(tt.filename))
		})
	}
}

func TestNodesToFileResults(t *testing.T) {
	nodes := []*grapher.Node{
		{
			Name: "models/user", Filename: "models/user.py",
			Classes:   []analyzer.ClassInfo{{Name: "User"}},
			Functions: []analyzer.FunctionInfo{{Name: "create_user"}},
		},
		{
			Name: "main", Filename: "main.py",
			Functions: []analyzer.FunctionInfo{{Name: "main"}},
		},
	}
	files := nodesToFileResults(nodes)
	require.Len(t, files, 2)
	assert.Equal(t, "models/user.py", files[0].Filename)
	assert.Len(t, files[0].Classes, 1)
	assert.Len(t, files[0].Functions, 1)
	assert.Equal(t, "main.py", files[1].Filename)

	// Nil input returns empty slice (make with cap=0)
	assert.Empty(t, nodesToFileResults(nil))
}

func TestFormatSnippetMarkdown(t *testing.T) {
	s := CodeSnippet{
		Label:     "main函数",
		Filename:  "main.py",
		StartLine: 10,
		EndLine:   15,
		Language:  "python",
		Code:      "def main():\n    pass",
	}
	result := FormatSnippetMarkdown(s)
	assert.Contains(t, result, "**main函数**")
	assert.Contains(t, result, "`main.py:10-15`")
	assert.Contains(t, result, "```python")
	assert.Contains(t, result, "def main():\n    pass")
}

func TestClearWikiCheckpoint(t *testing.T) {
	dir := t.TempDir()
	checkpointDir := filepath.Join(dir, ".codewiki", "checkpoint")
	os.MkdirAll(checkpointDir, 0755)
	checkpointPath := filepath.Join(checkpointDir, "wiki.json")
	os.WriteFile(checkpointPath, []byte("{}"), 0644)

	// Verify file exists
	_, err := os.Stat(checkpointPath)
	require.NoError(t, err)

	ClearWikiCheckpoint(dir)

	// Verify file removed
	_, err = os.Stat(checkpointPath)
	assert.True(t, os.IsNotExist(err))

	// Calling on non-existent dir should not panic
	ClearWikiCheckpoint(filepath.Join(dir, "nonexistent"))
}

func TestStripInlineMarkdown(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"**bold** text", "bold text"},
		{"__underline__ text", "underline text"},
		{"inline `code` here", "inline code here"},
		{"[link text](url)", "link text"},
		{"**bold** and `code`", "bold and code"},
		{"no formatting", "no formatting"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, stripInlineMarkdown(tt.input))
		})
	}
}

func TestCollectCoreClasses(t *testing.T) {
	graph := grapher.BuildGraph([]*analyzer.FileResult{
		{
			Filename: "models/user.py",
			Classes:  []analyzer.ClassInfo{{Name: "User"}, {Name: "UserProfile"}},
		},
		{
			Filename: "models/base.py",
			Classes:  []analyzer.ClassInfo{{Name: "BaseModel"}},
		},
		{
			Filename: "services/user_service.py",
			Classes:  []analyzer.ClassInfo{{Name: "UserService"}},
			Imports:  []analyzer.ImportInfo{{Module: "..models.user", IsRelative: true}},
		},
	})

	// Get all classes
	all := collectCoreClasses(graph, 10)
	require.Len(t, all, 4)
	names := make([]string, len(all))
	for i, c := range all {
		names[i] = c.Name
	}
	assert.Contains(t, names, "User")
	assert.Contains(t, names, "BaseModel")
	assert.Contains(t, names, "UserService")
	assert.Contains(t, names, "UserProfile")

	// Limit to 2 — should get top 2 by PageRank
	top2 := collectCoreClasses(graph, 2)
	assert.Len(t, top2, 2)

	// Empty graph
	assert.Empty(t, collectCoreClasses(&grapher.Graph{}, 5))

	// Graph with no classes
	noClasses := grapher.BuildGraph([]*analyzer.FileResult{
		{Filename: "utils/logger.py", Functions: []analyzer.FunctionInfo{{Name: "get_logger"}}},
	})
	assert.Empty(t, collectCoreClasses(noClasses, 5))
}

func TestLoadProjectReadme(t *testing.T) {
	// Empty dir
	assert.Empty(t, loadProjectReadme(""))

	dir := t.TempDir()

	// No readme
	assert.Empty(t, loadProjectReadme(dir))

	// README.md
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("Hello World"), 0644)
	assert.Equal(t, "Hello World", loadProjectReadme(dir))

	// readme.md (case insensitive via filename list)
	dir2 := t.TempDir()
	os.WriteFile(filepath.Join(dir2, "readme.md"), []byte("lowercase"), 0644)
	assert.Equal(t, "lowercase", loadProjectReadme(dir2))

	// Long content gets truncated
	dir3 := t.TempDir()
	long := strings.Repeat("a", 5000)
	os.WriteFile(filepath.Join(dir3, "README.md"), []byte(long), 0644)
	result := loadProjectReadme(dir3)
	assert.Contains(t, result, "...（README 内容已截断）")
	assert.Len(t, result, 4000+len("\n...（README 内容已截断）"))
}

func TestAddFrontmatterFallback(t *testing.T) {
	// fileTitleMap has no entry for this filename, and content has no heading → uses filename
	result := addFrontmatter("unknown-file.md", "plain text without heading", "test-project", 5)
	assert.Contains(t, result, "title: \"unknown-file.md\"")
	assert.Contains(t, result, "project: \"test-project\"")
	assert.Contains(t, result, "source_modules: 5")
	assert.Contains(t, result, "plain text without heading")

	// Content has heading → extract title from first heading
	result2 := addFrontmatter("some-file.md", "# My Title\n\ncontent here", "proj", 3)
	assert.Contains(t, result2, "title: \"My Title\"")
}

func TestCleanupOldHistory(t *testing.T) {
	// Non-existent dir returns nil
	assert.NoError(t, cleanupOldHistory(filepath.Join(t.TempDir(), "nonexistent"), 5))

	// Empty dir with keep=0 → nothing to remove
	dir := t.TempDir()
	assert.NoError(t, cleanupOldHistory(dir, 5))

	// Fewer entries than keep
	os.MkdirAll(filepath.Join(dir, "v1"), 0755)
	os.MkdirAll(filepath.Join(dir, "v2"), 0755)
	assert.NoError(t, cleanupOldHistory(dir, 5))
	assert.DirExists(t, filepath.Join(dir, "v1"))
	assert.DirExists(t, filepath.Join(dir, "v2"))

	// More entries than keep → oldest removed
	dir2 := t.TempDir()
	os.MkdirAll(filepath.Join(dir2, "v1"), 0755)
	os.MkdirAll(filepath.Join(dir2, "v2"), 0755)
	os.MkdirAll(filepath.Join(dir2, "v3"), 0755)
	assert.NoError(t, cleanupOldHistory(dir2, 2))
	assert.NoDirExists(t, filepath.Join(dir2, "v1"))
	assert.DirExists(t, filepath.Join(dir2, "v2"))
	assert.DirExists(t, filepath.Join(dir2, "v3"))
}

func TestBuildChapterNarrativePrompt(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename:  "auth/middleware.go",
			Functions: []analyzer.FunctionInfo{{Name: "Authenticate"}, {Name: "ValidateToken"}},
			Imports:   []analyzer.ImportInfo{{Module: "auth/session"}},
		},
		{
			Filename:  "auth/session.go",
			Functions: []analyzer.FunctionInfo{{Name: "CreateSession"}, {Name: "DestroySession"}},
		},
	}
	graph := grapher.BuildGraph(files)
	modules := []string{"auth/middleware.go", "auth/session.go"}
	title := ChapterTitle{Title: "用户认证", Subtitle: "身份验证与会话管理", Difficulty: "⭐⭐"}

	prompt := buildChapterNarrativePrompt("test-project", "认证系统", title, modules, graph)

	assert.Contains(t, prompt, "test-project")
	assert.Contains(t, prompt, "用户认证")
	assert.Contains(t, prompt, "身份验证与会话管理")
	assert.Contains(t, prompt, "⭐⭐")
	assert.Contains(t, prompt, "auth/middleware.go")
	assert.Contains(t, prompt, "auth/session.go")
	assert.Contains(t, prompt, "叙事式组织")
	assert.Contains(t, prompt, "设计决策")
	assert.Contains(t, prompt, "关键收获")
	assert.Contains(t, prompt, "来源标注")
}

func TestGenerateChapterNarrativesFallback(t *testing.T) {
	themes := map[string][]string{
		"主题A": {"mod1", "mod2"},
	}
	titles := map[string]ChapterTitle{
		"主题A": {Title: "标题A"},
	}
	files := []*analyzer.FileResult{
		{Filename: "mod1.go"},
		{Filename: "mod2.go"},
	}
	graph := grapher.BuildGraph(files)

	// provider=nil should return nil
	result := generateChapterNarratives(context.Background(), nil, "proj", themes, titles, graph)
	assert.Nil(t, result)
}

func TestFilterInTheme(t *testing.T) {
	themeModules := []string{"auth/middleware", "auth/session", "auth/token"}
	names := []string{"auth/middleware", "db/store", "auth/token", "config/loader"}

	filtered := filterInTheme(names, themeModules)
	assert.Equal(t, []string{"auth/middleware", "auth/token"}, filtered)
}

func TestFilterInThemeEmpty(t *testing.T) {
	result := filterInTheme(nil, []string{"a", "b"})
	assert.Nil(t, result)

	result = filterInTheme([]string{"a"}, nil)
	assert.Nil(t, result)
}

func TestGenerateChapterNarrativesStreamSuccess(t *testing.T) {
	themes := map[string][]string{
		"主题A": {"mod1.go", "mod2.go"},
	}
	titles := map[string]ChapterTitle{
		"主题A": {Title: "标题A"},
	}
	files := []*analyzer.FileResult{
		{Filename: "mod1.go", Functions: []analyzer.FunctionInfo{{Name: "Func1"}}},
		{Filename: "mod2.go", Functions: []analyzer.FunctionInfo{{Name: "Func2"}}},
	}
	graph := grapher.BuildGraph(files)

	mock := &mockProvider{response: "## 概述\n\n这是一篇关于主题A的叙事文章，介绍了认证与会话管理系统如何协作。整个系统通过中间件模式实现请求拦截，对令牌进行校验后转发至下游服务。\n\n## 设计决策\n\n> 采用无状态令牌而非服务端会话，减少存储依赖。\n\n## 关键收获\n\n这里是关键收获的详细讨论段落，解释了为什么这种设计模式在微服务架构中尤为重要。它不仅降低了系统复杂度，还提高了水平扩展能力。"}

	result := generateChapterNarratives(context.Background(), mock, "proj", themes, titles, graph)
	require.NotNil(t, result)
	assert.Contains(t, result, "主题A")
	assert.Contains(t, result["主题A"], "概述")
}

func TestGenerateChapterNarrativesProgressiveDegradation(t *testing.T) {
	themes := map[string][]string{
		"主题A": {"mod1.go"},
	}
	titles := map[string]ChapterTitle{
		"主题A": {Title: "标题A"},
	}
	files := []*analyzer.FileResult{
		{Filename: "mod1.go"},
	}
	graph := grapher.BuildGraph(files)

	// Stream fails but Complete succeeds → Level 3 non-streaming should work
	mock := &mockProvider{
		streamErr: errors.New("stream connect failed"),
		response:  "## 主题概述\n\n这是极简模式生成的文章。",
		err:       nil,
	}
	result := generateChapterNarratives(context.Background(), mock, "proj", themes, titles, graph)
	require.NotNil(t, result)
	assert.Contains(t, result["主题A"], "主题概述")
}

func TestGenerateChapterNarrativesAllLevelsFail(t *testing.T) {
	themes := map[string][]string{
		"主题A": {"mod1.go"},
	}
	titles := map[string]ChapterTitle{
		"主题A": {Title: "标题A"},
	}
	files := []*analyzer.FileResult{
		{Filename: "mod1.go"},
	}
	graph := grapher.BuildGraph(files)

	// Both stream and complete fail
	mock := &mockProvider{
		streamErr: errors.New("stream failed"),
		err:       errors.New("complete failed"),
	}
	result := generateChapterNarratives(context.Background(), mock, "proj", themes, titles, graph)
	assert.Nil(t, result)
}

func TestCleanNarrativeResponse(t *testing.T) {
	assert.Equal(t, "content", cleanNarrativeResponse("```markdown\ncontent\n```"))
	assert.Equal(t, "plain text", cleanNarrativeResponse("plain text"))
	assert.Equal(t, "", cleanNarrativeResponse(""))
	assert.Equal(t, "hello", cleanNarrativeResponse("  hello  "))
}

func TestBuildSimplifiedNarrativePrompt(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "a.go", Functions: []analyzer.FunctionInfo{{Name: "DoA"}}},
		{Filename: "b.go", Functions: []analyzer.FunctionInfo{{Name: "DoB"}}},
	}
	graph := grapher.BuildGraph(files)
	title := ChapterTitle{Title: "测试主题"}
	prompt := buildSimplifiedNarrativePrompt("proj", "theme", title, []string{"a.go", "b.go"}, graph)

	assert.Contains(t, prompt, "proj")
	assert.Contains(t, prompt, "测试主题")
	assert.Contains(t, prompt, "a")
	assert.Contains(t, prompt, "b")
	assert.Contains(t, prompt, "600-1000")
	// Should NOT contain function details or dependency details
	assert.NotContains(t, prompt, "关键函数")
	assert.NotContains(t, prompt, "主题内依赖")
}

func TestBuildMinimalNarrativePrompt(t *testing.T) {
	title := ChapterTitle{Title: "极简主题"}
	prompt := buildMinimalNarrativePrompt("proj", "theme", title, []string{"a", "b"})

	assert.Contains(t, prompt, "proj")
	assert.Contains(t, prompt, "极简主题")
	assert.Contains(t, prompt, "a")
	assert.Contains(t, prompt, "b")
	assert.Contains(t, prompt, "400-800")
}

func TestBuildChapterNarrativePromptModuleCap(t *testing.T) {
	var files []*analyzer.FileResult
	var modules []string
	for i := 0; i < 15; i++ {
		name := fmt.Sprintf("mod%d.go", i)
		files = append(files, &analyzer.FileResult{Filename: name})
		modules = append(modules, name)
	}
	graph := grapher.BuildGraph(files)
	title := ChapterTitle{Title: "大量模块主题"}

	prompt := buildChapterNarrativePrompt("proj", "theme", title, modules, graph)
	// Should only include 10 modules, not all 15
	assert.Contains(t, prompt, "展示前 10 个关键模块")
	assert.Contains(t, prompt, "mod0.go")
	assert.Contains(t, prompt, "mod9.go")
	assert.NotContains(t, prompt, "mod14.go")
}

func TestStreamCollectWithLiveness(t *testing.T) {
	ch := make(chan string, 3)
	ch <- "hello "
	ch <- "world"
	close(ch)

	result, completed := streamCollectWithLiveness(context.Background(), ch, 5*time.Second)
	assert.True(t, completed)
	assert.Equal(t, "hello world", result)
}
