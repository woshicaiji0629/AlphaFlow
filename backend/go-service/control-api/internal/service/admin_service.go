package service

import (
	"context"
	"fmt"

	"alphaflow/go-service/control-api/internal/domain"
	"alphaflow/go-service/control-api/internal/repository"
)

type AdminService struct {
	repository repository.AdminRepository
	passwords  repository.PasswordHasher
}

func NewAdminService(repo repository.AdminRepository, passwords repository.PasswordHasher) *AdminService {
	return &AdminService{repository: repo, passwords: passwords}
}

type CreateInitialAdminInput struct {
	Email, DisplayName, Password string
}

func (s *AdminService) CreateInitialAdmin(ctx context.Context, input CreateInitialAdminInput) (domain.InitialAdminResult, error) {
	if input.Password == "" {
		return domain.InitialAdminResult{}, fmt.Errorf("password is required")
	}
	hash, err := s.passwords.Hash(input.Password)
	if err != nil {
		return domain.InitialAdminResult{}, err
	}
	return s.repository.CreateInitialAdmin(ctx, domain.InitialAdmin{
		Email: input.Email, DisplayName: input.DisplayName, PasswordHash: hash,
	})
}
