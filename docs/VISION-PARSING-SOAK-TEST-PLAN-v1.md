# 数学题视觉解析循环与边界回放测试计划 v1

## 1. 目标

使用两张真实几何题图片反复调用当前 DeepSeek Vision 解析链路，持续发现模型输出的不确定字段形态、语义缺失、多图串图和图形还原偏差，并把每一种新失败自动沉淀为可重复执行的离线回归用例。

测试图片：

1. `/Users/nono/Downloads/几何 2.png`
2. `/Users/nono/Downloads/几何.png`

本计划覆盖以下完整路径：

```text
真实图片
→ DeepSeek Vision
→ 原始响应留档
→ 供应商响应归一化
→ StructuredQuestion 反序列化
→ 格式与数学语义校验
→ Lesson 状态流转与数据库持久化
→ 前端图形布局
→ 失败聚类
→ 回放夹具与回归测试
```

## 2. 重要原则

1. 遵守根目录 `AGENTS.md` 和 `docs/DEEPSEEK-VISION-ROBUSTNESS-GUIDE.md`。
2. 真实模型响应是不稳定输入，不能通过无限重试掩盖解析缺陷。
3. Prompt、归一化、内部协议、业务校验和有限纠错必须分层处理。
4. 生产代码不得为两道题硬编码点名、图形坐标或特殊分支。
5. 测试夹具可以记录每张图片的已知数学不变量，但只能用于断言，不得进入生产解析逻辑。
6. 每个新失败必须先保存原始响应和失败路径，再变成红色回归测试，之后才能修复。
7. 不在主业务数据库中无控制地创建大量 Lesson；完整链路循环使用隔离环境或明确标记并记录所有测试 Lesson ID。
8. API Key 只从环境变量读取，不写入代码、测试夹具、日志或报告。

## 3. 测试分层

### 3.1 A 层：离线响应回放

用途：快速、确定性、零模型调用成本地验证解析、归一化和校验边界。

输入：

- 数据库 `ocr_traces.raw_response` 中已经出现的真实响应。
- 真实循环中新捕获的成功或失败响应。
- 从真实响应派生的最小字段变异样本。

输出：

- 标准化后的 `StructuredQuestion`。
- 精确失败路径。
- 是否需要模型纠错。
- 字段形态指纹。

建议新增：

```text
backend/infra/ai/testdata/vision/
  geometry-01/
    manifest.json
    responses/
  geometry-02/
    manifest.json
    responses/
```

每个响应夹具需要保存：

- case ID。
- 来源图片 SHA-256，不复制或嵌入图片内容。
- Prompt 版本和 Schema 版本。
- 原始供应商响应。
- 期望结果：成功或失败。
- 期望失败路径。
- 期望归一化摘要。

测试入口建议：

```text
TestVisionTraceCorpus
```

该测试枚举全部夹具，并调用生产代码使用的同一个“响应解码 → 归一化 → 校验”入口，禁止在测试中复制一套解析逻辑。

### 3.2 B 层：真实模型循环

用途：利用同一图片多次响应的随机差异，主动发现未知字段漂移。

建议新增命令：

```text
cmd/vision-soak/main.go
```

建议参数：

```text
--image <path>                 可重复指定
--rounds <n>                   每张图片轮数，默认 5
--delay <duration>             请求间隔，默认 2s
--parallel <n>                 并发数，默认 1
--max-requests <n>             包含纠错请求的硬上限
--stop-after-clean <n>         连续多少次无新失败后停止
--max-unique-failures <n>      唯一失败指纹上限
--output <dir>                 本轮产物目录
--continuous                   显式开启持续运行，默认禁止无限循环
```

命令直接复用：

- `config.Load()`。
- `ai.NewDeepSeekVisionAnalyzer(...)`。
- 当前 Prompt、归一化和校验实现。

不允许脚本自行拼装另一套 Prompt 或字段协议。

每次请求记录：

- run ID、case ID、round。
- 图片路径和 SHA-256。
- provider、model、Prompt/Schema 版本。
- 开始时间、结束时间、耗时。
- `finish_reason`。
- 原始响应。
- 归一化结果。
- 校验结果和失败路径。
- 是否消耗纠错重试。
- 响应 token 使用量（供应商返回时记录）。

### 3.3 C 层：完整 Lesson 流程

用途：验证直接调用视觉适配器成功后，进入 Worker、状态机、数据库和版本表时仍然正确。

该层不需要像 B 层一样高频运行。建议每次代码修复后，对每张图片运行 2～3 轮。

覆盖表：

- `lessons`
- `lesson_status_events`
- `job_attempts`
- `ocr_traces`
- `question_versions`

必须断言：

- Lesson 最终进入 `awaiting_confirmation`。
- 每次模型调用都保留独立 `ocr_traces`。
- 成功时只生成预期版本数量的 `question_versions`。
- 失败阶段、失败路径和重试次数准确。
- 重试成功后，旧失败不能覆盖最终成功状态。
- `question_json` 中不存在仅由类型名组成的伪 asks，例如 `expression`、`proof_and_expression`。

环境隔离优先级：

1. 独立 `guide_flow_soak` MySQL schema、独立 Redis stream/group、独立 MinIO key prefix。
2. 如果暂时复用本地开发环境，必须在报告中记录全部测试 Lesson ID，不自动执行删除。

### 3.4 D 层：图形还原验证

用途：发现“解析成功，但图形关系或布局错误”。

后端结构断言：

- figure 数量正确。
- 每张图 points、segments、intersections、collinear 完整。
- 约束引用的点线属于当前 figure。
- 图 1 的辅助点线不会传播到图 2。
- `length_compare`、`midpoint`、`right_angle`、`perpendicular`、`point_on_segment`、`extension` 等关系保留。
- `layoutHints` 只包含合法弱提示，不作为数学事实。

前端布局断言：

- 为 `frontend/src/components/lesson/diagramLayout.ts` 引入 Vitest 单元测试。
- 输入保存后的 `DiagramFigure`，验证输出坐标满足数学不变量。
- 不比较固定像素截图作为唯一依据，优先比较点在线段内、共线、中点、直角边长关系等结构性质。
- 可选增加 SVG/页面截图，用于人工复核，但截图不能代替结构断言。

## 4. 两张图片的基线断言

开始循环前先人工查看原图，并将以下内容写入各自 `manifest.json`。这里只记录测试预期，不进入生产代码。

### 4.1 `/Users/nono/Downloads/几何.png`

当前已知基线：

- 至少包含图 1、图 2 两张图。
- 存在矩形 ABCD。
- `AB < AD`，还原时不能默认正方形。
- E、A、B 存在延长线或共线关系。
- 各图中的辅助线必须分别保存，禁止多图合并。

实施时需要根据原图再次确认并补全：

- 每张图的必需 points。
- 每张图的必需 segments。
- 交点 F、G 的所在直线。
- 图 1 与图 2 的差异线段。

### 4.2 `/Users/nono/Downloads/几何 2.png`

当前已知基线：

- `∠C = 90°`。
- `AC > BC`。
- D 是 AB 的中点。
- 图 1 中 E 是 AC 的中点。
- 图 1 中 F 在线段 BC 内，不在延长线上。
- `DF ⟂ DE`。
- 图 2 只允许保留原图已经给出的基础点线；需要补全的 E、F 不能被模型伪装成原图已明确绘制内容。

实施时需要根据原图确认：

- 图 1、图 2 各自的实际 segments。
- 第（2）问中“待补全”信息如何进入 `uncertainItems` 或题目目标，而不是变成伪坐标。

## 5. 字段边界矩阵

离线变异测试至少覆盖下表。变异必须从真实响应生成，不能凭空构造与生产无关的协议。

| 区域 | 需要覆盖的输入形态 | 内部结果 |
|---|---|---|
| topic | `topic/type/questionType`、中文题型 | 统一 topic |
| knowns | string、`{name,value,unit}`、`{id,text,type,...}` | `Known[]` |
| asks | string、`{type,target}`、`{id,text,type}`、`{question,goal,figure}` | 有语义的 string[] |
| relations | string、对象、单复数字段 | 统一关系文本 |
| expressions | string、对象 | `MathExpression[]` |
| points/segments | string[]、对象数组 | string[] |
| shape | string、string[]、中文/英文别名 | 单个规范 shape |
| intersections | 数组、`lines/of`、旧 map | 对象数组 |
| collinear | string[][]、对象数组 | string[][] |
| constraints | `point/points`、`segment/segments`、`at/of/on` | 规范约束 |
| layoutHints | object、object[]、string[] | 单个合法对象 |
| anchors | `{x,y}`、语义字符串、数组 | 只保留合法坐标 |
| coordinates | number/string、缺失、空对象 | 明确坐标或空 |
| figures | 缺 ID、figureNo、多图混合 | 稳定 ID 且多图隔离 |
| envelope | Markdown fence、空 content、截断、finish_reason=length | 精确失败路径或纠错 |

## 6. 语义质量断言

“JSON 能解析”不能算成功。每轮至少检查：

### 6.1 题干

- 题号和所有小问存在。
- 数字、比较符号、垂直符号、根号和变量未丢失。
- 不把答案或推导结果写成题目已知条件。
- 待补全图形不能被描述成原图已完整给出。

### 6.2 asks

- 数量不少于原题小问数量。
- 每一项必须包含真实问题语义。
- 禁止只保存 `expression`、`proof` 等分类标签。
- 多字段 asks 按 `text → question → goal → type+target` 的优先级归一化。

### 6.3 knowns 和 relations

- 已知条件文本不能因对象协议变化而丢失。
- 模型推断不能伪装为题干明确条件。
- 同一数学关系可以去重，但不能因字段名差异被重复添加。

### 6.4 diagram

- 每个约束引用的点必须属于当前图。
- 线段端点必须存在。
- 共线关系至少两个点。
- 中点必须同时存在中点和所属线段。
- point_on_segment 必须区分线段内部、端点和延长线。
- `layoutHints` 与数学约束冲突时，以数学约束为准。

## 7. 失败指纹与聚类

避免同一种失败重复生成大量报告。建议指纹包含：

```text
promptVersion
+ parseErrorPath
+ 规范化后的错误类型
+ 发生漂移的 JSON 类型签名
```

示例：

```text
question-vision-v4|asks|object(question,goal,figure)->empty
question-vision-v4|layoutHints.anchors|map[string]string
question-vision-v4|knowns[0]|object(id,text,type,points)
```

每个唯一指纹保存：

- 首次和最近出现时间。
- 出现次数。
- 图片 case ID。
- 最小响应片段。
- 完整原始响应路径。
- 对应回归测试名称。
- 修复状态。

## 8. 产物目录与报告

默认输出到不提交 Git 的运行目录：

```text
var/vision-soak/<run-id>/
  manifest.json
  runs.ndjson
  summary.json
  report.md
  raw/
  normalized/
  failures/
```

需要在 `.gitignore` 中忽略 `var/vision-soak/`，但经人工筛选并脱敏后的回归夹具应复制到 `backend/infra/ai/testdata/vision/`。

最终 `report.md` 至少包含：

- 总请求数和每张图片轮数。
- 成功、纠错成功、最终失败数量。
- 平均/P95 耗时。
- Prompt/Schema 版本分布。
- 唯一失败指纹及次数。
- asks、knowns、diagram 语义断言失败数。
- 每张图的结构覆盖率。
- 新增回归测试列表。
- 尚未解决的问题和下一轮建议。

## 9. 调用成本和安全阀

默认不允许无限循环。

建议阶段：

1. 试运行：每张图片 3 次，共 6 次基础请求。
2. 稳定性运行：每张图片 10 次。
3. 修复后验证：每张图片 20 次，或连续 10 次没有新失败指纹。
4. 长时间 soak：只有显式传入 `--continuous` 时启用。

安全阀：

- 默认并发为 1。
- 每次请求间隔至少 2 秒。
- `--max-requests` 必须包含模型纠错请求。
- 达到唯一失败上限后自动停止，先修复再继续。
- 捕获 SIGINT/SIGTERM，退出前写入 checkpoint 和报告。
- 不在日志中打印 API Key、完整请求 Header 或图片 Base64。
- 供应商限流、认证失败或连续网络失败时立即停止，不进行高频重试。

## 10. 实施顺序

### 阶段 0：锁定当前 asks 漂移

1. 从 lesson `lesson-1791713652928739000` 的 trace 提取两种 asks 对象。
2. 添加两个最小失败测试：
   - `{question,goal,figure}`。
   - `{id,text,type}`。
3. 修复 `decodeAsksText`，确保保存真实问题文本。
4. 增加“只有类型标签的 asks 不得通过”的语义校验。

### 阶段 1：建立纯解析回放入口

1. 将 HTTP 请求与响应内容解码拆开。
2. 形成生产和测试共用的纯函数入口：

```text
raw provider response
→ decode envelope
→ normalize
→ deserialize
→ validate
→ OCRResult
```

3. 保持供应商差异仅位于 `backend/infra/ai`。
4. 建立 `TestVisionTraceCorpus`。

### 阶段 2：实现真实模型 soak 命令

1. 实现 `cmd/vision-soak`。
2. 支持两张图片、多轮、限流、停止条件和产物目录。
3. 保留每次原始响应和规范化摘要。
4. 生成失败指纹和 Markdown 报告。

### 阶段 3：建立图片期望清单

1. 人工查看两张原图。
2. 为每张图片建立 `manifest.json`。
3. 只记录数学不变量和图间差异，不记录固定渲染坐标。
4. 实现通用断言器读取 manifest。

### 阶段 4：运行首轮循环

1. 每张图片运行 3 次试运行。
2. 修复脚本和报告问题。
3. 每张图片运行 10 次稳定性循环。
4. 将每个唯一字段漂移转成离线测试。
5. 先修复高频且可确定恢复的漂移，再继续扩大轮数。

### 阶段 5：完整链路与图形布局

1. 建立隔离的 MySQL、Redis 和对象存储前缀。
2. 每张图片运行 2～3 次完整 Lesson 流程。
3. 校验数据库状态流转和 trace 历史。
4. 为 `diagramLayout.ts` 增加结构性质测试。
5. 使用成功解析结果验证两道题的图形还原不变量。

### 阶段 6：稳定性验收

1. 每张图片至少完成 20 次真实模型解析。
2. 连续 10 次没有新增失败指纹。
3. 所有已知字段漂移均有离线回归测试。
4. 完整 Lesson 流程全部到达 `awaiting_confirmation`。
5. asks、knowns 和多图结构没有语义降级。
6. 前端布局结构断言全部通过。

## 11. 验收标准

- `go test ./...` 通过。
- 前端类型检查和构建通过。
- 全部离线 trace 回放通过。
- 可恢复字段漂移不消耗模型纠错重试。
- 不可恢复数据以精确路径失败，不进入后续解题流程。
- 两张图片各连续 10 次没有新增失败指纹。
- 最终报告可以定位每次失败对应的原始响应。
- 主业务数据库没有测试数据污染。
- 没有新增题目专用生产分支或固定坐标。

## 12. 新会话启动提示词

在新会话中发送以下内容：

```text
请执行 docs/VISION-PARSING-SOAK-TEST-PLAN-v1.md。

先阅读根目录 AGENTS.md、docs/DEEPSEEK-VISION-ROBUSTNESS-GUIDE.md 和该计划全文。
测试图片：
1. /Users/nono/Downloads/几何 2.png
2. /Users/nono/Downloads/几何.png

从计划的“阶段 0：锁定当前 asks 漂移”开始，按照失败测试先行的方式实施。完成每个阶段后运行相应验证并更新计划中的执行记录。不要把 API Key 写入文件或日志，不要在主业务数据库中批量制造测试数据，不要针对单道题在生产代码中硬编码。
```

## 13. 执行记录

当前状态：`代码能力已实现，真实 soak 与完整 Lesson 隔离环境待运行`

| 阶段 | 状态 | 结果 |
|---|---|---|
| 阶段 0：asks 漂移 | 已完成 | `backend/infra/ai/vision_corpus_test.go` 覆盖 question/goal 与 id/text，并拒绝只有 type 的 asks |
| 阶段 1：离线回放入口 | 已完成 | 夹具位于 `backend/infra/ai/testdata/vision/`，复用生产 decode/normalize/validate 入口 |
| 阶段 2：真实模型 soak 命令 | 已完成 | `cmd/vision-soak`，输出 `var/vision-soak/<run-id>/`，不记录 API Key |
| 阶段 3：图片 manifest | 已完成 | `docs/vision-baselines/geometry-01/` 与 `geometry-02/`，记录图片 SHA 和人工确认的不变量 |
| 阶段 4：首轮循环 | 待实施 | - |
| 阶段 5：完整链路与布局 | 待实施 | - |
| 阶段 6：稳定性验收 | 待实施 | - |
