package service

import (
	"context"
	"fmt"
	"strings"

	"alphaflow/go-service/control-api/internal/domain"
	"alphaflow/go-service/control-api/internal/repository"
	"alphaflow/go-service/pkg/strategyregistry"
	"alphaflow/go-service/pkg/strategyspec"
	"github.com/google/uuid"
)

type AdminStrategyInput struct {
	Code, Name, Description, Version, RiskLevel, Visibility, Status string
	Parameters                                                      map[string]string
	PaperEnabled, LiveEnabled                                       bool
}

type AdminStrategyService struct {
	repository repository.AdminStrategyRepository
}

type AdminStrategyValidationError struct{ Err error }

func (e AdminStrategyValidationError) Error() string { return e.Err.Error() }
func (e AdminStrategyValidationError) Unwrap() error { return e.Err }

func NewAdminStrategyService(repository repository.AdminStrategyRepository) *AdminStrategyService {
	return &AdminStrategyService{repository: repository}
}

func (s *AdminStrategyService) List(ctx context.Context) ([]domain.AdminStrategy, error) {
	return s.repository.ListAdminStrategies(ctx)
}

func (s *AdminStrategyService) CreateDraft(ctx context.Context, input AdminStrategyInput, audit domain.AuditEvent) (domain.AdminStrategy, error) {
	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.Name = strings.TrimSpace(input.Name)
	input.Version = strings.TrimSpace(input.Version)
	input.Status = "draft"
	input.LiveEnabled = false
	if err := ValidateAdminStrategy(input); err != nil {
		return domain.AdminStrategy{}, AdminStrategyValidationError{Err: err}
	}

	strategy := domain.AdminStrategy{
		ID:           uuid.New(),
		Code:         input.Code,
		Name:         input.Name,
		Description:  strings.TrimSpace(input.Description),
		Version:      input.Version,
		Parameters:   input.Parameters,
		Status:       input.Status,
		Visibility:   input.Visibility,
		RiskLevel:    input.RiskLevel,
		PaperEnabled: input.PaperEnabled,
		LiveEnabled:  input.LiveEnabled,
	}
	audit.Subject = strategy.ID.String()
	return s.repository.CreateAdminStrategy(ctx, strategy, audit)
}

func (s *AdminStrategyService) UpdateDraft(ctx context.Context, id uuid.UUID, input AdminStrategyInput, audit domain.AuditEvent) (domain.AdminStrategy, error) {
	current, err := s.repository.FindAdminStrategy(ctx, id)
	if err != nil {
		return domain.AdminStrategy{}, err
	}
	if current.Status != "draft" {
		return domain.AdminStrategy{}, domain.ErrStrategyNotEditable
	}
	if code := strings.ToLower(strings.TrimSpace(input.Code)); code != "" && code != current.Code {
		return domain.AdminStrategy{}, AdminStrategyValidationError{Err: fmt.Errorf("strategy code is immutable")}
	}
	input.Code = current.Code
	input.Name = strings.TrimSpace(input.Name)
	input.Version = strings.TrimSpace(input.Version)
	input.LiveEnabled = false
	if input.Status != "draft" && input.Status != "published" {
		return domain.AdminStrategy{}, AdminStrategyValidationError{Err: fmt.Errorf("draft can only be saved or published")}
	}
	if err := ValidateAdminStrategy(input); err != nil {
		return domain.AdminStrategy{}, AdminStrategyValidationError{Err: err}
	}
	current.Name = input.Name
	current.Description = strings.TrimSpace(input.Description)
	current.Version = input.Version
	current.Parameters = input.Parameters
	current.Status = input.Status
	current.Visibility = input.Visibility
	current.RiskLevel = input.RiskLevel
	current.PaperEnabled = input.PaperEnabled
	current.LiveEnabled = false
	audit.Subject = current.ID.String()
	return s.repository.UpdateAdminStrategy(ctx, current, audit)
}

func (s *AdminStrategyService) CreateVersion(ctx context.Context, sourceID uuid.UUID, version string, audit domain.AuditEvent) (domain.AdminStrategy, error) {
	source, err := s.repository.FindAdminStrategy(ctx, sourceID)
	if err != nil {
		return domain.AdminStrategy{}, err
	}
	if source.Status != "published" {
		return domain.AdminStrategy{}, AdminStrategyValidationError{Err: fmt.Errorf("new version must be copied from a published strategy")}
	}
	version = strings.TrimSpace(version)
	if version == "" || version == source.Version {
		return domain.AdminStrategy{}, AdminStrategyValidationError{Err: fmt.Errorf("a different version is required")}
	}
	parameters := make(map[string]string, len(source.Parameters))
	for key, value := range source.Parameters {
		parameters[key] = value
	}
	return s.CreateDraft(ctx, AdminStrategyInput{
		Code:         source.Code,
		Name:         source.Name,
		Description:  source.Description,
		Version:      version,
		Parameters:   parameters,
		RiskLevel:    source.RiskLevel,
		Visibility:   source.Visibility,
		PaperEnabled: source.PaperEnabled,
	}, audit)
}

func ValidateAdminStrategy(input AdminStrategyInput) error {
	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	if !strategyregistry.IsSupported(input.Code) {
		return fmt.Errorf("unsupported strategy code %q", input.Code)
	}
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Version) == "" {
		return fmt.Errorf("name and version are required")
	}
	if input.RiskLevel != "low" && input.RiskLevel != "medium" && input.RiskLevel != "high" {
		return fmt.Errorf("invalid risk level")
	}
	if input.Visibility != "public" && input.Visibility != "restricted" && input.Visibility != "admin_only" {
		return fmt.Errorf("invalid visibility")
	}
	if input.Status != "draft" && input.Status != "published" && input.Status != "disabled" {
		return fmt.Errorf("invalid status")
	}
	if input.LiveEnabled {
		return fmt.Errorf("live strategy publishing is not enabled")
	}
	if input.Status == "published" && !input.PaperEnabled {
		return fmt.Errorf("published strategy must enable paper mode")
	}
	_, err := strategyregistry.BuildSpec(strategyspec.Spec{Name: input.Code, Enabled: true, Params: input.Parameters})
	if err != nil {
		return fmt.Errorf("validate strategy parameters: %w", err)
	}
	return nil
}
