-- Guide Flow：统一业务时区并显式保存 MinIO 对象键。
-- 执行前请确认当前 lessons 等时间字段仍是按 UTC 写入的旧数据；本迁移只应执行一次。

ALTER TABLE lessons
    ADD COLUMN image_object_key VARCHAR(512) NULL AFTER image_name,
    ADD COLUMN thumbnail_object_key VARCHAR(512) NULL AFTER image_object_key;

-- 旧版本只保存 image_name，按当前 object_store.key_prefix=guide-flow 回填稳定对象键。
UPDATE lessons
SET image_object_key = CONCAT('guide-flow/lessons/', id, '/original.bin'),
    thumbnail_object_key = CONCAT('guide-flow/lessons/', id, '/thumbnail.jpg')
WHERE image_object_key IS NULL OR thumbnail_object_key IS NULL;

-- DATETIME 没有时区信息，旧数据按 UTC 写入；统一转换为 Asia/Shanghai 业务时间。
UPDATE lessons SET created_at = DATE_ADD(created_at, INTERVAL 8 HOUR), updated_at = DATE_ADD(updated_at, INTERVAL 8 HOUR), last_error_at = IF(last_error_at IS NULL, NULL, DATE_ADD(last_error_at, INTERVAL 8 HOUR));
UPDATE question_versions SET created_at = DATE_ADD(created_at, INTERVAL 8 HOUR);
UPDATE ocr_traces SET created_at = DATE_ADD(created_at, INTERVAL 8 HOUR);
UPDATE lesson_plans SET created_at = DATE_ADD(created_at, INTERVAL 8 HOUR), updated_at = DATE_ADD(updated_at, INTERVAL 8 HOUR);
UPDATE practice_results SET created_at = DATE_ADD(created_at, INTERVAL 8 HOUR);
UPDATE lesson_status_events SET created_at = DATE_ADD(created_at, INTERVAL 8 HOUR);
UPDATE job_attempts SET started_at = IF(started_at IS NULL, NULL, DATE_ADD(started_at, INTERVAL 8 HOUR)), finished_at = IF(finished_at IS NULL, NULL, DATE_ADD(finished_at, INTERVAL 8 HOUR)), created_at = DATE_ADD(created_at, INTERVAL 8 HOUR);
