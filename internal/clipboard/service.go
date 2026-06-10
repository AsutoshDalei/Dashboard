package clipboard

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) List(ctx context.Context) ([]Item, error) {
	return s.repo.List(ctx)
}

func (s *Service) Create(ctx context.Context, label, content, category string) (*Item, error) {
	item := Item{
		ID:        uuid.New().String(),
		Label:     label,
		Content:   content,
		Category:  category,
		CreatedAt: time.Now(),
	}
	if err := s.repo.Create(ctx, item); err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

func (s *Service) Search(ctx context.Context, query string) ([]Item, error) {
	return s.repo.Search(ctx, query)
}