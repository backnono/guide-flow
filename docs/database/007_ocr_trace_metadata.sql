-- Guide Flow：补充视觉结构化解析的协议、截断和失败路径元数据。

DELIMITER $$

DROP PROCEDURE IF EXISTS guide_flow_migrate_007_ocr_trace_metadata$$
CREATE PROCEDURE guide_flow_migrate_007_ocr_trace_metadata()
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'ocr_traces' AND column_name = 'prompt_version') THEN
        ALTER TABLE ocr_traces ADD COLUMN prompt_version VARCHAR(64) NULL COMMENT '视觉解析 Prompt 版本';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'ocr_traces' AND column_name = 'schema_version') THEN
        ALTER TABLE ocr_traces ADD COLUMN schema_version VARCHAR(64) NULL COMMENT '领域结构协议版本';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'ocr_traces' AND column_name = 'finish_reason') THEN
        ALTER TABLE ocr_traces ADD COLUMN finish_reason VARCHAR(32) NULL COMMENT '模型结束原因，例如 stop 或 length';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'ocr_traces' AND column_name = 'parse_error') THEN
        ALTER TABLE ocr_traces ADD COLUMN parse_error TEXT NULL COMMENT '结构解析或业务校验错误';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'ocr_traces' AND column_name = 'parse_error_path') THEN
        ALTER TABLE ocr_traces ADD COLUMN parse_error_path VARCHAR(255) NULL COMMENT '解析失败字段路径';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'ocr_traces' AND column_name = 'duration_ms') THEN
        ALTER TABLE ocr_traces ADD COLUMN duration_ms BIGINT NOT NULL DEFAULT 0 COMMENT '本次供应商请求耗时（毫秒）';
    END IF;
END$$

CALL guide_flow_migrate_007_ocr_trace_metadata()$$
DROP PROCEDURE guide_flow_migrate_007_ocr_trace_metadata$$

DELIMITER ;
