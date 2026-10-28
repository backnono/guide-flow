package mysql

import (
	"context"
	"encoding/json"
	"strings"

	lessondomain "github.com/guide-flow/guide-flow/backend/domain/lesson"
	solutiondomain "github.com/guide-flow/guide-flow/backend/domain/solution"
	"gorm.io/gorm"
)

func (r *LessonRepository) CreateTemplate(ctx context.Context, template solutiondomain.Template) error {
	return r.db.WithContext(ctx).Create(&SolutionTemplateModel{
		ID: template.ID, Version: template.Version, Name: template.Name,
		TopicsJSON: jsonBytes(template.Topics), KnowledgePointsJSON: jsonBytes(template.KnowledgePoints),
		Formula: template.Formula, ApplicabilityJSON: jsonBytes(template.Applicability), VariableSchemaJSON: jsonBytes(template.VariableSchema),
		StepsJSON: jsonBytes(template.Steps), CommonMistakesJSON: jsonBytes(template.CommonMistakes), Active: false, CreatedAt: lessondomain.Now(),
	}).Error
}

func (r *LessonRepository) PublishTemplate(ctx context.Context, id string, version int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var target SolutionTemplateModel
		if err := tx.First(&target, "id = ? AND version = ?", id, version).Error; err != nil {
			return err
		}
		if err := tx.Model(&SolutionTemplateModel{}).Where("id = ?", id).Update("active", false).Error; err != nil {
			return err
		}
		return tx.Model(&SolutionTemplateModel{}).Where("id = ? AND version = ?", id, version).Update("active", true).Error
	})
}

func jsonBytes(value any) []byte {
	data, _ := json.Marshal(value)
	if strings.TrimSpace(string(data)) == "null" {
		return []byte("[]")
	}
	return data
}

func (r *LessonRepository) ListActive(ctx context.Context) ([]solutiondomain.Template, error) {
	var rows []SolutionTemplateModel
	if err := r.db.WithContext(ctx).Where("active = ?", true).Order("id, version DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]solutiondomain.Template, 0, len(rows))
	for _, row := range rows {
		var template solutiondomain.Template
		template.ID, template.Version, template.Name, template.Formula = row.ID, row.Version, row.Name, row.Formula
		if err := decodeOptionalJSON(row.TopicsJSON, &template.Topics); err != nil {
			return nil, err
		}
		if err := decodeOptionalJSON(row.KnowledgePointsJSON, &template.KnowledgePoints); err != nil {
			return nil, err
		}
		if err := decodeOptionalJSON(row.ApplicabilityJSON, &template.Applicability); err != nil {
			return nil, err
		}
		if err := decodeOptionalJSON(row.VariableSchemaJSON, &template.VariableSchema); err != nil {
			return nil, err
		}
		if err := decodeOptionalJSON(row.StepsJSON, &template.Steps); err != nil {
			return nil, err
		}
		if err := decodeOptionalJSON(row.CommonMistakesJSON, &template.CommonMistakes); err != nil {
			return nil, err
		}
		result = append(result, template)
	}
	return result, nil
}

func decodeOptionalJSON(data []byte, target any) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	return json.Unmarshal(data, target)
}
