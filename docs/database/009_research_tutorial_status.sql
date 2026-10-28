-- Guide Flow：检索与教程阶段独立保存，避免教程失败覆盖已找到的参考解答。

DELIMITER $$

DROP PROCEDURE IF EXISTS guide_flow_migrate_009_research_tutorial_status$$
CREATE PROCEDURE guide_flow_migrate_009_research_tutorial_status()
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = DATABASE() AND table_name = 'lessons' AND column_name = 'research_status'
    ) THEN
        ALTER TABLE lessons ADD COLUMN research_status VARCHAR(32) NOT NULL DEFAULT 'not_started' COMMENT '题目检索阶段状态：not_started、searching、unavailable、found、verified、conflict';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = DATABASE() AND table_name = 'lessons' AND column_name = 'tutorial_status'
    ) THEN
        ALTER TABLE lessons ADD COLUMN tutorial_status VARCHAR(32) NOT NULL DEFAULT 'not_started' COMMENT '个性化教程阶段状态：not_started、generating、failed、ready';
    END IF;
END$$

CALL guide_flow_migrate_009_research_tutorial_status()$$
DROP PROCEDURE guide_flow_migrate_009_research_tutorial_status$$

DELIMITER ;
