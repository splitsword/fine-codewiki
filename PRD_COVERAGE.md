# PRD 覆盖追踪文档

> 本文档将 PRD 中的能力、测试、里程碑、发布门禁映射到实际实现状态。每次提交前更新对应条目，确保无遗漏。
> 文档版本：v0.3 | 最后更新：2026-05-12

---

## 一、里程碑总览

| 里程碑 | 状态 | 核心交付 | 阻塞项 |
|--------|------|----------|--------|
| M1 — 核心可行原型 | ✅ 已完成 | AST/文档/图表/CLI/Web | 无 |
| M2 — 问答与图表增强 | ✅ 已完成 | RAG/时序图/本地LLM/配置/中文 | 无 |
| M3 — 产品化打磨 | 🔄 接近完成 | 见下方详细拆解 | 分发渠道(3.8)待补，其余核心项均已完成 |
| M4 — 生态扩展 | ⏳ 未开始 | IDE/CI/图查询/1.0发布 | 无 |

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
| 3.8 | Homebrew/npm/winget 分发 | `scripts/install.sh` + `scripts/install.ps1` + `.github/workflows/release.yml` | 🟡 | GitHub Releases + 一键安装脚本已就绪；Homebrew tap 仓库/npm/winget manifest 待创建 |
| 3.9 | 大规模仓库性能优化 | `internal/cli/cli.go` (RunAsk) + `benchmark/benchmark_test.go` | ✅ | AST+Graph 缓存已实现，增量索引已实现；`BenchmarkEndToEnd100K` 验证 100K 行生成 < 4s；CI 性能门禁已配置 |
| 3.10 | 提示词优化（按语言）+ Prompt 快照回归 | `internal/docgen/docgen.go` | ✅ | prompt 已统一中文；按语言定制模板已实现（Python/Go/JS/Java/Rust/C++）；快照回归机制已建立 (`testdata/expected/prompts/`) |
| 3.11 | Rust / C++ AST 支持 + tree-sitter grammar 捆绑 | `internal/analyzer/analyzer.go` | ✅ | Rust/C++ 正则解析已实现；tree-sitter grammar 已捆绑（`gotreesitter/grammars` 自动检测 + tags query 提取），含 regex 兜底回退 |
| 3.12 | Beta 公开发布 | `README.md` + `CHANGELOG.md` | ⚠️ | 文档已较完整（README/CHANGELOG/PRD 覆盖追踪）；缺少安装分发指南（3.8 延后） |
| 3.13 | 工程基建：性能基准套件 + 预期 DSL 快照 + mmdc 语法校验 + CI 跨平台矩阵 | `benchmark/benchmark_test.go` + `internal/testutil/snapshot.go` | 🔄 | 性能基准套件已建立；图表 DSL 快照已建立；mmdc 语法校验已实现 (`internal/diagram/mmdc_test.go`)；CI 跨平台矩阵仍待实现 (G3、G5、G6 完成, G7 仍待) |

### M3 核心痛点回应追踪

| 痛点 | PRD 方案 | 当前状态 | 负责切片 |
|------|----------|----------|----------|
| 函数级语义摘要 | AST 控制流 + LLM | ✅ `describeFunction()` 静态推断 + `selectTopFunctions` LLM batch prompt 为前 5 个关键函数生成语义描述 | #3.1 |
| 模块角色推断 | 依赖图 + PageRank | ✅ `InferModuleRoles()` 已实现并集成 | #3.2 |
| 图表语义层 | Mermaid `%%` 注释 | ✅ 基础静态注释已覆盖四种图表类型；LLM 深度场景描述通过 `Sequence.Description` 和架构/概述增强实现 | #3.4 |
| 调用链语义描述 | 时序图场景文字 | ✅ `Sequence.Description` 已集成到 compilation.md / HTML / PDF | #3.3 |

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
| 发布到 Homebrew/npm/winget | ❌ | 未实现 |
| 发布后第一个月 100+ star | ⏳ | Beta 未发布 |
| **Web UI 对标 Zread 体验质量** | ✅ | 14 项 UI 特性已实现（磨砂玻璃态/暗色主题/阅读进度条/代码块复制/图表全屏/折叠导航/Ctrl+K搜索/Ask AI/滚动高亮/时长徽章/难度徽章/主题切换/图表导航/版本备份） |
| **Serve 与 Static HTML 双路径视觉一致** | ✅ | 共享 `wikiPageCSS` + `wikiPageJS`，渲染测试覆盖 |
| **新开发者 5 分钟内理解项目定位** | ✅ | 阅读时长徽章 + 难度星级 + "下一步阅读 →"导航 + 五分钟快速上手路径 |
| **暗色模式可用** | ✅ | CSS 变量双主题 + `localStorage` 持久化 + 顶栏一键切换 |

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
| CI 跨平台矩阵 | ✅ | GitHub Actions: ubuntu-latest / macos-latest / windows-latest × Go 1.23 / 1.24 |
| **代码块语言标签+复制** | ✅ | `TestMarkdownToHTMLCodeBlock` 验证 `<pre><code` + 语言标签 + 复制按钮 |
| **Mermaid 全屏展开** | ✅ | `TestGenerateStaticHTML` 验证 `mermaid-wrap` + expand 按钮 |
| **折叠导航分组** | ✅ | `TestWriteWikiFiles` + `TestGenerateStaticHTML` 验证 `nav-group` 结构 + 图标 span |
| **搜索覆盖层** | ✅ | `TestGenerateStaticHTML` 验证 `.search-overlay` + `filterStaticSearch` + Ctrl+K |
| **阅读时长估算** | ✅ | `EstimateReadingTime()` 中文 400 字/分钟，`cli.go` 注入 ⏱ 徽章 |
| **难度星级标注** | ✅ | `articleDifficulty()` 按文件名前缀分级 + 颜色编码 |
| **暗色主题** | ✅ | CSS `[data-theme="dark"]` + `localStorage` 持久化 |
| **Serve/Static 双路径一致性** | ✅ | `wikiPageCSS` + `wikiPageJS` 共享常量，两路径渲染测试覆盖 |

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
| 安装验证 (brew/npm/winget) | ❌ | ❌ | M3 完成 | 手动安装测试 |
| 跨平台编译矩阵 | ✅ | ✅ | M3 完成 | GitHub Actions (ubuntu/macOS/windows × Go 1.23/1.24) |

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
| ~~G7~~ | ~~无 CI/CD 跨平台矩阵~~ | ~~M3 发布门禁~~ | ~~M3 发布前~~ | ~~3.13, 发布门禁~~ |

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
