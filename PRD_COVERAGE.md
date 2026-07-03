# PRD 覆盖追踪文档

> 本文档将 PRD 中的能力、测试、里程碑、发布门禁映射到实际实现状态。每次提交前更新对应条目，确保无遗漏。
> 文档版本：v0.5 | 最后更新：2026-05-29

---

## 一、里程碑总览

| 里程碑 | 状态 | 核心交付 | 阻塞项 |
|--------|------|----------|--------|
| M1 — 核心可行原型 | ✅ 已完成 | AST/文档/图表/CLI/Web | 无 |
| M2 — 问答与图表增强 | ✅ 已完成 | RAG/时序图/本地LLM/配置/中文 | 无 |
| M3 — 产品化打磨 | ✅ 已完成 | 主题导向叙事文档、多图叙事化架构说明、静态 HTML/PDF 导出、流式 AI 问答、Web UI 14项特性、4 阶段并发管线、流式优先 LLM、update 自更新、GitHub Releases 自动发布 | 无 |
| M3.5 — 规模化可靠性加固 | ✅ 已完成 | 失败重试队列、checkpoint 函数级续传、增量不清盘、降级独立超时、idle 自适应、并发可配+流式429退避 | 无 |
| M4-B — 信息架构升级 | ✅ 已完成 | modules 文档 LLM 增强(重要度+被引用覆盖)、API 参考按模块分组、serve 语义搜索+8项体验 | 无 |
| M4 — 生态扩展 | ⏳ 延后至 V2 | Rust/C++ 支持、VS Code 扩展、CI 集成（GitHub Action）、图结构自然语言查询 | 无 |

---

## 二、M1 交付物细拆

| # | PRD 能力 | 实现文件 | 测试文件 | 状态 | 备注 |
|---|----------|----------|----------|------|------|
| 1.1 | AST 解析引擎（Python/JS/TS/Go/Java） | `internal/analyzer/` | `analyzer/*_test.go` (31 tests) | ✅ | tree-sitter grammar 已捆绑（`gotreesitter/grammars`），tags query 提取定义，AST walk 提取 import，正则回退兜底 |
| 1.2 | LLM 文档生成（概述/架构/API参考） | `internal/docgen/docgen.go` | `docgen/docgen_test.go` (11 tests) | ✅ | 含 LLM 增强 + 静态回退 + README 注入 |
| 1.3 | 架构图生成（Mermaid graph TD） | `internal/diagram/arch.go` + `internal/grapher/` | `diagram/*_test.go` + `grapher/*_test.go` | ✅ | 含循环依赖检测、社区检测 |
| 1.4 | 类图生成（Mermaid classDiagram） | `internal/diagram/class.go` | `diagram/class_test.go` | ✅ | 支持继承、方法、可见性 |
| 1.5 | CLI 框架（generate/serve/ask/config） | `cmd/codewiki/main.go` + `internal/cli/cli.go` | `cli/cli_test.go` (47 tests) | ✅ | 含交互式配置向导 |
| 1.6 | Wiki 输出（Markdown + Mermaid） | `internal/docgen/writer.go` | `docgen/docgen_test.go` | ✅ | 6 个输出文件 |
| 1.7 | 本地 Web 预览（HTTP + Mermaid.js） | `internal/cli/cli.go` (serve) | `cli/cli_test.go` | ✅ | CDN 加载 Mermaid.js |

### M1 成功标准核对

| 标准 | 状态 | 验证方式 |
|------|------|----------|
| 为 FastAPI 生成完整 Wiki | ✅ | `testdata/repos/python-basic` E2E 测试 |
| 生成时间 < 5 分钟（10万行） | ⚠️ | 未建立性能基准，无自动化验证 |
| 单二进制跨平台 | ✅ | `go build` 产出单文件；CI 已配置跨平台矩阵 |
| 全程本地运行 | ✅ | 代码零上传，Ollama 本地模型支持 |

### M1 测试计划核对

| 测试项 | 状态 | 实现方式 |
|--------|------|----------|
| AST：降级处理语法错误 | ✅ | `analyzer` 测试中覆盖 |
| AST：空文件/嵌套/import | ✅ | `analyzer` 测试中覆盖 |
| AST：并发解析无数据竞争 | ✅ | 目录遍历 + 并发解析测试 |
| Docgen：空仓库/单文件/多模块 | ✅ | `docgen` 测试中覆盖 |
| Docgen：Prompt 快照对比 | ✅ | `testdata/expected/prompts/` 存储 overview/architecture prompt 快照 |
| Docgen：LLM 无效 JSON 降级 | ✅ | OpenAI 端 JSON 解析失败时返回包含原始响应的错误；Ollama 端已有处理 |
| Diagram：循环依赖/空类/大图 | ✅ | `diagram` + `grapher` 测试中覆盖 |
| Diagram：Mermaid 语法校验 | ✅ | `internal/diagram/mmdc_test.go` 使用 `mmdc` CLI 验证快照和生成图表语法 |
| Diagram：DSL 内容稳定性 | ✅ | 确定性生成测试 |
| CLI：E2E generate 全流程 | ✅ | `TestGenerateCommand` |
| CLI：serve HTTP 可访问 | ✅ | `TestRunServeStarts` |
| CLI：跨平台编译 | ✅ | CI 矩阵: ubuntu/macOS/windows × Go 1.23/1.24 |

---

## 三、M2 交付物细拆

| # | PRD 能力 | 实现文件 | 测试文件 | 状态 | 备注 |
|---|----------|----------|----------|------|------|
| 2.1 | 代码向量化（语义分块） | `internal/chunker/` | `chunker/chunker_test.go` (6 tests) | ✅ | 类/函数/模块/import 分块 |
| 2.2 | Embedding + 本地向量存储 | `internal/embedder/` + `internal/vectorstore/` | `embedder/*_test.go` (6) + `vectorstore/*_test.go` (16) | ✅ | SQLite + 内存双后端，余弦相似度 |
| 2.3 | RAG 问答（ask 命令） | `internal/rag/` + `internal/cli/cli.go` | `rag/rag_test.go` (11) + `cli/cli_test.go` | ✅ | 含源码引用、多轮对话 |
| 2.4 | 时序图生成 | `internal/sequencer/` | `sequencer/sequencer_test.go` (19 tests) | ✅ | 含空内容 bug 修复 |
| 2.5 | 本地 LLM 适配（Ollama） | `internal/llm/llm.go` | `llm/llm_test.go` (30 tests) | ✅ | 含重试、超时、429 处理 |
| 2.6 | LLM 配置系统（config 向导） | `internal/cli/cli.go` + `internal/llm/llm.go` | `cli/cli_test.go` + `llm/llm_test.go` | ✅ | 双配置 generation + embedding |
| 2.7 | 多语言扩展（Go/Java） | `internal/analyzer/go.go` + `java.go` | `analyzer/go_test.go` + `java_test.go` | ✅ | 含类/函数/import 提取 |
| 2.8 | 全中文输出 | 全项目 | 全项目 | ✅ | CLI + Wiki + Prompt 全部中文 |

### M2 成功标准核对

| 标准 | 状态 | 验证方式 |
|------|------|----------|
| 自然语言提问 + 源码引用回答 | ✅ | `TestRunAskWithStore` 集成测试 |
| 时序图正确反映调用链 | ✅ | `sequencer` 多语言测试覆盖 |
| 本地 Ollama 走通全流程 | ✅ | `llm` 端到端测试 + CLI E2E |
| 问答准确率 > 80% | ✅ | `benchmark/qa_bench.json` 5/5 cases 通过（基于确定性 symbol embedder 的端到端管道验证） |
| 非代码描述全中文 | ✅ | 人工验证通过 |

### M2 测试计划核对

| 测试项 | 状态 | 实现方式 |
|--------|------|----------|
| 向量化：函数边界不被切断 | ✅ | `chunker` 测试 |
| 向量化：Top-3 检索准确 | ✅ | `vectorstore` 相似度测试 |
| 向量化：大规模索引 < 500ms | ⚠️ | 无性能基准；仅单元测试 |
| 向量化：增量索引 | ✅ | `RunAsk` 中使用 `ShouldIndexFile` + `MarkFileIndexed` 实现增量更新 |
| RAG：无关问题礼貌拒答 | ✅ | `rag` 测试中覆盖空检索场景 |
| RAG：多轮追问上下文保持 | ✅ | `Engine` 含 `History` 字段 |
| 时序图：单层/多层/递归/异步 | ✅ | `sequencer` 测试覆盖 |
| LLM：API 超时重试 | ✅ | `llm` mock 测试覆盖 |
| LLM：429 retry-after | ✅ | `llm` mock 测试覆盖 |
| LLM：Ollama 未启动提示 | ✅ | `OllamaProvider.post()` 检测连接被拒绝错误，返回友好提示"无法连接到 Ollama 服务..." |

---

## 四、M3 交付物细拆（当前进行中）

| # | PRD 能力 | 实现文件 | 状态 | Gap / 下一步 |
|---|----------|----------|------|--------------|
| 3.1 | 函数级逻辑分析 | `internal/docgen/docgen.go` | ✅ | `describeFunction()` 静态推断已覆盖 30+ 动词模式；`selectTopFunctions` + LLM batch prompt 已为前 5 个关键函数生成语义描述并注入 API Reference |
| 3.2 | 模块职责推断 | `internal/docgen/docgen.go` + `internal/grapher/grapher.go` | ✅ | `InferModuleRoles()` 已实现 PageRank + 角色标签（核心领域/入口层/工具库/业务模块/支撑模块/独立模块）；已集成到 `buildAutoDescription` |
| 3.3 | 调用链路语义描述 | `internal/sequencer/sequencer.go` + `internal/cli/cli.go` + `internal/docgen/docgen.go` | ✅ | `generateSequenceDescription()` 静态场景描述已集成到 Wiki 输出；Markdown 合辑/HTML/PDF 均展示可见的场景描述文字 |
| 3.4 | 图表语义标注 | `internal/diagram/diagram.go` + `internal/sequencer/sequencer.go` | ✅ | 基础静态 `%%` 注释已覆盖四种图表类型（架构图/类图/依赖图/时序图）；LLM 深度场景描述通过 `Sequence.Description` 和架构/概述增强已实现 |
| 3.5 | Wiki 导出 Markdown 合辑 | `internal/docgen/docgen.go` | ✅ | `GenerateMarkdownCompilation()` + `WriteWikiFiles` 自动生成 `compilation.md` |
| 3.6 | 导出静态 HTML | `internal/docgen/docgen.go` + `internal/cli/cli.go` | ✅ | `GenerateStaticHTML()` 生成单文件离线 HTML（含导航、Mermaid.js CDN、CSS）；`WriteWikiFiles` 自动输出 `index.html`；`serve` 保留实时渲染 |
| 3.7 | 导出 PDF | `internal/docgen/pdf.go` | ✅ | `GeneratePDF()` + `WriteWikiFiles` 集成；跨平台 CJK 字体自动探测；Markdown 渲染（标题/段落/列表/代码块/表格/分隔线）；图表附录以 DSL 代码块输出 |
| 3.7b | **Web UI 体验升级** | `internal/docgen/render.go` + `internal/cli/cli.go` | ✅ | **14 项 UI 特性：** 磨砂玻璃态(backdrop-filter)、暗色主题(CSS 变量+localStorage)、阅读进度条、代码块语言标签+复制、图表全屏展开、折叠导航分组(入门/动态/探索)、Ctrl+K 搜索覆盖层、Ask AI 快捷入口、滚动导航高亮(IntersectionObserver)、阅读时长徽章(EstimateReadingTime)、难度星级徽章(articleDifficulty)、Mermaid 点击导航(navigateToModule)、版本历史备份(.bak)、生成时间戳；Serve/Static 双路径共享 wikiPageCSS + wikiPageJS |
| 3.7c | **章节叙事 LLM 生成** | `internal/docgen/docgen.go` + `internal/docgen/chapter_page.go` | ✅ | `buildChapterNarrativePrompt()` 构建跨模块教学叙事 prompt；`generateChapterNarratives()` 逐主题 LLM 调用（5 分钟超时、清单检测、降级回退）；`GenerateChapterPage()` 叙事优先 + 折叠式模块详情参考区；checkpoint 持久化；无 LLM 时保持原有拼接行为 |
| 3.7d | **LLM 生成可靠性工程 (P0)** | `internal/llm/llm.go` + `internal/docgen/docgen.go` | ✅ | **流式优先架构**：所有 LLM 调用改为 `streamComplete()` 流式优先 + 非流式自动降级；**超时机制重构**：移除 `http.Client.Timeout`，新增 `StreamClient`（无 Timeout），所有超时由 context deadline 控制；**渐进降级**：章节叙事 3 级降级（完整 prompt 流式 15min → 精简 prompt 流式 10min → 极简 prompt 非流式 8min）；**Thinking/Reasoning 模式**：DeepSeek `thinking: {type: "enabled"}` + OpenAI `reasoning_effort: "high"` 双注入，`CODEWIKI_THINKING` 环境变量控制；**Prompt 规模控制**：模块/函数数量截断 + prompt 字符数日志；**活性检测**：流式接收 3 分钟无新 token 自动超时降级；所有 context 超时适配 thinking 模式（8-15 分钟） |
| 3.8 | Homebrew/npm/winget 分发 | `scripts/install.sh` + `scripts/install.ps1` + `.github/workflows/release.yml` | 🟡 | GitHub Releases + 一键安装脚本已就绪；Homebrew tap 仓库/npm/winget manifest 待创建 |
| 3.9 | 大规模仓库性能优化 | `internal/cli/cli.go` (RunAsk) + `benchmark/benchmark_test.go` | ✅ | AST+Graph 缓存已实现，增量索引已实现；`BenchmarkEndToEnd100K` 验证 100K 行生成 < 4s；CI 性能门禁已配置 |
| 3.10 | 提示词优化（按语言）+ Prompt 快照回归 | `internal/docgen/docgen.go` | ✅ | prompt 已统一中文；按语言定制模板已实现（Python/Go/JS/Java/Rust/C++）；快照回归机制已建立 (`testdata/expected/prompts/`) |
| 3.11 | Rust / C++ AST 支持 + tree-sitter grammar 捆绑 | `internal/analyzer/analyzer.go` | ✅ | Rust/C++ 正则解析已实现；tree-sitter grammar 已捆绑（`gotreesitter/grammars` 自动检测 + tags query 提取），含 regex 兜底回退 |
| 3.12 | Beta 公开发布 | `README.md` + `CHANGELOG.md` + `homepage/index.html` | ✅ | v1.0 Beta 已发布：README/CHANGELOG/PRD/主页全部同步；GitHub Releases + update 自更新就绪；Homebrew/npm/winget 待 V2 补全 |
| 3.13 | 工程基建：性能基准套件 + 预期 DSL 快照 + mmdc 语法校验 + CI 跨平台矩阵 | `benchmark/benchmark_test.go` + `internal/testutil/snapshot.go` | ✅ | 性能基准套件已建立；图表 DSL 快照已建立；mmdc 语法校验已实现 (`internal/diagram/mmdc_test.go`)；CI 跨平台矩阵已实现 (ubuntu/macOS/windows × Go 1.26) |

### M3 核心痛点回应追踪

| 痛点 | PRD 方案 | 当前状态 | 负责切片 |
|------|----------|----------|----------|
| 函数级语义摘要 | AST 控制流 + LLM | ✅ `describeFunction()` 静态推断 + `selectTopFunctions` LLM batch prompt 为前 5 个关键函数生成语义描述 | #3.1 |
| 模块角色推断 | 依赖图 + PageRank | ✅ `InferModuleRoles()` 已实现并集成 | #3.2 |
| 图表语义层 | Mermaid `%%` 注释 | ✅ 基础静态注释已覆盖四种图表类型；LLM 深度场景描述通过 `Sequence.Description` 和架构/概述增强实现 | #3.4 |
| 调用链语义描述 | 时序图场景文字 | ✅ `Sequence.Description` 已集成到 compilation.md / HTML / PDF | #3.3 |
| 章节页面"文档拼接"体验 | LLM 跨模块叙事 | ✅ `generateChapterNarratives()` 为每个主题生成教学叙事文章，模块文档降级为折叠参考区；无 LLM 时向后兼容；含 3 级渐进降级 + 流式活性检测保障可靠性 | #3.7c, #3.7d |

### M3 成功标准核对

| 标准 | 状态 | 验证方式 |
|------|------|----------|
| 每个函数附带职责说明而非仅签名 | ✅ | `describeFunction()` 静态推断覆盖 30+ 动词模式；LLM batch prompt 为前 5 个关键函数生成语义描述 |
| 每个模块附带设计意图而非仅文件清单 | ✅ | `InferModuleRoles()` 已集成 PageRank 角色推断到 overview |
| 图表附带语义标注（`%%` 注释） | ✅ | 基础静态注释已覆盖四种图表类型；LLM 深度场景描述通过 `Sequence.Description` 和架构/概述增强实现 |
| 时序图附带场景描述文字 | ✅ | `Sequence.Description` 已暴露到 `Wiki.SequenceDescription`，在 compilation.md（引用块）、index.html（场景描述段落）、PDF（场景描述文本）中可见 |
| 导出 HTML 含可交互 Mermaid 图表 | ✅ | `GenerateStaticHTML()` 生成单文件离线 HTML，含 Mermaid.js CDN 和导航；`serve` 保留实时渲染 |
| 10 万行仓库 < 3 分钟生成 | ✅ | `BenchmarkEndToEnd100K` 验证约 100K 行合成项目生成耗时 < 4s（阈值 180s）；CI 已配置性能门禁步骤 |
| 向量存储支持增量索引 | ✅ | `RunAsk` 通过 `ShouldIndexFile` / `PruneFiles` / `MarkFileIndexed` 实现增量更新 |
| Prompt 变更可被回归测试发现 | ✅ | `testdata/expected/prompts/` 存储 overview/architecture prompt 快照；`SnapshotCompare` 支持 `-update` 更新 |
| 发布到 Homebrew/npm/winget | 🟡 | GitHub Releases + 一键脚本已就绪；包管理器待 V2 补全 |
| 发布后第一个月 100+ star | ⏳ | Beta 已发布，等待市场反馈 |
| **Web UI 对标 Zread 体验质量** | ✅ | 14 项 UI 特性已实现（磨砂玻璃态/暗色主题/阅读进度条/代码块复制/图表全屏/折叠导航/Ctrl+K搜索/Ask AI/滚动高亮/时长徽章/难度徽章/主题切换/图表导航/版本备份） |
| **Serve 与 Static HTML 双路径视觉一致** | ✅ | 共享 `wikiPageCSS` + `wikiPageJS`，渲染测试覆盖 |
| **新开发者 5 分钟内理解项目定位** | ✅ | 阅读时长徽章 + 难度星级 + "下一步阅读 →"导航 + 五分钟快速上手路径 |
| **暗色模式可用** | ✅ | CSS 变量双主题 + `localStorage` 持久化 + 顶栏一键切换 |
| **章节页面为教学叙事而非文档拼接** | ✅ | `generateChapterNarratives()` LLM 生成跨模块叙事；叙事优先 + 折叠模块参考区；`isChecklistLike` 清单检测 + 降级回退 |
| **LLM 生成全流程可靠性** | ✅ | 流式优先架构 + HTTP 超时机制重构（context-only timeout）+ 渐进降级 + 活性检测；thinking 模式默认启用；实际运行无 context deadline exceeded 超时 |

### M3 测试计划核对

| 测试项 | 状态 | 实现方式 |
|--------|------|----------|
| 函数级逻辑描述 | ✅ | `describeFunction()` 静态推断已覆盖 30+ 动词模式；LLM batch prompt 为前 5 个关键函数生成语义描述 |
| 模块职责推断 | ✅ | `grapher` 单元测试覆盖 PageRank + 角色分类 |
| 调用链路语义描述 | ✅ | `generateSequenceDescription()` 静态场景描述已实现并集成到 compilation.md / HTML / PDF |
| 空函数/抽象函数降级 | ✅ | `describeFunction()` 已检测空参数+空返回值，返回"占位函数"或"抽象方法" |
| LLM 幻觉检测 | ✅ | `detectHallucination()` 检测反引号和加粗标识符是否真实存在于代码库；阈值 ≥2 处或 >30% 触发回退到静态描述 |
| 架构图/类图语义标注 | ✅ | 静态 `%%` 注释已覆盖四种图表类型；architecture 增强已添加 `isChecklistLike` 检测 |
| 图表"清单化"检测 | ✅ | `isChecklistLike()` 已在 overview 和 architecture LLM 增强中实现，检测模块名覆盖率>70% 或列表标记行>50% 时回退到静态描述 |
| HTML 导出含 Mermaid 图表 | ✅ | `TestGenerateStaticHTML` + `TestWriteWikiFiles` 验证导航、内容区块、Mermaid 嵌入及 CDN |
| PDF 导出含中文字符 | ✅ | `TestGeneratePDF` + `TestWriteWikiFiles` 验证；Windows/macOS/Linux 系统字体探测 |
| 基准测试套件 (bench-10k/50k/100k) | ✅ | `benchmark/benchmark_test.go` 覆盖 E2E/解析/图构建/Wiki/PageRank；CI 性能门禁已配置 |
| 增量索引：新增文件 | ✅ | `RunAsk` 中使用 `ShouldIndexFile` + `MarkFileIndexed` 实现增量更新 |
| 增量索引：删除文件清理 | ✅ | `RunAsk` 中调用 `PruneFiles` 清理已删除文件的向量 |
| Prompt 快照回归 | ✅ | `testdata/expected/prompts/` 存储 overview/architecture prompt 快照；`SnapshotCompare` 支持 `-update` 更新 |
| 图表 DSL 预期快照 | ✅ | `testdata/expected/diagrams/python-basic/` 存储架构图/类图/依赖图/时序图快照；`SnapshotCompare` 支持 `-update` 更新 |
| Mermaid 语法校验 (mmdc) | ✅ | `internal/diagram/mmdc_test.go` 使用 `mmdc` CLI 验证快照和生成图表的语法正确性 |
| Rust / C++ AST 解析 | ✅ | `analyzer` 单元测试覆盖 struct/trait/impl/class/include/method |
| tree-sitter grammar 捆绑验证 | ✅ | `grammar_bundle.go` + `gotreesitter/grammars` 自动检测；`treesitter_extract.go` tags query 提取定义 + AST walk 提取 import；正则兜底回退 |
| CI 跨平台矩阵 | ✅ | GitHub Actions: ubuntu-latest / macos-latest / windows-latest × Go 1.26 |
| **代码块语言标签+复制** | ✅ | `TestMarkdownToHTMLCodeBlock` 验证 `<pre><code` + 语言标签 + 复制按钮 |
| **Mermaid 全屏展开** | ✅ | `TestGenerateStaticHTML` 验证 `mermaid-wrap` + expand 按钮 |
| **折叠导航分组** | ✅ | `TestWriteWikiFiles` + `TestGenerateStaticHTML` 验证 `nav-group` 结构 + 图标 span |
| **搜索覆盖层** | ✅ | `TestGenerateStaticHTML` 验证 `.search-overlay` + `filterStaticSearch` + Ctrl+K |
| **阅读时长估算** | ✅ | `EstimateReadingTime()` 中文 400 字/分钟，`cli.go` 注入 ⏱ 徽章 |
| **难度星级标注** | ✅ | `articleDifficulty()` 按文件名前缀分级 + 颜色编码 |
| **暗色主题** | ✅ | CSS `[data-theme="dark"]` + `localStorage` 持久化 |
| **Serve/Static 双路径一致性** | ✅ | `wikiPageCSS` + `wikiPageJS` 共享常量，两路径渲染测试覆盖 |
| **章节叙事 LLM 生成** | ✅ | `TestGenerateChapterPageWithNarrative` 验证叙事渲染 + 折叠模块详情；`TestGenerateChapterPageNarrativeFallback` 验证无叙事降级；`TestBuildChapterNarrativePrompt` 验证 prompt 要素；`TestGenerateChapterNarrativesFallback` 验证 provider=nil 降级 |
| **Sidebar 章节展开模块列表** | ✅ | `TestBuildStaticNavSectionsWithSubItems` 验证当前章节 NavItem 包含模块 SubItems；非当前章节无 SubItems；`nav-sub-items` CSS 嵌套渲染 |
| **学习目标/前置知识字段** | ✅ | `TestGenerateChapterPageWithGoals` 验证 ChapterTitle.LearningGoals/Prerequisites 渲染；`chapter-goals`/`chapter-prereqs` HTML 区块；LLM prompt 已请求 learning_goals/prerequisites 字段 |
| **右侧 TOC 锚点导航** | ✅ | `TestGenerateChapterPageTOC` 验证本章目录区域 + toc-list/toc-item；`TestExtractHeadings` 验证 h2/h3 提取；`TestExtractHeadingsStripsInnerHTML` 验证内联标签剥离；IntersectionObserver 滚动高亮 |
| **Markdown 标题 ID 属性** | ✅ | `RenderMarkdownBody` 为所有 h1-h6 生成 `id` 属性（`headingSlug` 函数）；`TestMarkdownToHTMLHeaders` 验证 |
| **流式优先架构** | ✅ | `TestGenerateChapterNarrativesStreamSuccess` 验证流式生成成功路径；`TestStreamCollectWithLiveness` 验证活性检测超时降级 |
| **渐进降级（3 级）** | ✅ | `TestGenerateChapterNarrativesProgressiveDegradation` 验证 L1 失败→L2 重试→L3 最终回退；`TestGenerateChapterNarrativesAllLevelsFail` 验证全失败回退 |
| **Prompt 规模控制** | ✅ | `TestBuildSimplifiedNarrativePrompt` 验证精简格式 + 10 模块上限；`TestBuildMinimalNarrativePrompt` 验证极简格式 + 15 模块上限；`TestBuildChapterNarrativePromptModuleCap` 验证完整 prompt 10 模块 + 3 函数上限 |
| **Thinking 模式** | ✅ | `injectThinking()` DeepSeek/OpenAI 双注入 + `CODEWIKI_THINKING` 环境变量；`TestApplyDefaultsThinkingEnabled` 验证默认启用 |
| **StreamClient 超时隔离** | ✅ | `OpenAIProvider.StreamClient` / `OllamaProvider.StreamClient` 无 `http.Client.Timeout`，仅由 context 控制 |
| **学习路径清单检测移除** | ✅ | 移除 `isChecklistLike` 对学习路径的验证（仅检查空内容）；学习路径自然含列表，不应被误判为清单 |
| **代码块样式一致性** | ✅ | `:not(pre) > code` 选择器隔离内联代码样式；移除 `pre code { color: inherit }` 让 highlight.js 语法高亮生效 |
| **入口点导航移除** | ✅ | 删除 EntryPoints 侧栏入口渲染（`graph.EntryPoints()` 返回零入度模块无意义）；项目结构区已覆盖所有模块 |
| **JS 语法错误修复** | ✅ | 删除 `wikiPageJS` 中多余的 `}` 和 `});` 闭合括号，恢复主题切换、代码复制、图表全屏、搜索、导航折叠、滚动监听所有 JS 功能 |

---

## M3.5 交付物细拆（计划中：v1.0 RC）

> 详见 `prd.md` § M3.5。每个任务含问题根因（file:line）、实现方案、测试方案、验收标准。

| # | 任务 | 实现文件 | 测试文件（计划） | 状态 | 优先级 |
|---|------|----------|------------------|------|--------|
| A1 | 失败函数描述批次重试队列 | `internal/docgen/docgen.go`（批次循环 499-545） | `docgen_test.go::TestFunctionDescRetry*` | ✅ 已完成 | 🔴 P0 |
| A2 | checkpoint 函数级精细化续传 | `internal/docgen/docgen.go`（459-465） | `docgen_test.go::TestFunctionDescCheckpoint*` | ✅ 已完成 | 🔴 P0 |
| A3 | 单文件改动不再清空整盘 checkpoint | `internal/cli/cli.go`（97-101） | `cli_test.go::TestGenerateIncremental*` | ✅ 已完成 | 🔴 P0 |
| A4 | 降级非流式加独立超时 | `internal/docgen/docgen.go` `streamComplete`（2198-2215） | `docgen_test.go::TestStreamDegradeUsesIndependentTimeout` | ✅ 已完成 | 🟠 P1 |
| A5 | 流式 idle 超时自适应（reasoning 感知） | `internal/docgen/docgen.go` `streamCollectWithLiveness`（2163-2192） | `docgen_test.go::TestStreamIdleTimeout*` | ✅ 已完成 | 🟠 P1 |
| A6 | 并发可配 + 流式 429 退避 | `cmd/codewiki/main.go` / `internal/llm/llm.go`（290-353） | `llm_test.go::TestCompleteStream429Retry` | ✅ 已完成 | 🟡 P2 |

### M3.5 成功标准核对

| 标准 | 验证方式 |
|------|----------|
| 函数描述可恢复性：大仓中途失败后重跑，缺失函数描述 100% 可补回 | 集成测试：mock 中途超时 → 再 generate，断言 FailedFuncs 清零 |
| 增量正确性：改单文件，未受影响模块零重算 | 集成测试：改单文件 → 再 generate，断言函数描述阶段仅处理增量 |
| 超时不卡死：单批任意失败路径 ≤ 5 分钟返回 | 单元测试：mock 挂起，断言 5min 内返回 |
| 大仓覆盖率：函数描述覆盖率 ≥ 95% | E2E：project-ss（465 文件）全量 generate |
| 无回归 | `go test -race ./...` 全绿 |

### M3.5 测试计划核对

| 测试项 | 状态 | 实现方式（计划） |
|--------|------|------------------|
| A2 checkpoint 部分恢复 / 过期淘汰 | 🔜 | mock FuncDescMap，断言只对 pending 发请求 |
| A3 增量不清盘 / --force 清盘 | 🔜 | 改单文件后断言 checkpoint 保留 |
| A1 重试成功 / 重试耗尽进 FailedFuncs | 🔜 | mock 前 N 次失败、第 N+1 次成功 |
| A4 降级 ctx deadline 隔离 | 🔜 | 捕获 Complete 的 ctx，断言独立 deadline |
| A5 reasoning 计入活跃 / 普通仍 3min | 🔜 | mock reasoning token 间隔 4min 不超时 |
| A6 流式 429 退避 / -concurrency 生效 | 🔜 | mock 429+Retry-After；并发计数 |

---

## M4-B 交付物细拆（计划中：信息架构升级）

> 详见 `prd.md` § M4-B。实现顺序 B1 → B3 → B2。

| # | 任务 | 实现文件 | 测试文件（计划） | 状态 |
|---|------|----------|------------------|------|
| B1 | 模块文档 LLM 增强（重要度+被引用权重覆盖） | `internal/docgen/docgen.go`（GenerateModuleDocs + 新 selectTopModules） | `docgen_test.go::TestSelectTopModules / TestModuleDocsLLMEnhanced / TestModuleDocsCheckpointResume` | ✅ 已完成 |
| B3 | API 参考按模块分组 | `internal/docgen/docgen.go`（GenerateAPIReferenceMarkdown） | `docgen_test.go::TestAPIReferenceGroupedByModule` | ✅ 已完成 |
| B2 | serve 语义搜索 + 8 项浏览器体验 | `internal/cli/cli.go`（/api/search）+ `internal/docgen/render.go`（前端 JS）+ 新增混合检索 | `cli_test.go::TestSearchSemanticRecall / TestSearchExactSymbolBoost / TestSearchFallback` | ✅ 已完成 |

### M4-B 成功标准核对

| 标准 | 验证方式 |
|------|----------|
| modules/*.md 占位文案仅出现在显式标注的未选中模块 | 检查生成产物 + TestModuleDocsLLMEnhanced |
| API 参考按模块分组、标角色 | TestAPIReferenceGroupedByModule |
| serve 业务词召回代码符号 | TestSearchSemanticRecall |
| 精确符号字面命中排前 | TestSearchExactSymbolBoost |
| 库空降级字面 | TestSearchFallback |
| 8 项前端体验可用 | 人工验收 |
| 无回归 | `go test -race ./...` 全绿 |

---

## 五、M4 交付物细拆（未开始）

| # | PRD 能力 | 状态 |
|---|----------|------|
| 4.1 | VSCode 扩展 | ⏳ 未开始 |
| 4.2 | GitHub Action | ⏳ 未开始 |
| 4.3 | C# / Kotlin AST | ⏳ 未开始 |
| 4.4 | 图结构自然语言查询 | ⏳ 未开始 |
| 4.5 | v1.0.0 正式发布 | ⏳ 未开始 |

---

## 六、发布门禁（Quality Gates）

| 门禁项 | M1 状态 | M2 状态 | M3 目标 | 验证方式 |
|--------|---------|---------|---------|----------|
| E2E 全绿 | ✅ | ✅ | 保持 | `go test ./...` |
| 覆盖率未下降 | ⚠️ | ⚠️ | ✅ | `coverage-baseline.md` 已建立，容差 -2% |
| 安全扫描 (govulncheck) | ⚠️ | ⚠️ | M3 完成 | CI 中已配置 `govulncheck ./...`（continue-on-error）；本地标准库版本漏洞需升级 Go 版本 |
| CLI --help 完整性 | ✅ | ✅ | M3 完成 | 人工检查：已补全 Ask flags |
| 安装验证 (brew/npm/winget) | ❌ | ❌ | 🟡 延后 | GitHub Releases + 一键脚本已就绪；包管理器待 V2 补全 |
| 跨平台编译矩阵 | ✅ | ✅ | M3 完成 | GitHub Actions (ubuntu/macOS/windows × Go 1.26) |

---

## 七、已知结构性 Gap（跨里程碑）

| # | Gap 描述 | 影响范围 | 建议修复时机 | 相关切片 |
|---|----------|----------|--------------|----------|
| ~~G1~~ | ~~tree-sitter grammar 未捆绑，AST 精度依赖正则~~ | ~~M1-M4 分析质量~~ | ~~M3 完成~~ | ~~1.1, 3.11~~ |
| ~~G2~~ | ~~无 Prompt 快照/回归目录 (`llm-responses/`)~~ | ~~M2-M3 LLM 输出稳定性~~ | ~~M3 文档深度阶段~~ | ~~1.2, 3.10~~ |
| ~~G2b~~ | ~~Prompt 快照回归已建立 (`testdata/expected/prompts/`)~~ | ~~M3 Prompt 变更检测~~ | ~~M3 工程基建~~ | ~~3.10~~ |
| ~~G3~~ | ~~无性能基准套件 (`benchmark/bench-*`)~~ | ~~M3 性能优化无度量~~ | ~~M3 性能阶段~~ | ~~3.9, 3.13~~ |
| ~~G4~~ | ~~向量存储无增量索引，每次全量重建~~ | ~~M2 RAG 大项目体验~~ | ~~M3 性能阶段~~ | ~~2.2~~ |
| ~~G5~~ | ~~无 `expected/` 目录存储预期图表 DSL~~ | ~~M1 图表回归测试~~ | ~~M3 工程基建~~ | ~~1.3, 3.13~~ |
| ~~G6~~ | ~~Mermaid 语法无 `mmdc` 自动化校验~~ | ~~M1 图表正确性~~ | ~~M3 工程基建~~ | ~~1.3, 3.13~~ |
| ~~G7~~ | ~~无 CI/CD 跨平台矩阵~~ | ~~M3 发布门禁~~ | ~~M3 已完成~~ | ~~3.13, 发布门禁~~ |

---

## 八、使用指南：如何更新本文档

1. **新增功能**：在对应里程碑表格中新增一行，标注 `状态` 和 `备注`
2. **修复 Gap**：在 Gap 列表中找到对应项，改为 ~~删除线~~ 或移到"已修复"区
3. **变更状态**：里程碑总览中 `🔄 进行中` / `⏳ 未开始` / `✅ 已完成` 随代码同步更新
4. **每次 PR 前自查**：对照本文档检查是否遗漏了 PRD 中定义的测试用例或成功标准

---

## 九、快速索引：文件 → 能力映射

| 文件/目录 | 覆盖的 PRD 能力 |
|-----------|----------------|
| `internal/analyzer/` | M1-1.1 AST, M2-2.7 多语言 |
| `internal/docgen/` | M1-1.2 文档, M3-3.1~3.7 深度增强与导出, M3-3.7b Web UI 体验升级 |
| `internal/docgen/render.go` | M3-3.7b Web UI: `wikiPageCSS`/`wikiPageJS` 共享样式与脚本, `BuildWikiPage()` Serve 渲染, `RenderMarkdownBody()` Markdown→HTML |
| `internal/diagram/` | M1-1.3 架构图, M1-1.4 类图 |
| `internal/grapher/` | M1-1.3 依赖图, M3-3.2 模块推断 |
| `internal/sequencer/` | M2-2.4 时序图, M3-3.3 调用链语义 |
| `internal/chunker/` | M2-2.1 语义分块 |
| `internal/embedder/` | M2-2.2 Embedding |
| `internal/vectorstore/` | M2-2.2 向量存储 |
| `internal/rag/` | M2-2.3 RAG 问答 |
| `internal/llm/` | M2-2.5 本地LLM, M2-2.6 配置系统 |
| `internal/cli/` | M1-1.5 CLI, M1-1.7 Web, M2-2.3 ask, M3-3.7b Web UI (阅读时长/难度徽章/搜索入口) |
| `cmd/codewiki/` | M1-1.5 入口 |
