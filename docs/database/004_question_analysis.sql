-- Guide Flow：阶段一多模态题目结构化分析结果。
-- 该字段与 OCR 草稿并存，保留模型原始识别和后续结构化结果的边界。

DELIMITER $$

DROP PROCEDURE IF EXISTS guide_flow_migrate_004_question_analysis$$
CREATE PROCEDURE guide_flow_migrate_004_question_analysis()
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = DATABASE() AND table_name = 'lessons' AND column_name = 'question_analysis_json'
    ) THEN
        ALTER TABLE lessons ADD COLUMN question_analysis_json JSON NULL COMMENT '多模态模型输出的结构化题目分析结果';
    END IF;
END$$

CALL guide_flow_migrate_004_question_analysis()$$
DROP PROCEDURE guide_flow_migrate_004_question_analysis$$

DELIMITER ;
