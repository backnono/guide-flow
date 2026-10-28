# 讲给我听：第一版 Web + Agent 架构

版本：v0.1  
目标：支撑“上传一道题并完成一次可验证的分步讲解”闭环

## 1. 总体选择

第一版采用“单 Web 应用 + Go 模块化后端 + 一个异步 Worker”的结构，不拆微服务。后端采用轻量级整洁架构：HTTP 适配层、Application 用例层、Domain 领域层和 Infra 基础设施层由 Bootstrap 统一装配。Agent Worker 初期也使用 Go 的显式流程编排，不强行引入 LangChain/LangGraph。

原因：当前最大的未知数是识别质量、讲解是否易懂和生成耗时，而不是服务之间的部署规模。模块化单体可以保留清晰的替换接口，同时减少部署、调试和数据一致性成本。

```text
浏览器 Web
    │ HTTPS / 轮询
    ▼
Web API（鉴权、上传、查询、反馈）
    │
    ├── 讲解任务编排器
    │      ├── 题目识别模块 ── OCR Adapter
    │      ├── 题目结构化模块
    │      ├── 解题模块 ────── 数学计算/规则校验
    │      ├── 教学规划模块 ── LLM Adapter
    │      ├── 讲解渲染模块 ── JSON 场景
    │      └── 语音模块 ────── TTS Adapter（可选）
    │
    ├── MySQL：题目、任务、讲解、反馈和状态事件
    ├── Object Storage：原图、缩略图、音频
    └── Redis/队列：异步任务处理与恢复
```

## 2. Agent 不直接返回答案

对外只暴露一个深模块接口：

```text
createLesson(image, learnerProfile) -> LessonJob
getLesson(jobId) -> Lesson
confirmQuestion(jobId, correctedQuestion) -> LessonJob
submitPractice(lessonId, answer) -> PracticeResult
```

Agent 内部负责复杂流程，Web 层不参与提示词拼接、重试、模型选择和解题判断。

## 3. Agent 内部流程

### 阶段 A：识别题目

- 输入：图片地址
- 输出：`QuestionDraft`
- 内容：题干、数字、单位、公式、图形描述、识别置信度
- 低置信度时暂停，要求用户确认，不进入自动讲解

### 阶段 B：结构化题目

将自然语言转换为统一题目模型：

```json
{
  "grade": 5,
  "topic": "fraction_word_problem",
  "knowns": [{"name": "book_pages", "value": 120, "unit": "页"}],
  "relations": ["day_1 = total * 1/3", "day_2 = remaining * 1/4"],
  "asks": ["day_2"]
}
```

### 阶段 C：求解与校验

LLM 可以提出解法，但不能作为唯一计算器。至少需要：

- 结构化算式计算
- 结果代回题目条件
- 单位检查
- 多解法结果一致性检查

校验失败时返回“需要人工确认”，而不是生成看似流畅的错误教程。

### 阶段 D：教学规划

输入结构化题目、已验证解法和年级信息，输出固定格式的 `LessonPlan`：

```json
{
  "title": "先找出剩下的页数",
  "steps": [
    {
      "title": "先找出“剩下的页数”",
      "explanation": "第一天看了全书的 1/3，所以还剩下 2/3。",
      "formula": "1 - 1/3 = 2/3",
      "visual": {"type": "ratio_bar", "segments": [1, 2]},
      "voiceText": "我们先不急着算第二天，先找出剩下的部分。"
    }
  ],
  "finalAnswer": "20 页",
  "practice": {"question": "...", "answer": "12"}
}
```

第一版只支持少量视觉类型：`ratio_bar`、`groups`、`number_line`、`formula_reveal`。这比让模型直接生成视频更容易验证、缓存和修正。

## 4. 推荐的后端模块

```text
backend/
  lesson/                 # 用例：创建、确认、查询讲解
  question/               # 题目领域模型与结构化
  solving/                # 解法候选、计算、校验
  pedagogy/               # 年级约束、语言风格、步骤规划
  rendering/              # LessonPlan 转前端场景
  adapters/
    ocr/
    llm/
    tts/
    storage/
  jobs/                   # 状态机、队列、重试、超时
```

模块通过小接口连接；例如 `OcrAdapter`、`Solver`、`LessonPlanner` 都可以在测试中替换成固定实现。第一版只有一个真实 Adapter 时，也先保留接口，不把接口暴露给前端。

## 5. 任务状态机

```text
created → recognizing → awaiting_confirmation → structuring
                                      ↓              ↓
                                  rejected        failed
structuring → solving → planning → rendering → ready
                    ↓        ↓          ↓
                  failed    failed     failed
```

建议所有状态变化持久化。前端使用轮询获取进度；第一版不优先实现 SSE，后续根据任务量和实时性需求评估。

## 6. API 草案

```text
POST /api/lessons
  multipart image
  -> { lessonId, status }

GET /api/lessons/:id
  -> { status, questionDraft, lessonPlan, error }

POST /api/lessons/:id/question
  { text: string }
  -> { status: "planning" }

POST /api/lessons/:id/practice
  { answer: string }
  -> { correct, feedback, expectedAnswer }
```

## 7. 第一版建议技术栈

- Web：当前原型可迁移到 React/Next.js；讲解页只消费 `LessonPlan` JSON
- 后端 API：Go，负责用户请求、权限、上传、任务状态和领域数据
- Agent Worker：Go，负责固定步骤编排和外部模型 Adapter 调用
- 数据库：MySQL（InnoDB）
- 文件：S3 兼容对象存储
- 队列：Redis + 简单 worker
- 模型：OCR、LLM、TTS 都通过 Adapter 隔离
- 部署：一个 Web/API 实例 + 一个 Worker + MySQL + Redis

### Agent 框架决策

第一版流程是固定的业务流水线：OCR → 结构化 → 解题校验 → 教学规划 → 场景渲染。它不是需要自主决定工具和路径的开放式 Agent，因此直接用 Go 编排器、状态机和 Adapter 更容易测试、追踪和控制成本。

当流程出现以下需求时，再考虑引入 LangGraph：多轮工具调用、复杂条件分支、长时间运行、暂停后由用户修改状态并恢复、需要按节点回放调试。届时可以把 LangGraph 放到独立的 Python/Node Worker 中，通过内部 HTTP 或 gRPC 接入，Go 仍保留业务主入口。

## 8. 不建议第一版做的事

- 不让前端直接调用模型供应商
- 不让一个大 Prompt 同时负责 OCR、算题、教学和视频生成
- 不在数据库中保存模型原始长文本作为唯一结果
- 不以搜索结果直接作为答案；搜索最多作为知识参考，最终解法必须经过结构化计算校验
- 不先建设完整视频渲染平台；先用结构化视觉场景验证教学效果

## 9. 成功指标

- 识别后需要用户修改的比例
- 解题校验失败率
- 从上传到可播放讲解的平均耗时
- 长辈是否能依据讲解复述步骤
- 孩子相似题首次答对率
- “听不懂/换一种讲法”的重复请求率
