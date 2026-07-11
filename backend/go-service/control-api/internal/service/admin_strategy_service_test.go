package service

import (
	"context"
	"testing"

	"alphaflow/go-service/control-api/internal/domain"
	"github.com/google/uuid"
)

type adminStrategyRepository struct {
	items   []domain.AdminStrategy
	created domain.AdminStrategy
	audit   domain.AuditEvent
}

func (r *adminStrategyRepository) ListAdminStrategies(context.Context) ([]domain.AdminStrategy, error) {
	return r.items, nil
}

func (r *adminStrategyRepository) FindAdminStrategy(_ context.Context, id uuid.UUID) (domain.AdminStrategy, error) {
	for _, item := range r.items {
		if item.ID == id {
			return item, nil
		}
	}
	return domain.AdminStrategy{}, domain.ErrStrategyNotFound
}

func (r *adminStrategyRepository) CreateAdminStrategy(_ context.Context, strategy domain.AdminStrategy, audit domain.AuditEvent) (domain.AdminStrategy, error) {
	r.created = strategy
	r.audit = audit
	return strategy, nil
}

func (r *adminStrategyRepository) UpdateAdminStrategy(_ context.Context, strategy domain.AdminStrategy, audit domain.AuditEvent) (domain.AdminStrategy, error) {
	r.created = strategy
	r.audit = audit
	return strategy, nil
}

func TestValidateAdminStrategy(t *testing.T) {
	valid := AdminStrategyInput{Code: "supertrend", Name: "Supertrend", Version: "1", RiskLevel: "high", Visibility: "public", Status: "published", PaperEnabled: true, Parameters: map[string]string{"entry_threshold": "0.8"}}
	if err := ValidateAdminStrategy(valid); err != nil {
		t.Fatal(err)
	}
	valid.Code = "unknown"
	if err := ValidateAdminStrategy(valid); err == nil {
		t.Fatal("expected unsupported strategy error")
	}
}
func TestValidateAdminStrategyRejectsLive(t *testing.T) {
	input := AdminStrategyInput{Code: "supertrend", Name: "x", Version: "1", RiskLevel: "high", Visibility: "public", Status: "draft", PaperEnabled: true, LiveEnabled: true}
	if err := ValidateAdminStrategy(input); err == nil {
		t.Fatal("expected live rejection")
	}
}

func TestAdminStrategyServiceCreatesDraft(t *testing.T) {
	repository := &adminStrategyRepository{}
	service := NewAdminStrategyService(repository)

	created, err := service.CreateDraft(context.Background(), AdminStrategyInput{
		Code:         " SuperTrend ",
		Name:         " Supertrend ",
		Description:  " Trend strategy ",
		Version:      " 1 ",
		Parameters:   map[string]string{"entry_threshold": "0.8"},
		RiskLevel:    "high",
		Visibility:   "restricted",
		Status:       "published",
		PaperEnabled: true,
		LiveEnabled:  true,
	}, domain.AuditEvent{EventType: "admin.strategy.create"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID.String() == "00000000-0000-0000-0000-000000000000" {
		t.Fatal("expected generated strategy id")
	}
	if created.Code != "supertrend" || created.Name != "Supertrend" || created.Version != "1" {
		t.Fatalf("expected normalized fields, got %#v", created)
	}
	if created.Status != "draft" || created.LiveEnabled {
		t.Fatalf("expected safe draft, got %#v", created)
	}
	if repository.created.ID != created.ID {
		t.Fatal("expected strategy to be persisted")
	}
	if repository.audit.Subject != created.ID.String() || repository.audit.EventType != "admin.strategy.create" {
		t.Fatalf("unexpected audit event: %#v", repository.audit)
	}
}

func TestAdminStrategyServiceListsStrategies(t *testing.T) {
	repository := &adminStrategyRepository{items: []domain.AdminStrategy{{Name: "Supertrend"}}}
	service := NewAdminStrategyService(repository)

	items, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "Supertrend" {
		t.Fatalf("unexpected strategies: %#v", items)
	}
}

func TestAdminStrategyServiceCreatesVersionFromPublishedStrategy(t *testing.T) {
	source := domain.AdminStrategy{ID: uuid.New(), Code: "supertrend", Name: "Supertrend", Version: "1", Parameters: map[string]string{"entry_threshold": "0.8"}, Status: "published", Visibility: "restricted", RiskLevel: "high", PaperEnabled: true}
	repository := &adminStrategyRepository{items: []domain.AdminStrategy{source}}
	service := NewAdminStrategyService(repository)

	created, err := service.CreateVersion(context.Background(), source.ID, " 2 ", domain.AuditEvent{EventType: "admin.strategy.version.create"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == source.ID || created.Code != source.Code || created.Version != "2" || created.Status != "draft" {
		t.Fatalf("unexpected version: %#v", created)
	}
	if repository.audit.Subject != created.ID.String() {
		t.Fatalf("unexpected audit subject: %q", repository.audit.Subject)
	}
}

func TestAdminStrategyServiceRejectsVersionFromDraft(t *testing.T) {
	source := domain.AdminStrategy{ID: uuid.New(), Status: "draft"}
	service := NewAdminStrategyService(&adminStrategyRepository{items: []domain.AdminStrategy{source}})
	if _, err := service.CreateVersion(context.Background(), source.ID, "2", domain.AuditEvent{}); err == nil {
		t.Fatal("expected unpublished source rejection")
	}
}
