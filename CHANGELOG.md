# 变更日志

> 本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/) 规范。

---

## [Unreleased]

- 2026-05-08 — docs(PRD_COVERAGE): update M3 status for completed items (6e64421)

- 2026-05-08 — feat(diagram): add semantic annotations to Mermaid DSL output (fe93808)

- 2026-05-08 — feat(benchmark): add performance benchmark suite (1ce3c47)

- 2026-05-08 — feat(docgen): add module role column to architecture markdown (a975a54)

- 2026-05-08 — feat(grapher): add PageRank-based module role inference (f4ff10b)

- 2026-05-08 — update PRD_COVERAGE.md: add M3 success criteria, test plan, and engineering infra items (e246c7c)

- 2026-05-08 — docs(prd): expand M3 with engineering gaps from M1-M2 audit (4e000aa)

- 2026-05-08 — docs: add PRD coverage tracking document (84c9df8)

- 2026-05-08 — chore(docgen): print timeout hint when LLM request exceeds deadline (f197e7d)

- 2026-05-08 — feat(docgen): prioritize README and core modules in LLM prompt (f1730d0)

- 2026-05-08 — fix(docgen): truncate prompt module lists to avoid API timeout (3bd8719)

- 2026-05-08 — fix(docgen): add LLM error logging to diagnose enhancement failures (9939cc0)

- 2026-05-07 — fix(docgen): skip meaningless community groups in auto description (14f817e)

- 2026-05-07 — fix(docgen): restore project description in overview.md (623ab2a)

- 2026-05-07 — docs(prd): update M2 completion and M3 doc-depth enhancement (246b581)

- 2026-05-07 — feat(llm): split config into generation and embedding providers (80d5511)

- 2026-05-07 — feat: localize all wiki output and CLI to Simplified Chinese (4485ff0)

- 2026-05-07 — docs: update changelog with sequencer fix (d9379fc)

- 2026-05-07 — fix(sequencer): resolve empty sequence diagram due to class scope reset, regex cross-contamination, and self-loop source detection (98177ea)

- 2026-05-07 — chore: add post-commit hook to auto-update docs (02ab102)

### 修复
- **时序图空输出修复**：修复 `sequencer` 模块中导致时序图为空的三个交互 bug
  - Python 空行错误重置类作用域，导致方法名丢失类前缀
  - 跨语言正则污染（Java 正则匹配 Python 代码）产生虚假函数定义
  - 自循环边（如 `main()` 模块级调用）破坏入度计算，导致无法找到起始节点
  - 新增 `python-basic` 集成测试，确保端到端序列图生成正常

### 文档
- 新增 README.md 与 CHANGELOG.md

---

## [M2] — 2026-05-07

### 问答与图表增强

#### 新增
- **RAG 检索增强问答**：实现 `codewiki ask` 终端问答命令，支持基于代码向量索引的自然语言查询
- **源码溯源**：所有回答附带源文件路径与起始行号，实现答案到代码的精确追溯
- **多轮对话会话**：新增 `Session` / `Turn` 机制，支持连续追问与上下文保持
- **本地 LLM 适配**：完整支持 Ollama 本地模型运行全部生成流程，实现 100% 离线运行
- **时序图生成**：基于调用链分析生成 Mermaid 时序图，展示关键交互流程
- **依赖图生成**：新增独立依赖图（`graph LR`），展示模块间 import 关系的全貌
- **SQLite 向量存储**：使用 `modernc.org/sqlite`（纯 Go，无 CGO）实现本地向量持久化
- **增量索引机制**：基于文件 `mtime + size` 的增量索引，仅对变更文件重新 Embedding
- **向量存储裁剪**：`PruneFiles` 自动清理已删除文件的向量记录
- **社区检测**：基于确定性标签传播的图社区检测，用于架构图分层
- **Mermaid DSL 校验器**：结构级语法验证（括号平衡、边语法、子图嵌套等）
- **Tree-sitter 包装器**：`TreeSitterParser` 封装 `gotreesitter`，为纯 Go 语法解析预留扩展点
- **Go / Java AST 支持**：分析引擎新增 Go 与 Java 语言解析
- **TypeScript 增强**：支持接口、枚举、泛型的解析与类图生成
- **Benchmark 评测集**：`benchmark/qa_bench.json` 含 30+ 问答对，覆盖架构/API/调用链/设计模式
- **评测框架**：自动化评估答案内容匹配、引用来源检查、准确率与引用率统计

#### 改进
- **架构图稳定性**：节点与边排序，确保多次生成输出完全一致
- **类图完善**：支持多继承、空类、self/cls 参数过滤
- **循环依赖标注**：架构图与依赖图中循环依赖边使用虚线（`-.->`）标注
- **交互式问答终端**：`serve` 内置问答界面，可在浏览器中直接提问

#### 测试
- 新增 `internal/vectorstore` 测试套件，覆盖率 87.2%
- 新增 `internal/diagram/validate` 测试套件，覆盖率 90.5%
- 新增 `internal/grapher` 社区检测测试，覆盖率 98.1%
- 新增 `internal/rag` 多轮会话测试，覆盖率 92.7%
- 新增 `internal/benchmark` 评测框架测试，覆盖率 89.9%
- 新增 `internal/analyzer/treesitter` 回退测试
- 所有模块行覆盖率均达到或超过 PRD 目标（≥ 85%）

---

## [M1] — 2026-05-06

### 核心可行原型

#### 新增
- **CLI 框架**：`codewiki generate`、`serve`、`ask`、`config` 四大命令
- **AST 解析引擎**：基于正则与 tree-sitter 回退的解析器，支持 Python / JavaScript / TypeScript
- **依赖图构建**：分析 import 关系，构建全局模块依赖图，支持循环依赖检测
- **架构图生成**：基于依赖图自动生成 Mermaid `graph TD` 架构图，按目录分 subgraph
- **类图生成**：从 AST 提取类结构生成 Mermaid `classDiagram`，含方法与继承关系
- **文档生成引擎**：项目概览、架构文档、API 参考（函数/类签名、参数、返回值）
- **LLM 适配层**：统一接口适配 OpenAI 兼容 API 与 Ollama 本地模型
- **本地 Web 预览**：`serve` 命令启动 HTTP 服务，内嵌 Mermaid.js 渲染图表
- **代码向量化**：语义分块 + Embedding + 本地向量索引（JSON / SQLite 双后端）
- **Wiki 输出**：Markdown + Mermaid 文件写入 `.codewiki/wiki/`

#### 测试
- 完整的单元测试与集成测试覆盖
- AST 解析、图谱构建、图表生成、CLI 命令全链路 TDD 验证
