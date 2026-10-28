-- Guide Flow：阶段三 LLM 解题草稿及模型追踪信息。

DELIMITER $$

DROP PROCEDURE IF EXISTS guide_flow_migrate_006_solution_draft$$
CREATE PROCEDURE guide_flow_migrate_006_solution_draft()
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = DATABASE() AND table_name = 'lessons' AND column_name = 'solution_draft_json'
    ) THEN
        ALTER TABLE lessons ADD COLUMN solution_draft_json JSON NULL COMMENT 'LLM 生成的未校验解题草稿';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = DATABASE() AND table_name = 'lessons' AND column_name = 'solution_trace_json'
    ) THEN
        ALTER TABLE lessons ADD COLUMN solution_trace_json JSON NULL COMMENT 'LLM 模型、Prompt 和请求追踪信息';
    END IF;
END$$

CALL guide_flow_migrate_006_solution_draft()$$
DROP PROCEDURE guide_flow_migrate_006_solution_draft$$

DELIMITER ;
