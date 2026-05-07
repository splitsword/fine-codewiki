# CodeWiki —— 代码活体百科全书

> 纯本地 CLI 工具，将任意代码仓库转化为交互式 Wiki。自动生成架构图、类图、时序图，支持自然语言问答。代码永不出本机。

[![Go Version](https://img.shields.io/badge/Go-1.26%2B-blue)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Coverage](https://img.shields.io/badge/coverage-%E2%89%A585%25-brightgreen)](.coverage)

---

## 核心特性

- **完全本地运行**：代码分析、文档生成、问答全在本地完成，源码永不离开本机
- **双模 LLM**：支持远程 API（OpenAI / Claude / Gemini / 智谱等）或本地部署（Ollama / LocalAI）
- **自动图表生成**：基于 AST 静态分析自动生成架构图、类图、时序图、依赖图（Mermaid DSL）
- **自然语言问答**：RAG 检索增强生成，答案附带源码文件路径与行号，支持多轮对话
- **多语言支持**：Python、JavaScript / TypeScript、Go、Java（持续扩展中）
- **单二进制零依赖**：Go 编译为单一可执行文件，跨平台（macOS / Linux / Windows）
- **增量索引**：基于文件修改时间和大小的增量向量索引，避免重复计算

---

## 安装

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

### 4. 终端问答

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
│   ├── analyzer/          # AST 解析引擎（多语言）
│   ├── benchmark/         # 问答评测集与评估逻辑
│   ├── chunker/           # 代码语义分块
│   ├── cli/               # CLI 命令实现
│   ├── config/            # 配置管理
│   ├── diagram/           # Mermaid DSL 生成与校验
│   ├── docgen/            # 文档生成引擎
│   ├── embedder/          # Embedding 接口
│   ├── grapher/           # 依赖图构建与社区检测
│   ├── llm/               # LLM 适配层
│   ├── rag/               # RAG 检索与问答
│   ├── sequencer/         # 时序图生成
│   ├── server/            # 本地 Web 服务
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
- **M3**（规划中）：产品化打磨 —— 导出 HTML/PDF、性能优化、安装分发（Homebrew / npm / winget）
- **M4**（规划中）：生态扩展 —— Rust / C++ 支持、插件系统、VS Code 扩展

详见 [prd.md](prd.md)。

---

## 许可证

[MIT](LICENSE)

---

<p align="center">Built with Go. Code stays local.</p>

> 最后更新：2026-05-07
