# DeepSeek Vision 结构化解析稳定性方案

## 1. 背景

本项目使用 DeepSeek Flash 多模态模型识别数学题图片，并完成以下工作：

1. 读取原题图片。
2. 生成正式、适合讲解的题目题干。
3. 提取题型、已知条件、关系、问题目标和图形结构。
4. 等待用户确认题目后，再进入解题和讲解流程。

视觉模型的返回内容具有概率性。即使开启 JSON Output，也只能主要保证返回内容是合法 JSON，不能天然保证字段名称、字段层级和数组元素类型永远与业务代码一致。

DeepSeek 官方文档要求在 Prompt 中明确要求 JSON 并提供示例，同时提醒 JSON Output 可能返回空内容，或者因 `max_tokens` 不足导致输出被截断：

- [DeepSeek JSON Output](https://api-docs.deepseek.com/guides/json_mode/)
- [DeepSeek Chat Completion](https://api-docs.deepseek.com/api/create-chat-completion/)
- [DeepSeek Responses API](https://api-docs.deepseek.com/api/create-response/)

## 2. 已出现的真实问题

同一类几何题，DeepSeek 曾返回过不同的字段协议。

### 第一种返回形式

```json
{
  "type": "几何证明与计算题",
  "known": ["四边形ABCD是矩形"],
  "asks": ["求证：BD⊥EC"],
  "diagram": {
    "figures": [
      {
        "id": "图1",
        "points": ["A", "B"],
        "segments": ["AB"],
        "intersections": [
          {"point": "F", "of": ["AD", "EC"]}
        ]
      }
    ]
  }
}
```

### 第二种返回形式

```json
{
  "questionType": "几何综合题",
  "knownConditions": ["四边形ABCD是矩形"],
  "asks": ["求证：BD⊥EC"],
  "diagram": {
    "figures": [
      {
        "figureNo": "图1",
        "points": [
          {"name": "A", "coordinates": {}},
          {"name": "B", "coordinates": {}}
        ],
        "segments": [
          {"name": "AB"}
        ],
        "intersections": [
          {"lines": ["AD", "EC"], "point": "F"}
        ],
        "collinear": [
          {"points": ["A", "F", "D"], "description": "点F在AD上"}
        ]
      }
    ]
  }
}
```

因此，不能把模型输出直接反序列化到最终业务模型中。

### 第三种返回形式：字段类型漂移

```json
{
  "shape": ["triangle", "right triangle", "quadrilateral"],
  "layoutHints": [
    {"aspectRatio": 0.72},
    {"orientation": "portrait"}
  ],
  "constraints": [
    {"type": "right angle", "at": "C", "of": ["AC", "BC"]},
    {"type": "midpoint", "point": "D", "of": "AB"},
    {"type": "point on segment", "point": "F", "on": "BC"}
  ]
}
```

这类响应虽然是合法 JSON，但不能直接进入固定类型反序列化。适配层应将 `shape` 收敛为最具体的单个形状，将多个 `layoutHints` 合并为一个对象，并把 `of/on/at` 映射到统一约束字段。此类可确定恢复的漂移不得消耗模型纠错重试。

## 3. 推荐架构

```text
DeepSeek Vision
      ↓
JSON / JSON Schema 输出
      ↓
供应商响应兼容层
      ↓
统一的内部 StructuredQuestion
      ↓
格式校验 + 业务语义校验
      ↓
一次有限纠错重试
      ↓
用户确认原图与正式题干
      ↓
解题、规划、讲解
```

核心原则：

- DeepSeek 的字段结构不是业务契约。
- 业务代码只依赖统一后的内部模型。
- 所有供应商字段差异都收敛在 AI 适配层。
- 原始响应必须保留，便于重放和诊断。
- 解析失败不能直接进入解题阶段。

## 4. 统一内部协议

项目内部建议固定使用以下结构：

```json
{
  "schemaVersion": "question-vision-v1",
  "grade": 9,
  "topic": "geometry_numeric",
  "normalizedText": "正式题目题干……",
  "knowns": [
    {
      "name": "AB",
      "value": "1",
      "unit": ""
    }
  ],
  "relations": [
    "E、A、B共线"
  ],
  "asks": [
    "求证：BD⊥EC"
  ],
  "diagram": {
    "figures": [
      {
        "id": "figure_1",
        "title": "图1",
        "points": ["A", "B", "C", "D", "E", "F", "G"],
        "segments": ["AB", "BC", "CD", "DA", "AE", "EC", "BD"],
        "curves": [
          {"type": "arc", "endpoints": ["A", "B"], "center": "O", "sweep": "upper"}
        ],
        "intersections": [
          {
            "point": "F",
            "of": ["AD", "EC"]
          }
        ],
        "collinear": [
          ["E", "A", "B"]
        ],
        "coordinates": {}
      }
    ]
  }
}
```

模型可以有多种输入格式，但进入领域层后必须统一为这一种格式。

## 5. Prompt 约束

Prompt 中应明确要求：

1. 只能使用约定字段名。
2. 不允许使用字段别名，例如 `questionType`、`knownConditions`、`figureNo`。
3. `points` 必须是字符串数组。
4. `segments` 必须是字符串数组。
5. `intersections` 必须是对象数组，字段固定为 `point` 和 `of`。
6. `collinear` 必须是字符串数组的数组。
7. 没有内容时返回空数组，不要省略字段或改变字段类型。
8. 不要根据图片视觉比例臆造坐标。
9. 图形题必须保留每一张图，不能把图1和图2合并。
10. `shape` 必须是单个字符串，不能是字符串数组。
11. `layoutHints` 必须是单个对象，不能是数组。
12. `constraint.type` 使用 snake_case，约束对象统一使用 `points`、`lines`、`segments`。
13. `knowns` 固定使用 `{name,value,unit}[]`，不要返回 `{id,text,type,points}` 形式的供应商自定义对象。
14. `layoutHints.anchors` 只能使用 `point → {x,y}`，且坐标范围为 0～1；禁止使用 `top`、`bottom_left` 等语义位置字符串。
15. 非几何题不要强行生成 `diagram.figures`；几何题中的圆弧必须使用 `curves`，不能只把“半圆”写在自然语言中。
16. 题型先标记为 `geometry`、`function`、`word_problem`、`arithmetic` 或 `other`，再执行对应扩展规则；公共字段仍必须遵循同一内部协议。

Prompt 示例：

```text
只输出 JSON，不输出 Markdown。
必须严格使用下面的字段名和数据类型，不得使用同义字段名：
- topic: string
- knowns: object[]
- relations: string[]
- asks: string[]
- diagram.figures: object[]
- figure.points: string[]
- figure.segments: string[]
- figure.intersections: object[]，每项必须是 {"point": string, "of": string[]}
- figure.collinear: string[][]
- figure.shape: string，只能是单个形状
- figure.layoutHints: object，不得返回数组
- figure.constraints: object[]，type 使用 snake_case，不得用 of/on 代替 lines/segments

缺失字段必须返回空数组或空对象，不得改成其他类型。
```

## 6. 解析与归一化

适配层建议采用以下策略：

### 字段别名

```text
topic             ← topic / type / questionType
knowns            ← knowns / known / knownConditions
figure.id         ← id / figureNo
```

### 字符串数组和对象数组

以下输入都应归一化为字符串数组：

```json
["A", "B"]
```

或：

```json
[{"name":"A"}, {"name":"B"}]
```

### 交点字段

同时兼容：

```json
{"point":"F", "of":["AD", "EC"]}
```

```json
{"point":"F", "lines":["AD", "EC"]}
```

旧版映射也可以兼容：

```json
{"F":"AD与EC"}
```

但内部统一输出对象数组，不要继续使用映射作为主模型，因为一个交点可能涉及多条线。

### 图形语义字段

- `shape: string|string[]` 统一为单个字符串；多个候选同时存在时选择更具体的形状，例如 `right_triangle` 优先于 `triangle` 和 `quadrilateral`。
- `shape: semicircle` 只有在响应明确提供直径和圆心时，才可归一化为 `curves: [{type:"arc", endpoints:[...], center:"...", sweep:"upper|lower"}]`；不得根据图形比例猜测。
- `curves` 统一为对象数组；当前支持 `type=arc`，端点必须属于当前 figure，圆心和方向必须可验证。
- `layoutHints: object|object[]|string[]` 统一为一个对象；只保留 `aspectRatio`、`orientation`、`anchors`。
- `layoutHints.anchors` 只保留带有数值 `x/y` 的坐标对象；语义位置字符串不能可靠换算为坐标，应丢弃并仅在原始 trace 中保留。
- `knowns` 的 `{id,text,type,points,...}` 富对象协议统一读取 `text` 并转换为内部 `Known.Name`，供应商专属字段不进入领域模型。
- `grade` 只有在供应商返回明确数字或数字字符串时才归一化为整数；`初中` 等无法唯一映射的教育阶段值不得猜测，删除该字段并写入 `uncertainItems`。
- 约束类型统一为 snake_case，例如 `right angle → right_angle`、`point on segment → point_on_segment`。
- `at` 补入 `points`，`of/on` 根据约束类型补入 `lines` 或 `segments`。
- 文本提取出的全局约束只能补入包含全部引用点的 figure，禁止把图1的辅助线关系传播到图2。

归一化只处理可以确定恢复的协议差异，不猜测缺失的数学关系。无法确定的内容必须进入校验或 `uncertainItems`。

## 7. 校验策略

校验应分成两层。

### JSON 格式校验

- 响应必须是合法 JSON。
- 必须存在题干文本。
- `topic` 必须非空。
- 数组字段必须是数组。
- 图形字段必须满足统一类型。

### 数学业务校验

- 图形题必须存在 `diagram.figures`。
- 每张图至少有点或线段。
- 交点中的点必须出现在 points 中，或者可以从关系中补充。
- 交点 `of` 不能是空数组。
- 共线关系至少包含两个点。
- `layoutHints.aspectRatio` 必须为正数，`orientation` 只能是 `portrait` 或 `landscape`。
- `layoutHints.anchors.*.x/y` 必须是 0～1 范围内的有限数值。
- 每条 constraint 必须有非空 `type`，且引用点必须属于当前 figure。
- 多图题的约束必须通过点集归属校验，不能跨图传播。
- 题目必须存在一个问题目标。
- 不确定项必须进入 `uncertainItems`，不能静默猜测。

校验失败时不要直接进入 `structuring`、`solving` 或 `planning`。

## 8. 纠错重试

建议最多重试 1～2 次。

第一次解析失败时，保留：

- 原始图片
- 原始模型响应
- 解析错误
- 失败字段路径
- provider、model、promptVersion、schemaVersion

然后向模型发送明确的纠错信息：

```text
上一次输出未通过结构校验：
- diagram.figures[0].points 必须是 string[]，不能是 object[]
- intersections 必须使用 of 字段，不能使用 lines
- topic 字段不能为空

请根据原图重新输出完整 JSON，不要只输出修改片段。
```

如果再次失败，应进入用户确认/重新解析状态，而不是无限重试。

## 9. 超时和截断

视觉题解析通常比纯文本更慢，当前项目曾出现：

```text
context deadline exceeded
```

建议：

- 视觉解析超时从 30 秒提高到 60～90 秒。
- 设置合理的 `max_tokens`。
- 检查 `finish_reason`，如果是 `length`，不能把响应当成完整结果。
- 将结构解析错误和请求超时分别保存，避免后一个重试错误覆盖前一个更有价值的错误。
- 每次尝试记录开始时间、结束时间和耗时。

DeepSeek 官方文档也特别提醒要合理设置 `max_tokens`，避免 JSON 输出被截断。

## 10. 可观测性和回放

`ocr_traces` 建议至少记录：

- lesson_id
- provider
- model
- request_id
- prompt_version
- schema_version
- 原始请求摘要
- 原始响应
- finish_reason
- 解析错误
- 解析失败字段路径
- 请求耗时
- 创建时间

生产问题必须可以使用数据库中的原始响应离线回放，而不需要再次调用 DeepSeek。

## 11. 当前项目的落地清单

- [x] DeepSeek Vision 使用图片输入。
- [x] 原始响应写入 `ocr_traces`。
- [x] 识别阶段和确认阶段拆分。
- [x] 确认页展示原题图片。
- [x] 图形题保留多张 figures。
- [x] 兼容 `asks`、`relations`、`knowns` 的多种格式。
- [x] 兼容交点对象数组和旧映射格式。
- [x] 兼容 `questionType`、`knownConditions`、对象化 points/segments/collinear。
- [x] 在供应商适配层兼容 `shape` 的 string/string[] 漂移并收敛为单值。
- [x] 兼容数组化 `layoutHints` 以及约束中的 `of/on/at` 别名。
- [x] 过滤语义字符串 anchors，只保留合法归一化坐标，并校验坐标范围。
- [x] 兼容 `{id,text,type,points}` 形式的 knowns 富对象并收敛到内部 Known。
- [x] 增加约束点集归属校验，阻止多图题约束串图。
- [x] 前端按统一后的交点结构绘制图形。
- [x] 增加正式 JSON Schema 版本（`docs/schemas/question-vision.schema.json`）。
- [x] 保留 `response_format=json_object`，并在适配层以固定 schemaVersion 校验；待供应商稳定支持 JSON Schema 后可继续切换原生接口。
- [x] 增加结构校验失败后的单次完整 JSON 纠错重试。
- [x] 将默认视觉解析超时提高到 75 秒，并设置 `max_tokens=4096`。
- [x] 在数据库中记录 schemaVersion、promptVersion、finishReason、解析错误路径和耗时。
- [x] 增加两种历史字段协议、截断响应和字段漂移纠错的回放测试。
- [x] 兼容 `grade` 返回教育阶段字符串的类型漂移，不因无法唯一映射而触发模型重试。
- [x] 将视觉 Prompt 拆成公共题型约束与几何、函数、应用题扩展，避免所有题目都套用几何规则。
- [x] 兼容历史 `shape=semicircle` 响应并归一化为可渲染的 `arc` 曲线。

## 12. 新会话使用说明

在新会话中继续开发时，可以直接提供以下上下文：

```text
请先阅读 docs/DEEPSEEK-VISION-ROBUSTNESS-GUIDE.md。

本项目的 DeepSeek Vision 输出不是稳定业务协议，必须经过供应商响应兼容层、统一内部模型、JSON/业务校验和有限纠错重试后，才能进入后续解题流程。

优先完成文档中的未完成落地项，不要把新的模型字段差异扩散到业务层或前端。
```
