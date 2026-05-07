# PRD 覆盖追踪文档

> 本文档将 PRD 中的能力、测试、里程碑、发布门禁映射到实际实现状态。每次提交前更新对应条目，确保无遗漏。
> 文档版本：v0.2 | 最后更新：2026-05-08

---

## 一、里程碑总览

| 里程碑 | 状态 | 核心交付 | 阻塞项 |
|--------|------|----------|--------|
| M1 — 核心可行原型 | ✅ 已完成 | AST/文档/图表/CLI/Web | 无 |
| M2 — 问答与图表增强 | ✅ 已完成 | RAG/时序图/本地LLM/配置/中文 | 无 |
| M3 — 产品化打磨 | 🔄 进行中 | 见下方详细拆解 | LLM超时/大项目prompt优化已修 |
| M4 — 生态扩展 | ⏳ 未开始 | IDE/CI/图查询/1.0发布 | 无 |

---

## 二、M1 交付物细拆

| # | PRD 能力 | 实现文件 | 测试文件 | 状态 | 备注 |
|---|----------|----------|----------|------|------|
| 1.1 | AST 解析引擎（Python/JS/TS/Go/Java） | `internal/analyzer/` | `analyzer/*_test.go` (31 tests) | ✅ | 正则回退方案；tree-sitter grammar 未捆绑 |
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
| 单二进制跨平台 | ✅ | `go build` 产出单文件；CI 未配置跨平台矩阵 |
| 全程本地运行 | ✅ | 代码零上传，Ollama 本地模型支持 |

### M1 测试计划核对

| 测试项 | 状态 | 实现方式 |
|--------|------|----------|
| AST：降级处理语法错误 | ✅ | `analyzer` 测试中覆盖 |
| AST：空文件/嵌套/import | ✅ | `analyzer` 测试中覆盖 |
| AST：并发解析无数据竞争 | ✅ | 目录遍历 + 并发解析测试 |
| Docgen：空仓库/单文件/多模块 | ✅ | `docgen` 测试中覆盖 |
| Docgen：Prompt 快照对比 | ❌ | `llm-responses/` 目录不存在；当前用 mock provider 代替 |
| Docgen：LLM 无效 JSON 降级 | ⚠️ | Ollama 端有 JSON 解析错误处理；OpenAI 端返回原始错误 |
| Diagram：循环依赖/空类/大图 | ✅ | `diagram` + `grapher` 测试中覆盖 |
| Diagram：Mermaid 语法校验 | ⚠️ | 无 `mmdc` 集成测试；仅通过 render 间接验证 |
| Diagram：DSL 内容稳定性 | ✅ | 确定性生成测试 |
| CLI：E2E generate 全流程 | ✅ | `TestGenerateCommand` |
| CLI：serve HTTP 可访问 | ✅ | `TestRunServeStarts` |
| CLI：跨平台编译 | ⚠️ | 手动验证，无 CI 矩阵 |

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
| 问答准确率 > 80% | ⚠️ | `benchmark/qa_bench.json` 框架存在，但未运行过完整评测 |
| 非代码描述全中文 | ✅ | 人工验证通过 |

### M2 测试计划核对

| 测试项 | 状态 | 实现方式 |
|--------|------|----------|
| 向量化：函数边界不被切断 | ✅ | `chunker` 测试 |
| 向量化：Top-3 检索准确 | ✅ | `vectorstore` 相似度测试 |
| 向量化：大规模索引 < 500ms | ⚠️ | 无性能基准；仅单元测试 |
| 向量化：增量索引 | ❌ | 未实现增量更新，每次全量重建 |
| RAG：无关问题礼貌拒答 | ✅ | `rag` 测试中覆盖空检索场景 |
| RAG：多轮追问上下文保持 | ✅ | `Engine` 含 `History` 字段 |
| 时序图：单层/多层/递归/异步 | ✅ | `sequencer` 测试覆盖 |
| LLM：API 超时重试 | ✅ | `llm` mock 测试覆盖 |
| LLM：429 retry-after | ✅ | `llm` mock 测试覆盖 |
| LLM：Ollama 未启动提示 | ⚠️ | 返回网络错误，未做特定端点健康检查 |

---

## 四、M3 交付物细拆（当前进行中）

| # | PRD 能力 | 实现文件 | 状态 | Gap / 下一步 |
|---|----------|----------|------|--------------|
| 3.1 | 函数级逻辑分析 | `internal/docgen/docgen.go` (静态) | ⚠️ | `buildAutoDescription` 有基础规模/核心模块/入口点描述；缺少 LLM 驱动的函数语义摘要 |
| 3.2 | 模块职责推断 | `internal/docgen/docgen.go` | ⚠️ | `buildAutoDescription` 识别核心模块；缺少 PageRank 角色推断 |
| 3.3 | 调用链路语义描述 | `internal/docgen/docgen.go` | ⚠️ | `buildAutoDescription` 检测循环依赖；缺少时序图场景文字生成 |
| 3.4 | 图表语义标注 | 无 | ❌ | Mermaid DSL 中无 `%%` 场景注释；无图表→语义描述生成 |
| 3.5 | Wiki 导出 Markdown 合辑 | 无 | ❌ | 未实现多文件合并导出 |
| 3.6 | 导出静态 HTML | `internal/cli/cli.go` (serve) | ⚠️ | `serve` 实时渲染 HTML，但无离线 HTML 导出功能 |
| 3.7 | 导出 PDF | 无 | ❌ | 未实现 |
| 3.8 | Homebrew/npm/winget 分发 | 无 | ❌ | 未实现 |
| 3.9 | 大规模仓库性能优化 | 无 | ❌ | 无缓存策略、无并发控制调优；增量索引未实现 (G4) |
| 3.10 | 提示词优化（按语言）+ Prompt 快照回归 | `internal/docgen/docgen.go` | ⚠️ | prompt 已统一中文；无按语言定制模板；无快照回归机制 (G2) |
| 3.11 | Rust / C++ AST 支持 + tree-sitter grammar 捆绑 | 无 | ❌ | 未实现；grammar 未捆绑 (G1) |
| 3.12 | Beta 公开发布 | 无 | ❌ | 文档/指南不完整 |
| 3.13 | 工程基建：性能基准套件 + 预期 DSL 快照 + mmdc 语法校验 + CI 跨平台矩阵 | 无 | ❌ | 未开始 (G3, G5, G6, G7) |

### M3 核心痛点回应追踪

| 痛点 | PRD 方案 | 当前状态 | 负责切片 |
|------|----------|----------|----------|
| 函数级语义摘要 | AST 控制流 + LLM | ⚠️ 静态描述有，LLM 增强受 timeout/prompt 限制 | #3.1 |
| 模块角色推断 | 依赖图 + PageRank | ⚠️ 核心模块识别有，无 PageRank 角色标签 | #3.2 |
| 图表语义层 | Mermaid `%%` 注释 | ❌ 未实现 | #3.4 |
| 调用链语义描述 | 时序图场景文字 | ❌ 未实现 | #3.3 |

### M3 成功标准核对

| 标准 | 状态 | 验证方式 |
|------|------|----------|
| 每个函数附带职责说明而非仅签名 | ❌ | 未实现函数级语义摘要 |
| 每个模块附带设计意图而非仅文件清单 | ⚠️ | `buildAutoDescription` 有基础描述，缺 PageRank 角色标签 |
| 图表附带语义标注（`%%` 注释） | ❌ | 未实现 |
| 时序图附带场景描述文字 | ❌ | 未实现 |
| 导出 HTML 含可交互 Mermaid 图表 | ⚠️ | `serve` 可实时渲染，缺离线导出 |
| 10 万行仓库 < 3 分钟生成 | ❌ | 无性能基准套件 (G3) |
| 向量存储支持增量索引 | ❌ | 每次全量重建 (G4) |
| Prompt 变更可被回归测试发现 | ❌ | 无快照机制 (G2) |
| 发布到 Homebrew/npm/winget | ❌ | 未实现 |
| 发布后第一个月 100+ star | ⏳ | Beta 未发布 |

### M3 测试计划核对

| 测试项 | 状态 | 实现方式 |
|--------|------|----------|
| 函数级逻辑描述 | ❌ | 未实现 |
| 模块职责推断 | ❌ | 未实现 |
| 调用链路语义描述 | ❌ | 未实现 |
| 空函数/抽象函数降级 | ❌ | 未实现 |
| LLM 幻觉检测 | ❌ | 未实现 |
| 架构图/类图语义标注 | ❌ | 未实现 |
| 图表"清单化"检测 | ❌ | 未实现 |
| HTML 导出含 Mermaid 图表 | ❌ | 未实现 |
| PDF 导出含中文字符 | ❌ | 未实现 |
| 基准测试套件 (bench-10k/50k/100k) | ❌ | 未实现 (G3) |
| 增量索引：新增文件 | ❌ | 未实现 (G4) |
| 增量索引：删除文件清理 | ❌ | 未实现 (G4) |
| Prompt 快照回归 | ❌ | 未实现 (G2) |
| 图表 DSL 预期快照 | ❌ | 未实现 (G5) |
| Mermaid 语法校验 (mmdc) | ❌ | 未实现 (G6) |
| tree-sitter grammar 捆绑验证 | ❌ | 未实现 (G1) |
| CI 跨平台矩阵 | ❌ | 未实现 (G7) |

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
| 覆盖率未下降 | ⚠️ | ⚠️ | 需建立基准 | `go test -cover` |
| 安全扫描 (govulncheck) | ❌ | ❌ | M3 完成 | `govulncheck ./...` |
| CLI --help 完整性 | ⚠️ | ⚠️ | M3 完成 | 人工检查 |
| 安装验证 (brew/npm/winget) | ❌ | ❌ | M3 完成 | 手动安装测试 |
| 跨平台编译矩阵 | ❌ | ❌ | M3 完成 | GitHub Actions |

---

## 七、已知结构性 Gap（跨里程碑）

| # | Gap 描述 | 影响范围 | 建议修复时机 | 相关切片 |
|---|----------|----------|--------------|----------|
| G1 | tree-sitter grammar 未捆绑，AST 精度依赖正则 | M1-M4 分析质量 | M3 或延后 | 1.1, 3.11 |
| G2 | 无 Prompt 快照/回归目录 (`llm-responses/`) | M2-M3 LLM 输出稳定性 | M3 文档深度阶段 | 1.2, 3.10 |
| G3 | 无性能基准套件 (`benchmark/bench-*`) | M3 性能优化无度量 | M3 性能阶段 | 3.9, 3.13 |
| G4 | 向量存储无增量索引，每次全量重建 | M2 RAG 大项目体验 | M3 性能阶段 | 2.2 |
| G5 | 无 `expected/` 目录存储预期图表 DSL | M1 图表回归测试 | M3 工程基建 | 1.3, 3.13 |
| G6 | Mermaid 语法无 `mmdc` 自动化校验 | M1 图表正确性 | M3 工程基建 | 1.3, 3.13 |
| G7 | 无 CI/CD 跨平台矩阵 | M3 发布门禁 | M3 发布前 | 3.13, 发布门禁 |

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
| `internal/docgen/` | M1-1.2 文档, M3-3.1~3.4 深度增强 |
| `internal/diagram/` | M1-1.3 架构图, M1-1.4 类图 |
| `internal/grapher/` | M1-1.3 依赖图, M3-3.2 模块推断 |
| `internal/sequencer/` | M2-2.4 时序图, M3-3.3 调用链语义 |
| `internal/chunker/` | M2-2.1 语义分块 |
| `internal/embedder/` | M2-2.2 Embedding |
| `internal/vectorstore/` | M2-2.2 向量存储 |
| `internal/rag/` | M2-2.3 RAG 问答 |
| `internal/llm/` | M2-2.5 本地LLM, M2-2.6 配置系统 |
| `internal/cli/` | M1-1.5 CLI, M1-1.7 Web, M2-2.3 ask |
| `cmd/codewiki/` | M1-1.5 入口 |
