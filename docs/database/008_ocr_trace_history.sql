-- OCR 追踪需要保留同一 Lesson 的多次识别尝试，不能再以 lesson_id 作为唯一主键覆盖历史记录。
-- 执行前请确认数据库已完成 001_initial.sql；本迁移只调整 ocr_traces 的主键和索引，不删除已有原始响应。
ALTER TABLE ocr_traces
    ADD COLUMN id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT UNIQUE COMMENT 'OCR 追踪记录主键，区分同一 Lesson 的不同尝试';

ALTER TABLE ocr_traces
    DROP PRIMARY KEY,
    ADD PRIMARY KEY (id),
    ADD KEY idx_ocr_traces_lesson_created (lesson_id, created_at);
