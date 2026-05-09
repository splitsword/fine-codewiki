# 覆盖率基准

> 本文档记录各包的测试覆盖率基准，用于 PR 时检查覆盖率是否下降。
> 生成方式：`go test -cover ./...`
> 最后更新：2026-05-08

## 各包覆盖率

| 包 | 覆盖率 | 备注 |
|---|---|---|
| `internal/analyzer` | 92.2% | |
| `internal/benchmark` | 89.9% | 基准测试辅助代码 |
| `internal/chunker` | 100.0% | |
| `internal/cli` | 75.2% | 含交互式配置向导测试 |
| `internal/diagram` | 90.7% | 含 mmdc 验证测试 |
| `internal/docgen` | 83.6% | 含 PDF/HTML/Markdown 生成测试 |
| `internal/embedder` | 96.6% | |
| `internal/grapher` | 97.3% | |
| `internal/llm` | 82.0% | 含双配置、环境变量测试 |
| `internal/rag` | 92.7% | |
| `internal/sequencer` | 91.5% | |
| `internal/vectorstore` | 87.2% | 含增量索引测试 |

## 发布门禁

- 新增代码覆盖率不得低于对应包当前基准的 **-2%** 容差。
- 核心包（`analyzer`, `chunker`, `grapher`, `embedder`）不得低于 **90%**。
