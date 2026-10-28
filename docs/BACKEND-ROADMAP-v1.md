# 后端剩余模块与执行计划

版本：v1.0  
范围：后端与基础设施，不包含前端工作

## 1. 当前基础

目前已经完成 Go 模块化后端的最小骨架：

- Lesson 领域模型和基础状态
- 创建、查询、确认题目、提交练习 API
- OCR → 结构化 → 求解 → 教学规划 → 渲染的 Worker 流程
- `OcrAdapter`、`Solver`、`LessonPlanner`、`Renderer` 等可替换接口
- 内存仓储、内存图片存储和内存任务队列
- Demo Adapter、低置信度确认分支和基础测试

当前实现仅用于验证模块边界和流程，尚未具备生产持久化和真实模型调用能力。

## 2. 技术约束

### 2.1 持久化必须使用 MySQL

本项目后端持久化统一使用 **MySQL**，不使用 PostgreSQL。

选择 MySQL 的主要原因：

- 项目优先复用现有 MySQL 运维、备份和监控能力
- 团队对 MySQL 的开发、排障和发布流程更熟悉
- 当前 MVP 的核心数据以关系型表为主，不依赖 PostgreSQL 专有能力
- 题目结构、关系、LessonPlan 等半结构化内容使用 MySQL JSON 类型保存即可
- 减少数据库种类，降低本地开发和部署成本

实现时需要遵守：

- 使用 InnoDB
- 使用事务保证 Lesson 与状态事件的一致性
- 使用 `JSON` 保存结构化题目和讲解计划
- 使用 `VARCHAR` 保存状态，不依赖 PostgreSQL ENUM
- 使用 `DATETIME(3)` 保存时间
- 使用应用层校验状态转换，并在数据库层增加必要约束
- 通过迁移脚本管理表结构，不手工修改生产数据库

## 3. P0：真实闭环必须完成

### 3.1 MySQL 持久化模块

新增：

```text
backend/infra/persistence/mysql/
db/migrations/
```

建议的核心表：

```text
lessons
question_drafts
structured_questions
lesson_plans
lesson_steps
practice_results
lesson_status_events
```

需要支持：

- Lesson 创建、查询和更新
- 题目识别结果保存
- 结构化题目保存
- 已验证解法和教学计划保存
- 练习提交记录
- 全部状态变化记录
- 失败原因、重试次数和最后错误时间
- 事务、索引和幂等更新

完成后替换当前的 `MemoryRepository`。

### 3.2 对象存储模块

实现真实图片存储 Adapter：

- 保存原图
- 保存图片元数据
- 校验 JPG、PNG 和文件大小
- 生成缩略图
- 返回临时访问地址
- 支持图片清理

对象存储仍使用 S3 兼容接口；数据库只保存对象 key 和必要元数据，不保存图片二进制。

### 3.3 正式题目模型与结构化模块

新增：

```text
backend/question/
```

职责：

- 将 OCR 文本转换为统一题目模型
- 提取题干、数字、单位、公式和图形描述
- 识别年级、题型和知识点
- 提取已知条件、数量关系和问题目标
- 判断结构是否完整
- 对字段和整体结果进行置信度判断

### 3.4 数学求解与校验模块

新增：

```text
backend/solving/
```

必须包含：

- 结构化算式计算
- 单位检查
- 结果代回题目条件
- 多解法结果一致性检查
- 五、六年级知识范围约束
- 校验失败时停止生成讲解

LLM 可以提出解法，但不能作为唯一计算器。

### 3.5 教学规划与 LessonPlan 校验

新增：

```text
backend/pedagogy/
backend/rendering/
```

需要保证：

- 讲解不超过 3 步
- 先解释题意和关键关系，再展示算式
- 语言适合长辈照着讲给孩子
- 支持 `ratio_bar`、`groups`、`number_line`、`formula_reveal`
- 视觉参数合法
- 讲解、公式、校验结果和最终答案一致
- 自动生成一道相似练习题

## 4. P1：可靠运行能力

### 4.1 Redis 队列与 Worker

当前内存 Channel 需要替换为 Redis 队列，实现：

- 任务入队和消费确认
- Worker 重启后继续处理
- 最大重试次数
- 指数退避
- 单任务超时
- 幂等消费
- 失败任务记录

### 4.2 完整状态机

统一状态转换，不允许 Worker 直接随意修改状态：

```text
created → recognizing → awaiting_confirmation → planning
                                      ↓
                                  rejected
planning → validating → rendering → ready
              ↓
            failed
```

所有状态变化必须写入 MySQL 的 `lesson_status_events`。

### 4.3 API 完善

需要补充：

- 上传类型、大小和内容校验
- 请求体大小限制
- 统一错误码和错误响应
- 请求 ID
- 幂等键
- 用户年级等 `learnerProfile` 参数
- 任务进度和阶段信息
- 题目拒绝接口
- 超时和请求取消

第一版继续采用轮询，不优先实现 SSE。

### 4.4 配置和依赖注入

新增配置模块，支持：

- HTTP 端口
- MySQL DSN
- Redis 地址
- S3 配置
- OCR、LLM、TTS 配置
- 超时和重试次数
- 最大上传大小
- 开发、测试、生产环境

服务启动入口不再直接写死 Demo Adapter 和内存实现。

### 4.5 日志与指标

需要记录：

- 请求 ID 和 Lesson ID
- 各阶段耗时
- OCR 置信度
- OCR 失败率
- 求解校验失败率
- Worker 重试次数
- 任务成功率
- 从上传到 ready 的总耗时

## 5. P1：真实外部 Adapter

### 5.1 OCR Adapter

通过统一接口接入真实 OCR，保留：

- 供应商隔离
- 超时和重试
- 原始响应保存
- 结构化输出校验
- 低置信度转人工确认

### 5.2 LLM Adapter

用于题目结构化和教学规划，必须支持：

- Prompt 模板隔离
- 模型选择
- JSON Schema 校验
- 超时、重试和降级
- Token 和费用统计
- 模型输出不直接作为最终数学答案

### 5.3 TTS Adapter

可选实现，用于根据 `voiceText` 生成普通话音频。TTS 失败不应阻塞图文讲解进入 ready。

## 6. P2：安全与质量

### 6.1 API 与文件安全

- 文件 MIME 类型和真实内容双重校验
- 图片尺寸和压缩炸弹防护
- 文件名清洗
- 路径穿越防护
- 请求限流
- 日志脱敏

### 6.2 模型安全

- 防止图片/OCR 文本中的提示词注入
- 限制模型输出格式
- 禁止模型绕过求解校验
- 对超纲、无法验证和信息不完整的题目明确失败

### 6.3 测试体系

- MySQL Repository 集成测试
- 状态机测试
- Worker 重试和超时测试
- OCR 低置信度测试
- 求解校验失败测试
- Redis 队列测试
- 对象存储测试
- 上传到 ready 的端到端测试
- 典型小学数学题回归样本

## 7. 执行顺序

```text
第一阶段：MySQL + 迁移 + Repository
        ↓
第二阶段：S3 对象存储 + 上传校验
        ↓
第三阶段：题目模型 + 结构化模块
        ↓
第四阶段：数学求解 + 规则校验
        ↓
第五阶段：教学规划 + LessonPlan 校验
        ↓
第六阶段：真实 OCR / LLM Adapter
        ↓
第七阶段：Redis 队列 + 重试 + 超时
        ↓
第八阶段：API 完善 + 日志 + 指标
        ↓
第九阶段：TTS、安全治理和完整集成测试
```

## 8. 下一里程碑

下一阶段不做前端工作，目标是完成：

> MySQL 持久化、S3 图片存储、真实 OCR、统一题目结构、可验证数学求解和可持久化的 Worker 状态流转。

完成后，后端将具备脱离 Demo Adapter、支撑真实图片上传到讲解结果的能力。
