package mysql

import (
	"fmt"

	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// Open 初始化 GORM 数据库连接；连接细节留在 infra，业务层只接触 Repository 接口。
func Open(dsn string) (*gorm.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("mysql dsn is empty")
	}
	db, err := gorm.Open(gormmysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open mysql with gorm: %w", err)
	}
	return db, nil
}

// AutoMigrate 只负责开发阶段和 MVP 的表结构同步；生产环境仍建议使用版本化 SQL 迁移。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&LessonModel{},
		&QuestionVersionModel{},
		&OCRTraceModel{},
		&LessonPlanModel{},
		&PracticeResultModel{},
		&StatusEventModel{},
		&JobAttemptModel{},
		&OutboxEventModel{},
		&SolutionTemplateModel{},
	)
}
