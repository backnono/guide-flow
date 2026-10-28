package retrieval

import (
	"context"
	"errors"
	"strings"

	solutiondomain "github.com/guide-flow/guide-flow/backend/domain/solution"
)

var (
	ErrTemplateInvalid          = errors.New("solution template is invalid")
	ErrTemplateStoreUnavailable = errors.New("solution template store is unavailable")
)

type Service struct{ store TemplateStore }

func NewService(store TemplateStore) *Service { return &Service{store: store} }

func (s *Service) Create(ctx context.Context, template solutiondomain.Template) error {
	if s.store == nil {
		return ErrTemplateStoreUnavailable
	}
	if strings.TrimSpace(template.ID) == "" || template.Version <= 0 || strings.TrimSpace(template.Name) == "" {
		return ErrTemplateInvalid
	}
	if len(template.Topics) == 0 && len(template.KnowledgePoints) == 0 && strings.TrimSpace(template.Formula) == "" {
		return ErrTemplateInvalid
	}
	return s.store.CreateTemplate(ctx, template)
}

func (s *Service) Publish(ctx context.Context, id string, version int64) error {
	if s.store == nil {
		return ErrTemplateStoreUnavailable
	}
	if strings.TrimSpace(id) == "" || version <= 0 {
		return ErrTemplateInvalid
	}
	return s.store.PublishTemplate(ctx, id, version)
}
