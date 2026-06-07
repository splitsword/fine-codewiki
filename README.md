# CodeWiki —— 代码活体百科全书

> 纯本地 CLI 工具，将任意代码仓库转化为交互式 Wiki。自动生成架构图、类图、时序图，支持自然语言问答。代码永不出本机。

[![Go Version](https://img.shields.io/badge/Go-1.26%2B-blue)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Coverage](https://img.shields.io/badge/coverage-%E2%89%A585%25-brightgreen)](.coverage)

---

## 核心特性

- **完全本地运行**：代码分析、文档生成、问答全在本地完成，源码永不离开本机
- **双模 LLM**：支持远程 API（OpenAI / Claude / Gemini / 智谱等）或本地部署（Ollama / LocalAI）
- **学习体验优先**：主题导向叙事文档（项目概述 / 能做什么 / 架构说明 / 核心概念 / 学习路径），设计决策显性化，按"概念→架构→实现"组织，而非模块清单罗列
- **自动图表生成**：基于 AST 静态分析自动生成架构图、类图、时序图、依赖图（Mermaid DSL），图表在叙事段落中穿插，支持全屏查看
- **自然语言问答**：RAG 检索增强生成，答案附带源码文件路径与行号，支持多轮对话
- **来源溯源弹窗**：每个段落标注来源文件，点击弹出暗色代码窗口，语法高亮显示源码内容
- **静态 HTML 导出**：一键导出带三栏布局（导航+内容+文件树）的完整离线 Wiki，支持暗色主题、Ctrl+K 搜索、滚动导航高亮
- **多语言支持**：Python、JavaScript / TypeScript、Go、Java（持续扩展中），自动检测项目语言
- **单二进制零依赖**：Go 编译为单一可执行文件，跨平台（macOS / Linux / Windows）
- **增量索引**：基于文件修改时间和大小的增量向量索引，避免重复计算
- **异步并行生成**：4 阶段并发管线，LLM 调用全并行 + 依赖型并行 + 主题级并行，总耗时 2-5 分钟
- **流式优先 LLM**：流式 + 非流式双路径降级，3 级渐进 retry，Thinking/Reasoning 模式自动适配

---

## 安装

### 一键安装（推荐）

**Linux / macOS：**

```bash
curl -fsSL https://raw.githubusercontent.com/splitsword/fine-codewiki/main/scripts/install.sh | sh
```

**Windows（PowerShell）：**

```powershell
irm https://raw.githubusercontent.com/splitsword/fine-codewiki/main/scripts/install.ps1 | iex
```

### Homebrew（macOS / Linux）

> 即将推出：需要创建 `splitsword/homebrew-tap` 仓库后方可使用。
>
> 当前请使用一键安装脚本或从 [Releases](https://github.com/splitsword/fine-codewiki/releases) 下载预编译二进制。

<!--
```bash
brew tap splitsword/fine-codewiki
brew install codewiki
```
-->

### 使用 Go 安装

```bash
go install github.com/splitsword/fine-codewiki/cmd/codewiki@latest
```

### 从源码编译

```bash
git clone https://github.com/splitsword/fine-codewiki.git
cd fine-codewiki
go build -o codewiki ./cmd/codewiki
```

### 预编译二进制

前往 [Releases](https://github.com/splitsword/fine-codewiki/releases) 页面下载对应平台的可执行文件。

---

## 快速开始

### 1. 初始化配置

```bash
./codewiki config
```

按提示选择 LLM 模式（API / 本地 Ollama）并填入相应参数。

### 2. 生成 Wiki

在项目根目录执行：

```bash
./codewiki generate
```

CodeWiki 将自动：
- 解析仓库中的源码文件（Python / JS / TS / Go / Java）
- 构建模块依赖图与调用链
- 生成架构图、类图、API 参考文档
- 输出至 `.codewiki/wiki/` 目录

### 3. 本地预览

```bash
./codewiki serve
```

打开浏览器访问 `http://localhost:8080`，即可浏览生成的 Wiki、查看交互式图表、进行自然语言问答。

### 4. 一键浏览（生成 + 自动打开浏览器）

```bash
./codewiki browse
```

最简上手方式：自动完成 generate + serve 的全流程，无需手动切换命令。

### 5. 终端问答

```bash
./codewiki ask
```

进入交互式问答终端，用自然语言向代码库提问：

```
> User 类有哪些方法？
> 认证流程涉及哪些模块？
```

每个回答均附带引用的源代码文件路径与行号，支持连续追问。

---

## 支持的图表类型

| 图表 | 说明 | 示例场景 |
|------|------|----------|
| **架构图** | 模块依赖拓扑与层次结构 | 理解系统分层与模块职责 |
| **类图** | UML 风格类定义及继承关系 | 理清对象模型与继承链 |
| **时序图** | 函数调用序列与交互流程 | 追踪请求处理链路 |
| **依赖图** | 模块间 import 依赖全貌 | 发现循环依赖与耦合点 |

所有图表均以 **Mermaid DSL** 纯文本格式输出，可直接嵌入 Markdown，在 GitHub / GitLab 中渲染。

---

## 技术架构

```
CLI 入口 (Go)
    │
    ├── 代码分析引擎 (tree-sitter) ──▶ 代码图谱 (内存图 + JSON)
    │                                     │
    ├── 文档生成引擎 (LLM) ◀──────────────┘
    │
    ├── 图表引擎 (Mermaid DSL) ◀──────────┘
    │
    ├── 问答引擎 (RAG + 向量索引) ◀───────┘
    │
    └── LLM 适配层 (API / Ollama / 自定义 URL)
```

### 核心技术选型

| 层次 | 技术 | 说明 |
|------|------|------|
| CLI 语言 | Go 1.26+ | 单二进制分发，零依赖，跨平台 |
| AST 解析 | gotreesitter (纯 Go) | 无 CGO，支持增量解析与错误恢复 |
| 图表 DSL | Mermaid | 纯文本、Markdown 原生渲染、Git 友好 |
| 向量存储 | SQLite (modernc.org/sqlite) | 纯 Go SQLite，本地文件存储，零服务依赖 |
| 代码图谱 | 内存图 + JSON 持久化 | 依赖关系天然适合图结构 |
| LLM 客户端 | OpenAI 兼容接口 | 统一适配远程 API 与本地 Ollama |

---

## 项目结构

```
fine-codewiki/
├── cmd/codewiki/          # CLI 入口
├── internal/
│   ├── analyzer/          # AST 解析引擎（多语言，基于 gotreesitter 纯 Go 绑定）
│   ├── benchmark/         # 问答评测集与评估逻辑
│   ├── cache/             # 分析结果缓存（避免重复解析）
│   ├── chunker/           # 代码语义分块
│   ├── cli/               # CLI 命令实现（generate / serve / ask / config）
│   ├── config/            # 配置管理
│   ├── diagram/           # Mermaid DSL 生成与校验
│   ├── docgen/            # 文档生成引擎（LLM 叙事化文档 + 静态 HTML）
│   ├── embedder/          # Embedding 接口
│   ├── exporter/          # 静态 HTML 导出（三栏布局 + 文件树）
│   ├── grapher/           # 依赖图构建与社区检测
│   ├── llm/               # LLM 适配层（流式优先 + 3 级降级）
│   ├── qa/                # 问答评测框架
│   ├── rag/               # RAG 检索与问答
│   ├── sequencer/         # 时序图生成
│   ├── server/            # 本地 Web 服务（serve 模式）
│   ├── testutil/          # 测试工具函数
│   └── vectorstore/       # 向量存储（SQLite 后端）
├── benchmark/             # 评测数据集
├── testdata/              # 测试固件
└── prd.md                 # 产品需求文档
```

---

## 测试

本项目采用 **TDD（测试驱动开发）** 流程，所有功能均先写测试后写实现。

```bash
# 运行全部测试
go test -race -count=1 ./...

# 查看覆盖率
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

### 覆盖率目标

| 模块 | 行覆盖率 |
|------|----------|
| AST 解析引擎 | ≥ 90% |
| 代码图谱构建 | ≥ 85% |
| Mermaid DSL 生成 | ≥ 90% |
| RAG 问答引擎 | ≥ 80% |
| 向量存储 | ≥ 80% |
| **整体项目** | **≥ 85%** |

---

## 路线图

- **M1**（已完成）：核心可行原型 —— AST 解析、文档生成、架构图/类图、本地 Web 预览
- **M2**（已完成）：问答与图表增强 —— RAG 问答、时序图、本地 LLM 适配、增量索引、多轮对话
- **M3**（已完成，v1.0 Beta 已发布）：
  - ✅ 主题导向文档（What it Does / Architecture / Project Structure / Key Concepts / Learning Path）
  - ✅ LLM 叙事化改造 + 设计决策显性化 + 学习路径引导
  - ✅ 架构说明多图叙事化（功能架构图 + 技术架构图双图穿插）
  - ✅ 静态 HTML 导出（三栏布局、文件树、离线可浏览）
  - ✅ Web UI 全面升级（暗色主题、阅读进度、Ctrl+K 搜索、代码复制、图表全屏）
  - ✅ 右侧面板整合搜索与 AI 问答（serve 模式双 Tab 面板，替代旧弹窗和独立 /ask 页面）
  - ✅ 流式 AI 回答（实时流式输出，来源可点击弹出源码窗口）
  - ✅ 来源溯源弹窗（点击查看源码，语法高亮）
  - ✅ 流式优先 LLM + 3 级渐进降级 + Thinking 模式适配
  - ✅ 4 阶段异步并行生成管线
  - ✅ 图表质量升级（节点角色标注、颜色图例、边类型区分）
  - 🟡 性能优化（部分完成）
  - ✅ GitHub Releases 自动发布 + `codewiki update` 自更新
  - ✅ PDF 导出（纯 Go 零依赖，CJK 中文字体自动检测）
  - ❌ Homebrew / npm / winget（待后续补全）
- **V2 规划**：生态扩展 —— Rust / C++ 支持、VS Code 扩展、CI 集成（GitHub Action）、图结构自然语言查询

详见 [prd.md](prd.md)。

---

## 许可证

[MIT](LICENSE)

---

<p align="center">Built with Go. Code stays local.</p>

> 最后更新：2026-06-08

---

## CLI 帮助

```
fine-codewiki — turn any codebase into an interactive wiki

Usage:
  codewiki <command> [flags]

Commands:
  generate   Analyze code and generate wiki documentation
  browse     Generate (if needed) and open wiki in browser
  serve      Start a local HTTP server to preview the wiki
  ask        Ask a natural-language question about the codebase
  export     Export wiki to PDF or other formats
  config     Configure LLM provider and API settings
  update     Check for and install the latest version
  version    Print version information
  help       Show this help message

Generate flags:
  -source string   Source code directory (default ".")
  -output string   Output directory for wiki files
  -lang string     Language filter: python, javascript, typescript, go, java, rust, c, cpp
  -name string     Project name
  -max-functions   Max functions for LLM semantic description: -1=auto, 0=skip, N=cap
  -force           Force full regeneration, ignore checkpoints

Browse flags:
  -source string   Source code directory (default ".")
  -output string   Output directory for wiki files
  -name string     Project name

Export pdf flags:
  -source string   Source code directory (default ".")
  -dir string      Wiki directory to export (default "<source>/.codewiki/wiki")
  -output string   Output PDF file path (default "<project-name>.pdf")
  -lang string     Language filter
  -name string     Project name

Serve flags:
  -dir string      Wiki directory to serve (default "./.codewiki/wiki")
  -port int        HTTP server port (default 8080)
  -source string   Source directory for RAG Q&A (default: current dir)

Ask flags:
  -source string   Source code directory (default ".")
  -interactive     Start interactive Q&A session

Config flags:
  -path string     Config file path

Examples:
  codewiki generate --source ./my-project --name "My Project" --lang go
  codewiki browse
  codewiki serve --port 3000
  codewiki ask "What does the auth module do?"
  codewiki ask --interactive
  codewiki config
  codewiki export pdf --source ./my-project --dir ./my-project/.codewiki/wiki --output ./my-project.pdf
```
