# 讲给我听 · MVP

这是一个“讲给我听”第一版闭环：浏览器原型 + Go 模块化后端基础结构，用于验证小学数学家庭辅导。

直接打开 `index.html` 即可体验前端示例。后端接口层使用 Gin，业务层和服务启动仍基于 Go 标准库：

```bash
go run ./cmd/server
```

服务默认监听 `http://localhost:8080`，提供：

- `POST /api/lessons`：上传 multipart 字段 `image`，创建异步讲解任务
- `GET /api/lessons/:id`：查询任务状态与结构化讲解
- `POST /api/lessons/:id/question`：低置信度识别后的题目确认
- `POST /api/lessons/:id/question/confirm`：确认或修改题目，并生成新的题目版本
- `POST /api/lessons/:id/question/reject`：拒绝当前题目
- `POST /api/lessons/:id/retry`：重试可恢复失败的任务
- `POST /api/lessons/:id/practice`：提交相似题答案
- `GET /healthz`：健康检查

所有 API 响应都会返回 `X-Request-ID`；客户端可传入同名请求头以贯穿一次请求链路。错误响应统一包含 `code`、`message`、`requestId`、`lessonId` 和 `retryable`，服务端内部错误不会直接暴露数据库或外部供应商细节。创建 Lesson 除了 `grade` 外，也支持 multipart 字段 `learnerProfile`，值为 JSON，例如 `{"grade":6}`。任务状态继续通过 `GET /api/lessons/:id` 轮询，使用 `status`、`stage`、`progress` 和 `nextAction` 判断下一步。

当前默认使用 Demo OCR、Demo Solver 和 Demo Renderer，方便先跑通流程；不提供数据库或对象存储配置时会回退到内存实现。任务状态现在通过统一状态机推进，并记录状态事件、题目版本、失败阶段和练习结果；查询响应包含 `stage`、`progress` 和 `nextAction`。数据库初始化和持久化实现位于 `backend/infra/persistence/mysql`，使用 GORM 管理 ORM Model 和事务；`internal/bootstrap` 负责依赖装配。服务配置由根目录 [`config.yaml`](config.yaml) 提供，并由 Viper 加载。LessonPlan 约束见 [`docs/lesson-plan.schema.json`](docs/lesson-plan.schema.json)，完整实施计划见 [`docs/BACKEND-IMPLEMENTATION-PLAN-v1.md`](docs/BACKEND-IMPLEMENTATION-PLAN-v1.md)。

### Phase 1：DeepSeek Agent 公网检索

研究阶段使用 DeepSeek 的 tool calling 编排 `web_search`，再调用 DeepSeek 自研 Search API 访问公网。模型和搜索默认共用 `APP_LLM_API_KEY`；也可以单独设置 `APP_SEARCH_API_KEY`。密钥缺失时会明确记录 `research_unavailable`，不会把空结果当作搜索成功。

```bash
APP_LLM_PROVIDER=deepseek \
APP_LLM_API_KEY=你的DeepSeekKey \
APP_LLM_MODEL=deepseek-chat \
APP_SEARCH_PROVIDER=deepseek \
go run ./cmd/server
```

`APP_SEARCH_ENDPOINT` 默认为 `https://api.deepseek.com/v1/tools/search`，`APP_SEARCH_MAX_TOOL_CALLS` 默认为 3。一次研究请求的第一次 DeepSeek 调用会强制要求 `web_search`，后续最多继续调用配置数量的工具；搜索结果经过本地归一化和 `ResearchContext` 校验后，才会进入教程阶段。当前实现文件为 [`backend/infra/research/deepseek_web_search.go`](backend/infra/research/deepseek_web_search.go)。

接口层按模块拆分：`backend/api/http` 负责 Gin 装配，`backend/api/http/controller` 负责模块路由和接口处理，`backend/api/http/request` 负责请求 DTO，`backend/api/http/response` 负责响应 DTO。领域模型位于 `backend/domain/lesson`，用例编排位于 `backend/application/lesson`，基础设施位于 `backend/infra`，依赖装配位于 `internal/bootstrap`。新增模块时应新增对应 Controller、Request、Response、Application 和 Domain 契约，并在 Router/Bootstrap 中完成注册，避免把多个业务模块堆到同一个 Handler 文件中。

配置 `database.mysql_dsn`（或环境变量 `APP_DATABASE_MYSQL_DSN`）后，服务启动时会由 `internal/bootstrap` 调用 `backend/infra/persistence/mysql` 初始化 GORM，然后使用 GORM Lesson Repository。HTTP 地址可通过 `server.http_addr` 或 `APP_SERVER_HTTP_ADDR` 配置；也可以用 `APP_CONFIG_FILE` 指定配置文件路径。数据库初始化脚本作为文档资产保存在 [`docs/database/001_initial.sql`](docs/database/001_initial.sql)；当前 `AutoMigrate` 作为本地开发兜底。未配置时继续使用内存 Repository。GORM Repository 使用事务保存 Lesson 主记录、题目版本、LessonPlan、练习结果和状态事件，并通过 `revision` 做乐观并发控制。配置 `MYSQL_GORM_TEST_DSN` 可执行 GORM Repository 集成测试。

配置 `object_store.endpoint`、`bucket`、`access_key` 和 `secret_key` 后，服务会使用 S3 兼容对象存储；未配置时继续使用内存图片存储。S3 存储会校验 JPEG/PNG、10 MiB 大小上限和图片可读性，按 Lesson ID 保存原图与 512 像素以内的缩略图，并在 `lessons.image_object_key`、`lessons.thumbnail_object_key` 中保存稳定对象键。数据库不保存临时签名 URL，因为 URL 会过期；需要展示图片时再由对象存储按对象键生成临时访问地址。数据库或队列写入失败时会删除孤儿对象。`use_path_style: true` 适用于本地 MinIO 等服务。设置 `S3_TEST_ENDPOINT` 可执行对象存储集成测试。

### 阶段 6：真实 OCR 配置

`ocr.provider` 默认为 `demo`。切换为 `http` 后，服务会向 `ocr.endpoint` 发送 multipart 字段 `image`，并附带 `X-Request-ID`；可选使用 `Authorization: Bearer <api_key>`。OCR 服务需要返回如下 JSON：

阿里云题目识别使用官方 Go SDK。将 `ocr.provider` 设置为 `aliyun`，并通过环境变量注入 AccessKey：

```bash
APP_OCR_PROVIDER=aliyun \
APP_OCR_ACCESS_KEY_ID=你的AccessKeyId \
APP_OCR_ACCESS_KEY_SECRET=你的AccessKeySecret \
go run ./cmd/server
```

默认调用 `ocr-api.cn-hangzhou.aliyuncs.com` 的 `RecognizeEduQuestionOcr`，适合单题图片、印刷体文字和公式；也可以通过 `APP_OCR_ENDPOINT` 覆盖接入点。阿里云账号需要开通教育场景识别并给 RAM 用户授予 `AliyunOCRFullAccess`。

```json
{"text":"题目文本","confidence":0.95,"fieldConfidence":{"numbers":0.98,"units":0.92,"ask":0.88}}
```

HTTP OCR 对超时、网络错误、429 和 5xx 按配置重试；4xx 和格式错误不会盲目重试。原始响应保存于数据库 `ocr_traces`，不会作为前端唯一数据来源。整题置信度或关键字段（数字、分数、单位、公式、问题目标、图形描述）低于 `0.8` 时进入 `awaiting_confirmation`；用户确认/修改后只生成新的 `QuestionVersion`，不会再次调用 OCR。

示例：

```bash
APP_OCR_PROVIDER=http \
APP_OCR_ENDPOINT=http://127.0.0.1:8090/ocr \
APP_OCR_TIMEOUT_SECS=30 \
APP_OCR_MAX_RETRIES=2 \
go run ./cmd/server
```

时间约定：当前产品的数据库 `DATETIME(3)` 字段统一按 `Asia/Shanghai` 业务时间写入和读取；HTTP 响应格式为 `YYYY-MM-DD HH:mm:ss`，不返回 RFC3339 时区后缀。已有数据库请先执行 [`docs/database/002_local_timezone_and_image_refs.sql`](docs/database/002_local_timezone_and_image_refs.sql) 一次，将旧 UTC 数据转换为东八区并回填 MinIO 对象键。

## 启动本地 MySQL

项目提供 [`docker-compose.yml`](docker-compose.yml)，会启动 MySQL 8.4、本地 MinIO 和 Redis 7，并在数据卷首次创建时自动执行 [`docs/database/001_initial.sql`](docs/database/001_initial.sql)。宿主机映射端口为 `3308`，用于避开已占用的 `3307` 和 `13306`；容器内部仍使用 `3306`。MinIO 的 S3 API 使用 `9000`，管理控制台使用 `9001`，初始化容器会自动创建 `guide-flow` Bucket；Redis 使用 `6379`，通过 Redis Streams 保存异步任务。

```bash
docker compose up -d mysql minio minio-init redis
go run ./cmd/server
```

默认连接信息为：数据库 `guide_flow`，用户 `app`，密码 `app`。MinIO 控制台地址为 `http://localhost:9001`，用户名和密码均为 `minioadmin`。Redis 任务流默认为 `guide-flow:lessons`，消费者组为 `guide-flow-workers`。如果需要重新执行初始化 SQL 或清空本地 MySQL、MinIO、Redis 数据，需要先停止服务并删除项目专用数据卷：

```bash
docker compose down -v
docker compose up -d mysql
```

## 生成 Agent Context

项目内置了一个只读 Context Compiler，用于把任务、项目规则、变更材料和工作区状态编译成 Markdown Context Pack，并可同时生成 JSON manifest。

```bash
go run ./cmd/context \
  --task "接入真实 OCR Adapter" \
  --phase propose \
  --output /tmp/guide-flow-context.md \
  --manifest /tmp/guide-flow-context.json
```

阶段支持 `propose`、`apply`、`review`。指定 `--task-id CHANGE-001` 后，会按阶段读取 `.agent-context/changes/CHANGE-001/` 下的 `spec.md`、`tasks.md`、`log.md` 和 Review 清单。知识文件通过重复指定 `--knowledge` 显式关联；默认 Context 大小为 32 KiB，可用 `--max-bytes` 调整。
