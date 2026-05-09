# CodeWiki M3 成果人工验证手册

> 按以下步骤执行，逐项验证 M3 交付物是否正常工作。
> 环境要求：Go 1.22+，可选 Ollama（本地模型验证）。

---

## 前置准备

```bash
# 1. 编译 CLI
go build -o codewiki.exe ./cmd/codewiki

# 2. 准备测试仓库（已内置在 testdata/）
ls testdata/repos/python-basic/
ls testdata/repos/python-complex/
```

---

## 一、AST 解析与 tree-sitter grammar 捆绑 (G1)

### 1.1 单元测试验证
```bash
go test -v ./internal/analyzer -run "TestParseDirectory|TestParsePythonComplexRepo"
```
**预期结果**：全部 PASS。tree-sitter 提取到 class + methods，regex 作为兜底回退。

### 1.2 交互式验证 tree-sitter 提取
```bash
# 使用 generate 命令实际跑一遍解析
./codewiki.exe generate testdata/repos/python-basic -o testdata/out/verify-ast
```
**人工检查点**：
- 打开 `testdata/out/verify-ast/api_reference.md`
- 确认 `User` 类下包含 `create`、`authenticate`、`deactivate` 三个方法
- 确认 `Imports` 章节列出了 `BaseModel`、`hash_password` 等 import

### 1.3 验证 tree-sitter 对多语言的支持
```bash
# 生成 Go 项目的 Wiki（如有 Go 测试仓库）
./codewiki.exe generate . -o testdata/out/verify-go
```
**人工检查点**：
- `api_reference.md` 中应包含 Go 的 struct、interface、function
- classDiagram 中应展示 Go 类型的继承/实现关系

---

## 二、LLM 幻觉检测

### 2.1 单元测试验证
```bash
go test -v ./internal/docgen -run "TestDetectHallucination|TestExtractQuotedIdentifiers|TestCollectRealIdentifiers"
```
**预期结果**：全部 PASS。

### 2.2 端到端验证（需要配置 LLM）
```bash
# 1. 配置 LLM（如使用 OpenAI）
set CODEWIKI_API_KEY=sk-your-key

# 2. 生成一个你熟知代码结构的项目
./codewiki.exe generate testdata/repos/python-complex -o testdata/out/verify-hallucination

# 3. 查看 overview.md 和 architecture.md
```
**人工检查点**：
- 搜索文档中反引号 `` ` `` 或加粗 `**` 包裹的标识符
- 对比 `api_reference.md`，确认这些标识符真实存在
- 若触发幻觉阈值（≥2 处或 >30%），终端应打印：
  ```
  警告：LLM 输出检测到 X 处幻觉（...），已回退到静态描述
  ```
- 回退后的描述应仍可读，且不含虚假类/函数名

### 2.3 故意构造幻觉场景（可选）
若你想主动触发幻觉检测：
1. 在 `internal/docgen/docgen.go` 中临时修改 prompt，要求 LLM "编造一个不存在的类名"
2. 运行 generate，观察终端是否报告幻觉并回退

---

## 三、文档生成深度增强 (M3-3.1 ~ 3.7)

### 3.1 函数级语义摘要 (3.1)
```bash
./codewiki.exe generate testdata/repos/python-complex -o testdata/out/verify-docgen
```
**人工检查点**（打开 `api_reference.md`）：
- 前 5 个关键函数（如 `User.__init__`、`Order.validate` 等）应附带 LLM 生成的语义描述
- 描述不是简单的签名重复，而是解释了"这个函数做什么"
- 空函数/抽象函数应降级为"占位函数"或"抽象方法"

### 3.2 模块职责推断 (3.2)
**人工检查点**（打开 `overview.md`）：
- 表格或列表中应包含模块角色标签，如：
  - `models/user.py` → "核心领域"
  - `utils/crypto.py` → "工具库"
  - `main.py` → "入口层"
- 不是简单的文件清单，而是有设计意图总结

### 3.3 调用链路语义描述 (3.3)
**人工检查点**（打开 `compilation.md` 或 `index.html`）：
- 时序图下方应有 `SequenceDescription` 文字段落
- 描述应概括调用链的场景，如"用户创建订单流程"

### 3.4 图表语义标注 (3.4)
**人工检查点**：
- `architecture.md` 中的 Mermaid 代码块顶部有 `%%` 注释，如 `%% 系统架构图：展示核心模块依赖关系`
- `classDiagram.md`、`dependency.md`、`sequence.md` 同理

### 3.5 Markdown 合辑 (3.5)
```bash
ls testdata/out/verify-docgen/
```
**预期文件**：除分散的 `.md` 外，还应有 `compilation.md`（单文件合辑）。

### 3.6 静态 HTML (3.6)
**人工检查点**：
- 目录中存在 `index.html`
- 用浏览器打开，确认：导航栏正常、Mermaid 图表渲染正常、中文无乱码

### 3.7 PDF 导出 (3.7)
**人工检查点**：
- 目录中存在 `wiki.pdf`
- 用 PDF 阅读器打开，确认：中文字符正常显示、标题/代码块/表格排版正确、图表以 DSL 代码块附录形式存在

---

## 四、RAG 问答与增量索引 (M2-M3)

### 4.1 基础问答验证
```bash
# 先索引
./codewiki.exe ask testdata/repos/python-basic "User 类有哪些方法？"
```
**人工检查点**：
- 回答应列出 `create`、`authenticate`、`deactivate`
- 回答底部应附带源文件引用，如 `来源: models/user.py:11`

### 4.2 增量索引验证
```bash
# 1. 首次问答（会建立索引）
./codewiki.exe ask testdata/repos/python-basic "test"

# 2. 新增一个文件到仓库
echo "class AdminUser(User): pass" >> testdata/repos/python-basic/models/admin.py

# 3. 再次问答，观察索引更新速度（应只解析新增文件）
./codewiki.exe ask testdata/repos/python-basic "AdminUser"
```
**人工检查点**：第二次响应应比首次快（无全量重建），且能回答新增内容。

### 4.3 删除文件清理验证
```bash
# 删除刚才新增的文件
rm testdata/repos/python-basic/models/admin.py

# 再次问答，观察是否不再引用已删除文件
./codewiki.exe ask testdata/repos/python-basic "AdminUser"
```
**预期结果**：回答应表示未找到相关信息（或引用旧缓存不再存在）。

---

## 五、双模型配置验证 (M2-2.6)

### 5.1 配置向导交互验证
```bash
./codewiki.exe config
```
**人工检查点**：
- 先提示配置【文档生成模型】
- 再问"是否使用相同配置配置 embedding 模型？"
- 选择 "n" 后，应能独立配置 embedding 模型（如 Ollama + nomic-embed-text）

### 5.2 配置文件格式验证
```bash
# Windows
cat %USERPROFILE%\.codewiki\config.yaml
# macOS/Linux
cat ~/.codewiki/config.yaml
```
**预期格式**：
```yaml
generation:
  provider: openai
  api_key: sk-xxx
  model: gpt-4o
embedding:
  provider: ollama
  api_key: ""
  base_url: http://localhost:11434
  model: nomic-embed-text
```

### 5.3 向后兼容验证
```bash
# 手动写一份旧格式配置（无 generation/embedding 嵌套）
echo "provider: openai
api_key: sk-test
model: gpt-4o" > %USERPROFILE%\.codewiki\config.yaml

# 运行 generate，应正常读取并同时用于 generation 和 embedding
./codewiki.exe generate testdata/repos/python-basic -o testdata/out/verify-compat
```
**预期结果**：不报错，正常生成文档。

---

## 六、性能基准验证 (3.9 / 3.13)

```bash
go test -v ./benchmark -run TestBenchmark -timeout 10m
```
**人工检查点**：
- 测试应输出各阶段耗时（解析、图构建、Wiki 生成）
- 若你有 10 万行量级的真实仓库，可运行：
  ```bash
  ./codewiki.exe generate <大型仓库路径> -o testdata/out/verify-perf
  ```
- 用秒表或 `time` 命令测量总耗时，目标 < 3 分钟

---

## 七、CLI 完整性检查

```bash
./codewiki.exe --help
./codewiki.exe generate --help
./codewiki.exe ask --help
./codewiki.exe serve --help
./codewiki.exe config --help
```
**人工检查点**：所有帮助文本均为中文，flag 描述完整，无遗漏。

---

## 八、快速通过清单（5 分钟版）

如果你时间有限，只做这 5 步：

- [ ] `go test ./...` 全绿
- [ ] `./codewiki.exe generate testdata/repos/python-complex -o testdata/out/verify` 成功
- [ ] `api_reference.md` 中 `User` 类有 3 个 methods 且带语义描述
- [ ] `overview.md` 中有模块角色标签（如"核心领域"）
- [ ] `./codewiki.exe ask testdata/repos/python-basic "User 类有哪些方法？"` 回答正确且带源码引用

全部通过 = M3 核心功能验证完成。
