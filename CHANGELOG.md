# 变更日志

> 本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/) 规范。

---

## [Unreleased]

- 2026-05-17 — @ feat(docgen): 架构说明多图叙事化 + KeyConcepts 合并入架构叙事 (9c3074a)

- 2026-05-17 — @ docs(prd): v0.10 架构说明多图叙事化 + KeyConcepts 并入设计 (5967fea)

- 2026-05-17 — @ fix(docgen): 修复项目结构标题缺失 + WhatItDoes 标题重复 (17aa08e)

- 2026-05-17 — @ chore(docgen): 移除 Wiki.ProjectStructureNarrative 死字段 (dc95b4e)

- 2026-05-17 — @ feat(docgen, cli): 项目结构页 LLM 叙事化重构 + 来源弹窗 (93a5501)

- 2026-05-17 — @ feat(prd): 项目结构页 LLM 叙事化重构设计文档 (e41c115)

- 2026-05-16 — feat(diagram, rag): 图表质量全面升级 + RAG 检索增强 (88eb127)

- 2026-05-15 — feat(docgen, rag, chunker): RAG 问答系统改造 + 生成管线异步并行化 (57b9d5e)

- 2026-05-15 — feat(docgen): 架构说明叙事化重构 — 对标 Zread 从数据展示转向叙事构建 (468f757)

- 2026-05-15 — ﻿feat(llm, docgen): LLM 生成可靠性工程 — P0/P1 三合一修复 + 4项 Bug 修复 (4bbc4e5)

- 2026-05-15 — @ feat(llm, docgen): LLM 生成可靠性工程 — P0/P1 三合一修复 + 4项 Bug 修复 (3fc72ce)

- 2026-05-12 — feat(docgen, cli): Web UI 全面升级 — 14项交互特性 + Zread 体验对标 (27cac31)

- 2026-05-12 — @ feat(docgen, cli): Web UI 全面升级 — 14项交互特性 + Zread 体验对标 (4183ef0)

- 2026-05-10 — feat(docgen): 图表跟随主题文章内嵌，移除独立 .mmd 文件 (241bb2f)

- 2026-05-10 — fix(cache): 深拷贝 FileResult 防止缓存腐败，升级缓存版本 (faf5dbb)

- 2026-05-10 — feat(sequencer): 诊断输出增加 sourceDir、path 和 resolved 路径 (8bd29b5)

- 2026-05-10 — fix(cli): 缓存中的相对路径 Filename 导致 BuildCallGraph 读取文件失败 (10a853f)

- 2026-05-10 — feat(sequencer): 为 BuildCallGraph 添加逐文件诊断日志 (f3f3da0)

- 2026-05-10 — feat(cli, sequencer): --force 强制重新解析 AST，添加调用链诊断日志 (4d75120)

- 2026-05-10 — 修复 BuildCallGraph 返回 0 调用的问题 (476b1a5)

- 2026-05-10 — feat(docgen, cli): 完成第三批 Zread 体验对齐 (8297287)

- 2026-05-10 — feat(cli, docgen, diagram): 完成第一批与第二批 PRD 差距修复 (6c1040c)

- 2026-05-08 — feat(analyzer): tree-sitter grammar 捆绑（`gotreesitter/grammars`），tags query 提取 class/function，AST walk 提取 import，regex 兜底回退
- 2026-05-08 — feat(docgen): LLM 幻觉检测，检查反引号/加粗标识符是否真实存在于代码库，阈值 ≥2 处或 >30% 时回退到静态描述
- 2026-05-08 — feat(llm): Ollama 连接拒绝时返回友好提示，引导用户检查服务状态
- 2026-05-08 — feat(llm): OpenAI JSON 解析失败时返回包含原始响应的错误信息
- 2026-05-08 — feat(analyzer): Rust / C++ AST 正则解析支持（struct、trait、impl、class、#include、方法等）
- 2026-05-08 — fix(docgen): 修复 `transform_` 前缀动词推断时截断错误（`name[11:]` 修正为 `name[10:]`）
- 2026-05-08 — test(docgen): 新增 `languagePromptHint`、`selectTopFunctions`、`buildFunctionDescriptionPrompt`、`parseFunctionDescriptions` 单元测试
- 2026-05-08 — test(docgen): 补充 35+ 动词模式覆盖（`__str__`、`register`、`logout`、`encode_` 等），覆盖率从 73.6% 提升至 83.6%
- 2026-05-08 — docs(PRD_COVERAGE): 修复 Prompt 快照回归、mmdc 语法校验、增量索引、覆盖率基准等状态不一致
- 2026-05-08 — feat(docgen): 按语言定制 LLM prompt 模板（Python/Go/JS/Java/Rust/C++）
- 2026-05-08 — feat(docgen): LLM batch prompt 为前 5 个关键函数生成语义描述并注入 API Reference
- 2026-05-08 — feat(docgen): 空函数/抽象函数自动降级描述（"占位函数" / "抽象方法"）
- 2026-05-08 — feat(diagram): 为依赖图添加基础静态 `%%` 语义注释
- 2026-05-08 — fix(docgen): architecture LLM 增强添加 `isChecklistLike` 检测，避免清单化输出
- 2026-05-08 — docs: 建立覆盖率基准文件 `coverage-baseline.md`
- 2026-05-08 — docs(cli): 补全 `--help` 中缺失的 Ask flags
- 2026-05-08 — feat(docgen): 实现 Wiki Markdown 合辑导出 (f31cf8b)

- 2026-05-08 — docs(PRD_COVERAGE): 更新 M3-3.1 和 M3-3.3 静态层完成状态 (86639cd)

- 2026-05-08 — feat(sequencer): 为时序图添加调用链路场景描述 (d3d370f)

- 2026-05-08 — feat(docgen): 添加函数级静态语义摘要 (ef1c09a)

- 2026-05-08 — docs: 将 CHANGELOG.md 提交历史描述翻译为简体中文 (f057ea5)

- 2026-05-08 — docs(PRD_COVERAGE): 更新 M3 已完成项状态 (6e64421)

- 2026-05-08 — feat(diagram): 为 Mermaid DSL 输出添加语义注释 (fe93808)

- 2026-05-08 — feat(benchmark): 添加性能基准测试套件 (1ce3c47)

- 2026-05-08 — feat(docgen): 在架构 Markdown 中添加模块角色列 (a975a54)

- 2026-05-08 — feat(grapher): 基于 PageRank 实现模块角色推断 (f4ff10b)

- 2026-05-08 — 更新 PRD_COVERAGE.md：添加 M3 成功标准、测试计划和工程基建项 (e246c7c)

- 2026-05-08 — docs(prd): 根据 M1-M2 审计结果扩展 M3 工程基建内容 (4e000aa)

- 2026-05-08 — docs: 添加 PRD 覆盖追踪文档 (84c9df8)

- 2026-05-08 — chore(docgen): LLM 请求超时时打印超时提示 (f197e7d)

- 2026-05-08 — feat(docgen): 在 LLM 提示词中优先注入 README 和核心模块 (f1730d0)

- 2026-05-08 — fix(docgen): 截断提示词模块列表以避免 API 超时 (3bd8719)

- 2026-05-08 — fix(docgen): 添加 LLM 错误日志以诊断增强失败 (9939cc0)

- 2026-05-07 — fix(docgen): 在自动描述中跳过无意义的社区分组 (14f817e)

- 2026-05-07 — fix(docgen): 恢复 overview.md 中的项目描述 (623ab2a)

- 2026-05-07 — docs(prd): 更新 M2 完成状态及 M3 文档深度增强 (246b581)

- 2026-05-07 — feat(llm): 将配置拆分为生成模型和向量模型 (80d5511)

- 2026-05-07 — feat: 将所有 Wiki 输出和 CLI 本地化为简体中文 (4485ff0)

- 2026-05-07 — docs: 更新变更日志，补充时序图修复 (d9379fc)

- 2026-05-07 — fix(sequencer): 修复类作用域重置、正则交叉污染和自环源检测导致的时序图为空问题 (98177ea)

- 2026-05-07 — chore: 添加 post-commit 钩子自动更新文档 (02ab102)

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
