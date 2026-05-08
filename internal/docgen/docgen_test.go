package docgen

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	// Should contain module list
	assert.Contains(t, md, "## 模块")

	// Should contain module list
	assert.Contains(t, md, "models/user")
	assert.Contains(t, md, "services/user_service")
	assert.Contains(t, md, "utils/logger")
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

	// Should contain role-based descriptions
	assert.Contains(t, md, "核心领域")
	assert.Contains(t, md, "入口层")
	assert.Contains(t, md, "工具库")
	assert.Contains(t, md, "`models/user`")
	assert.Contains(t, md, "`main`")
	assert.Contains(t, md, "`utils/logger`")
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

	md, err := GenerateAPIReferenceMarkdown(graph)
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
	archDSL := "graph TD\n    models_user --> models_base\n"

	md, err := GenerateArchitectureMarkdown(graph, archDSL)
	require.NoError(t, err)
	require.NotEmpty(t, md)

	assert.Contains(t, md, "# 架构")
	assert.Contains(t, md, "## 模块概览")
	assert.Contains(t, md, "| 模块 | 角色 | 类型 | 依赖 | 被依赖 |")
	assert.Contains(t, md, "## 依赖图")
	assert.Contains(t, md, "```mermaid")
	assert.Contains(t, md, archDSL)
	assert.Contains(t, md, "```")
}

func TestWriteWikiFiles(t *testing.T) {
	tmpDir := t.TempDir()
	wikiDir := filepath.Join(tmpDir, ".codewiki", "wiki")

	wiki := &Wiki{
		ProjectName:   "test-project",
		Overview:      "# test-project\n",
		APIReference:  "# API Reference\n",
		Architecture:  "# Architecture\n",
		ClassDiagram:  "classDiagram\n",
		ArchitectureDiagram: "graph TD\n",
	}

	err := WriteWikiFiles(wikiDir, wiki)
	require.NoError(t, err)

	// Verify files exist
	assert.FileExists(t, filepath.Join(wikiDir, "overview.md"))
	assert.FileExists(t, filepath.Join(wikiDir, "api-reference.md"))
	assert.FileExists(t, filepath.Join(wikiDir, "architecture.md"))
	assert.FileExists(t, filepath.Join(wikiDir, "class-diagram.mmd"))
	assert.FileExists(t, filepath.Join(wikiDir, "architecture.mmd"))
	assert.FileExists(t, filepath.Join(wikiDir, "compilation.md"))

	// Verify content
	content, err := os.ReadFile(filepath.Join(wikiDir, "overview.md"))
	require.NoError(t, err)
	assert.Equal(t, "# test-project\n", string(content))

	// Verify compilation contains all sections
	compContent, err := os.ReadFile(filepath.Join(wikiDir, "compilation.md"))
	require.NoError(t, err)
	comp := string(compContent)
	assert.Contains(t, comp, "# test-project Wiki 合辑")
	assert.Contains(t, comp, "## 目录")
	assert.Contains(t, comp, "## test-project")
	assert.Contains(t, comp, "## Architecture")
	assert.Contains(t, comp, "## API 参考")
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
	assert.Contains(t, wiki.Overview, "models/base")

	// Verify architecture doc contains embedded diagram
	assert.Contains(t, wiki.Architecture, "```mermaid")
	assert.Contains(t, wiki.Architecture, archDSL)
}

// ---------- Mock LLM Tests ----------

type mockProvider struct {
	response string
	err      error
}

func (m *mockProvider) Complete(ctx context.Context, prompt string) (string, error) {
	return m.response, m.err
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
	wiki, err := GenerateWikiEnhanced(context.Background(), mock, graph, "", "ai-project", "graph TD\n", "classDiagram\n", "sequenceDiagram\n")

	require.NoError(t, err)
	require.NotNil(t, wiki)

	// Should contain LLM enhancement
	assert.Contains(t, wiki.Overview, "AI-enhanced project overview")
	// Should also contain static content
	assert.Contains(t, wiki.Overview, "ai-project")
	assert.Contains(t, wiki.Overview, "models/user")
}

func TestGenerateWikiWithLLMFallback(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "models/user.py", Classes: []analyzer.ClassInfo{{Name: "User"}}},
	}
	graph := grapher.BuildGraph(files)

	// LLM returns error — should fall back to static generation
	mock := &mockProvider{err: errors.New("llm unavailable")}
	wiki, err := GenerateWikiEnhanced(context.Background(), mock, graph, "", "fallback-project", "graph TD\n", "classDiagram\n", "sequenceDiagram\n")

	require.NoError(t, err)
	require.NotNil(t, wiki)

	// Should still contain static content, no panic
	assert.Contains(t, wiki.Overview, "fallback-project")
	assert.Contains(t, wiki.Overview, "models/user")
	// Should NOT contain enhanced header format since LLM failed
	assert.False(t, strings.Contains(wiki.Overview, "---"))
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
	assert.Contains(t, wiki.Architecture, "模块概览")
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
	desc := describeFunction("foobar", []string{}, "")
	assert.Contains(t, desc, "foobar")
	assert.Contains(t, desc, "执行")
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

	md, err := GenerateAPIReferenceMarkdown(graph)
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
		Architecture:        "# 架构\n\n架构说明内容。\n",
		APIReference:        "# API 参考\n\nAPI 内容。\n",
		ArchitectureDiagram: "graph TD\n    A --> B\n",
		ClassDiagram:        "classDiagram\n    class A\n",
		SequenceDiagram:     "sequenceDiagram\n    A->>B: msg\n",
	}

	comp := GenerateMarkdownCompilation(wiki)

	// 应该包含合辑标题
	assert.Contains(t, comp, "# demo Wiki 合辑")

	// 应该包含目录
	assert.Contains(t, comp, "## 目录")
	assert.Contains(t, comp, "1. [项目概述](#项目概述)")
	assert.Contains(t, comp, "2. [架构说明](#架构说明)")
	assert.Contains(t, comp, "3. [API 参考](#api-参考)")

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

	// 应该包含嵌入的图表
	assert.Contains(t, comp, "## 架构图")
	assert.Contains(t, comp, "## 类图")
	assert.Contains(t, comp, "## 时序图")
	assert.Contains(t, comp, "```mermaid")
}
