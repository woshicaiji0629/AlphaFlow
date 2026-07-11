package service

import (
	"alphaflow/go-service/control-api/internal/domain"
	"alphaflow/go-service/control-api/internal/repository"
	"context"
	"fmt"
	"github.com/google/uuid"
)

type StrategyCatalogService struct {
	repo repository.StrategyCatalogRepository
}

func NewStrategyCatalogService(repo repository.StrategyCatalogRepository) *StrategyCatalogService {
	return &StrategyCatalogService{repo: repo}
}
func (s *StrategyCatalogService) List(ctx context.Context, user domain.User) ([]domain.PublishedStrategy, error) {
	items, err := s.repo.ListVisibleStrategies(ctx, user.ID, user.Role == "admin")
	if err != nil {
		return nil, fmt.Errorf("list visible strategies: %w", err)
	}
	return items, nil
}
func (s *StrategyCatalogService) Performance(ctx context.Context, user domain.User, strategyID uuid.UUID) ([]domain.StrategyPerformance, error) {
	visible, err := s.List(ctx, user)
	if err != nil {
		return nil, err
	}
	allowed := false
	for _, item := range visible {
		if item.ID == strategyID {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, domain.ErrForbidden
	}
	items, err := s.repo.ListPerformance(ctx, strategyID)
	if err != nil {
		return nil, fmt.Errorf("list strategy performance: %w", err)
	}
	return items, nil
}
