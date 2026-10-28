# 解题草稿供应商适配协议

## 目标

解题生成链路只允许业务层依赖 `solution-draft-v1`。DeepSeek、其他兼容 OpenAI Chat Completions 的服务，或未来的自研 Agent，都可以拥有不同的响应包装，但这些差异必须停留在 `backend/infra/ai`。

## 边界

```text
供应商响应
  → DraftNormalizer（容错解析、有限别名/包装兼容）
  → SolutionDraft（固定内部协议）
  → validateDraft（格式与业务门禁）
  → Worker / Verifier / 前端
```

`DraftNormalizer` 是可插拔接口。DeepSeek 默认使用 `DeepSeekDraftNormalizer`；接入其他供应商时实现新的归一化器，并通过 `HTTPLLMConfig.Normalizer` 注入，不修改 Worker、Verifier 或领域模型。

## 兼容原则

- 只登记可解释的包装层，例如 DeepSeek 当前出现的 `content.draft` 和 `content.draft.solutionDraft`。
- 包装层遍历有最大深度，字段集合是白名单；未知结构直接失败，不递归猜测“看起来像答案”的字段。
- 可确定的字段转换在适配器内完成，例如 `solutionDraft[].steps[].text` 展平为 `steps`，`conclusion` 归入 `expressions`。
- 步骤对象采用“别名优先、语义推断兜底”：优先识别 `text/description/desc` 和 `expression/formula/expr`；只有在候选字段唯一，或明确存在一个自然语言字段与一个数学表达式字段时才推断。
- 无法确定的数学事实不得自动补全，保留在 `uncertainties` 或让本次生成失败并记录原始响应。
- 每次归一化记录 `schemaVersion`、`shape`、warnings 和未知字段路径；失败时保留供应商原文，便于新增真实回放样本。

语义推断只解决字段命名和容器类型漂移，不负责推断数学事实。出现两个以上同类候选字段时必须失败，不能使用“第一个字段”“最长字符串”等不可解释规则静默选值。

## 迁移规则

修改内部协议时必须同步更新：

1. `backend/domain/solution` 中的协议与校验；
2. `backend/infra/ai` 的 Prompt、Normalizer 和有限纠错逻辑；
3. `docs/schemas/solution-draft.schema.json`；
4. 真实供应商响应回放测试；
5. 下游存储与 API 的兼容测试。

供应商新增字段不应直接添加到 `SolutionDraft`。只有成为跨供应商、稳定且有业务语义的概念时，才升级内部协议。
