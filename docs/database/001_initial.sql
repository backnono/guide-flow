-- Guide Flow 阶段二：Lesson 持久化基础表
-- 生产环境使用版本化 SQL 迁移；本地未配置数据库时由 GORM AutoMigrate 兜底。

CREATE TABLE IF NOT EXISTS lessons (
    id VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL,
    learner_profile_json JSON NOT NULL,
    image_name VARCHAR(255) NOT NULL,
    image_object_key VARCHAR(512) NULL,
    thumbnail_object_key VARCHAR(512) NULL,
    question_draft_json JSON NULL,
    question_analysis_json JSON NULL COMMENT '多模态模型输出的结构化题目分析结果',
    research_context_json JSON NULL COMMENT '本题检索上下文及检索状态',
    solution_draft_json JSON NULL COMMENT 'LLM 生成的未校验解题草稿',
    solution_trace_json JSON NULL COMMENT 'LLM 模型、Prompt 和请求追踪信息',
    current_question_version BIGINT NOT NULL DEFAULT 0,
    question_json JSON NULL,
    error TEXT NULL,
    error_code VARCHAR(64) NULL,
    failure_kind VARCHAR(32) NULL,
    failed_from VARCHAR(32) NULL,
    retry_count INT NOT NULL DEFAULT 0,
    last_error_at DATETIME(3) NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    PRIMARY KEY (id),
    KEY idx_lessons_status_updated (status, updated_at)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS question_versions (
    lesson_id VARCHAR(64) NOT NULL,
    version_id BIGINT NOT NULL,
    text TEXT NOT NULL,
    source VARCHAR(32) NOT NULL,
    confidence DECIMAL(6,5) NOT NULL,
    created_at DATETIME(3) NOT NULL,
    PRIMARY KEY (lesson_id, version_id),
    CONSTRAINT fk_question_versions_lesson FOREIGN KEY (lesson_id) REFERENCES lessons(id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS ocr_traces (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT 'OCR 追踪记录主键，区分同一 Lesson 的不同尝试',
    lesson_id VARCHAR(64) NOT NULL,
    provider VARCHAR(64) NOT NULL,
    request_id VARCHAR(128) NULL,
    prompt_version VARCHAR(64) NULL COMMENT '视觉解析 Prompt 版本',
    schema_version VARCHAR(64) NULL COMMENT '领域结构协议版本',
    raw_response LONGTEXT NOT NULL,
    finish_reason VARCHAR(32) NULL COMMENT '模型结束原因，例如 stop 或 length',
    parse_error TEXT NULL COMMENT '结构解析或业务校验错误',
    parse_error_path VARCHAR(255) NULL COMMENT '解析失败字段路径',
    duration_ms BIGINT NOT NULL DEFAULT 0 COMMENT '本次供应商请求耗时（毫秒）',
    created_at DATETIME(3) NOT NULL,
    PRIMARY KEY (id),
    KEY idx_ocr_traces_lesson_created (lesson_id, created_at),
    CONSTRAINT fk_ocr_traces_lesson FOREIGN KEY (lesson_id) REFERENCES lessons(id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS lesson_plans (
    lesson_id VARCHAR(64) NOT NULL,
    plan_json JSON NOT NULL,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    PRIMARY KEY (lesson_id),
    CONSTRAINT fk_lesson_plans_lesson FOREIGN KEY (lesson_id) REFERENCES lessons(id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS practice_results (
    lesson_id VARCHAR(64) NOT NULL,
    attempt INT NOT NULL,
    answer TEXT NOT NULL,
    correct BOOLEAN NOT NULL,
    feedback TEXT NOT NULL,
    expected_answer TEXT NOT NULL,
    created_at DATETIME(3) NOT NULL,
    PRIMARY KEY (lesson_id, attempt),
    CONSTRAINT fk_practice_results_lesson FOREIGN KEY (lesson_id) REFERENCES lessons(id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS lesson_status_events (
    lesson_id VARCHAR(64) NOT NULL,
    revision BIGINT NOT NULL,
    from_status VARCHAR(32) NULL,
    to_status VARCHAR(32) NOT NULL,
    stage VARCHAR(32) NOT NULL,
    error_code VARCHAR(64) NULL,
    created_at DATETIME(3) NOT NULL,
    PRIMARY KEY (lesson_id, revision),
    KEY idx_status_events_lesson_created (lesson_id, created_at),
    CONSTRAINT fk_status_events_lesson FOREIGN KEY (lesson_id) REFERENCES lessons(id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS job_attempts (
    id BIGINT NOT NULL AUTO_INCREMENT,
    lesson_id VARCHAR(64) NOT NULL,
    job_id VARCHAR(128) NOT NULL,
    stage VARCHAR(32) NOT NULL,
    attempt INT NOT NULL,
    status VARCHAR(32) NOT NULL,
    error_code VARCHAR(64) NULL,
    started_at DATETIME(3) NULL,
    finished_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_job_attempt (job_id, attempt),
    KEY idx_job_attempts_lesson (lesson_id, created_at),
    CONSTRAINT fk_job_attempts_lesson FOREIGN KEY (lesson_id) REFERENCES lessons(id) ON DELETE CASCADE
) ENGINE=InnoDB;
