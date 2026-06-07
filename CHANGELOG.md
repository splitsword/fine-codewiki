# 变更日志

> 本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/) 规范。

---

## [Unreleased]

- 2026-06-07 — fix(docgen): 修复进度条计数器超总、任务残留、printf 破坏显示 (a5e36cc)

- 2026-06-07 — fix(docgen): 函数描述并发度 20→10 降低流式超时 (859a83d)

- 2026-06-07 — fix(docgen): 进度条活跃任务始终显示任务名而非 detail 文本 (caad119)

- 2026-06-07 — fix(docgen): 进度条改为"历史行一次性写入 + 单行原地刷新"消除屏闪 (208dc0c)

- 2026-06-07 — fix(docgen): prompt 中模块/入口/角色均使用含扩展名的全路径 (dd6a9d4)

- 2026-06-07 — fix(lang): 多语言项目探测返回全部主要语言而非单一胜者 (f01b11c)

- 2026-06-07 — feat(docgen): B 方案动画进度条替代流式打点与结构化输出 (15ad72f)

- 2026-06-07 — feat(docgen): 项目概述补充文档探测并抑制标题臆造 (bb08595)

- 2026-06-07 — feat(docgen): 优化函数描述覆盖率分段与批次并发生成 (8a70eb6)

- 2026-05-30 — docs(readme): 追加 codewiki --help CLI 帮助信息 (1b78381)

- 2026-05-30 — feat(pdf): Chrome headless PDF 导出 + export pdf 命令 (b0d2547)

- 2026-05-30 — docs(homepage): 竞品对照文案微调 (d770e2a)

- 2026-05-29 — docs(prd): 同步 M3 里程碑状态为全部已完成 (6032d98)

- 2026-05-29 — docs(prd-coverage): 同步 M3 完成状态与 v1.0 Beta 发布 (891077b)

- 2026-05-29 — docs(changelog): 插入 v1.0-beta 发布章节，清空 Unreleased (986cd18)

---

## [v1.0-beta] — 2026-05-29

### M3 完成与 v1.0 Beta 发布

**主题**：从"结构百科"转向"学习百科"——主题导向文档、设计决策显性化、学习路径引导、导出功能、安装分发。

#### 新增
- **主题导向叙事文档**：生成 5 篇主题文章 —— 项目概述 / 能做什么 / 架构说明 / 核心概念 / 学习路径，按"概念→架构→实现"组织，替代冷冰冰的模块清单
- **架构说明多图叙事化**：功能架构图 + 技术架构图双图穿插叙事，节点标注模块角色（Controller / Service / Repository 等），颜色图例与边类型区分
- **静态 HTML 导出**：完整离线 Wiki 三栏布局（导航 + 内容 + 文件树），支持暗色主题、Ctrl+K 搜索、代码复制、图表全屏、滚动导航高亮
- **流式 AI 问答**：`serve` 模式右侧面板整合搜索与 AI 问答，实时流式输出，来源可点击弹出暗色源码窗口（语法高亮）
- **PDF 导出**：纯 Go 零依赖实现（`signintech/gopdf`），自动检测系统 CJK 字体，含封面、Markdown 渲染、图表附录、模块文档附录
- **产品官网主页**：独立 `homepage/index.html`，含安装指南、三步上手、功能对比、技术栈展示
- **`codewiki update` 自更新**：从 GitHub Releases 自动检测并下载新版本，Windows 下后台自动替换二进制
- **GitHub Releases 自动发布**：`.github/workflows/release.yml` 多平台构建（linux/darwin/windows × amd64/arm64）
- **4 阶段异步并行生成管线**：LLM 调用全并行 + 依赖型并行 + 主题级并行，10 万行代码生成耗时 2–5 分钟
- **流式优先 LLM + 3 级渐进降级**：Thinking/Reasoning 模式自动适配，超时/限流时自动降为非流式并重试
- **图表质量升级**：架构图节点角色标注、颜色图例、循环依赖虚线标注；类图支持多继承与空类；时序图添加场景描述
- **增量缓存机制**：基于文件 `mtime + size` 的增量向量索引与 AST 解析缓存，避免重复计算
- **模块角色推断**：基于 PageRank 自动推断模块在架构中的角色（Controller、Service、Repository 等）
- **RAG 双路检索**：文档 chunks 优先 + 代码 chunks 补充，确保 LLM 上下文既有百科文章又有精确源码
- **性能基准测试套件**：`benchmark/` 目录含 100K 行端到端性能测试与 RAG 问答准确率评测

#### 改进
- **Web UI 全面升级**：磨砂玻璃态设计、阅读进度 / 预估时长 / 难度徽章、来源溯源弹窗、图表全屏查看、模块中文名称智能生成
- **LLM 生成可靠性**：幻觉检测（反引号/加粗标识符真实性校验）、按语言定制 prompt 模板、空函数自动降级描述、batch 语义描述生成
- **CLI 体验优化**：`browse` 命令一键生成并打开浏览器，`config` 交互式向导，`--help` 全面补全
- **源码溯源**：AI 回答每段标注来源文件，点击弹出暗色代码窗口，支持预生成静态 HTML 中的来源弹窗

#### 修复
- RAG 基准测试 mock LLM 适配中文 prompt 标题（`## 问题` / `### 上下文`）
- 来源弹窗弹出失败（正则语法错误导致 JS 脚本整体失效）
- 来源引用 hover 串扰 + 点击加载错误文件
- 核心能力表对应模块列中出现字面量 `<br>`
- 来源标签中 LLM 输出 Markdown 链接导致路径乱码
- 来源弹窗需双击关闭 + 缩写路径 404
- AI 问答来源链接点击无效（`openSource` 作用域与注入目标双重错误）
- Go 分组 import 解析失败导致依赖图 0 条边
- `render.go` 开头的 UTF-8 BOM 字节导致 `go test -cover` 编译失败
- CI Go 版本矩阵与 `go.mod` 不一致（1.23/1.24 → 1.26）

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
