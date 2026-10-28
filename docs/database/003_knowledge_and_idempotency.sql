-- Guide Flow：P0 题目/解法绑定与持久化幂等键。
-- 该迁移可能在 AutoMigrate 或部分执行失败后再次运行，因此所有新增字段和索引都做存在性检查。

DELIMITER $$

DROP PROCEDURE IF EXISTS guide_flow_migrate_003_lessons$$
CREATE PROCEDURE guide_flow_migrate_003_lessons()
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = DATABASE() AND table_name = 'lessons' AND column_name = 'idempotency_key'
    ) THEN
        ALTER TABLE lessons ADD COLUMN idempotency_key VARCHAR(128) NULL COMMENT '创建请求幂等键，防止重试产生重复 Lesson';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = DATABASE() AND table_name = 'lessons' AND column_name = 'question_id'
    ) THEN
        ALTER TABLE lessons ADD COLUMN question_id VARCHAR(64) NULL COMMENT '归属的题目知识资产 ID';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = DATABASE() AND table_name = 'lessons' AND column_name = 'question_version_id'
    ) THEN
        ALTER TABLE lessons ADD COLUMN question_version_id BIGINT NOT NULL DEFAULT 0 COMMENT '本次讲解使用的题目版本';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = DATABASE() AND table_name = 'lessons' AND column_name = 'solution_candidates_json'
    ) THEN
        ALTER TABLE lessons ADD COLUMN solution_candidates_json JSON NULL COMMENT '检索候选及评分';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = DATABASE() AND table_name = 'lessons' AND column_name = 'verified_solution_json'
    ) THEN
        ALTER TABLE lessons ADD COLUMN verified_solution_json JSON NULL COMMENT '经过校验的当前题目解法结果';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.statistics
        WHERE table_schema = DATABASE() AND table_name = 'lessons' AND index_name = 'uk_lessons_idempotency_key'
    ) THEN
        CREATE UNIQUE INDEX uk_lessons_idempotency_key ON lessons (idempotency_key);
    END IF;
END$$

CALL guide_flow_migrate_003_lessons()$$
DROP PROCEDURE guide_flow_migrate_003_lessons$$

DELIMITER ;

CREATE TABLE IF NOT EXISTS outbox_events (
    id BIGINT NOT NULL AUTO_INCREMENT COMMENT 'Outbox 事件主键',
    lesson_id VARCHAR(64) NOT NULL COMMENT '关联的 Lesson ID',
    event_type VARCHAR(64) NOT NULL COMMENT '事件类型',
    status VARCHAR(16) NOT NULL COMMENT '投递状态：pending 或 published',
    created_at DATETIME(3) NOT NULL COMMENT '事件创建时间',
    published_at DATETIME(3) NULL COMMENT '成功投递时间',
    PRIMARY KEY (id),
    KEY idx_outbox_pending (status, id),
    CONSTRAINT fk_outbox_lesson FOREIGN KEY (lesson_id) REFERENCES lessons(id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS solution_templates (
    id VARCHAR(64) NOT NULL COMMENT '解法模板稳定 ID',
    version BIGINT NOT NULL COMMENT '模板不可变版本号',
    name VARCHAR(255) NOT NULL COMMENT '模板名称',
    topics_json JSON NULL COMMENT '适用题型列表',
    knowledge_points_json JSON NULL COMMENT '适用知识点列表',
    formula TEXT NULL COMMENT '结构化公式表达式',
    applicability_json JSON NULL COMMENT '适用条件',
    variable_schema_json JSON NULL COMMENT '参数变量定义',
    steps_json JSON NULL COMMENT '解法步骤模板',
    common_mistakes_json JSON NULL COMMENT '常见错误说明',
    active BOOLEAN NOT NULL DEFAULT TRUE COMMENT '是否参与检索',
    created_at DATETIME(3) NOT NULL COMMENT '创建时间',
    PRIMARY KEY (id, version),
    KEY idx_solution_templates_active (active, id, version)
) ENGINE=InnoDB;
