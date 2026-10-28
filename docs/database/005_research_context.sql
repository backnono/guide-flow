-- Guide Flow：阶段二按需检索上下文。

DELIMITER $$

DROP PROCEDURE IF EXISTS guide_flow_migrate_005_research_context$$
CREATE PROCEDURE guide_flow_migrate_005_research_context()
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = DATABASE() AND table_name = 'lessons' AND column_name = 'research_context_json'
    ) THEN
        ALTER TABLE lessons ADD COLUMN research_context_json JSON NULL COMMENT '本题检索上下文及检索状态';
    END IF;
END$$

CALL guide_flow_migrate_005_research_context()$$
DROP PROCEDURE guide_flow_migrate_005_research_context$$

DELIMITER ;
