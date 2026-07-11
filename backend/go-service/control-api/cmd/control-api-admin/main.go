package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"alphaflow/go-service/control-api/internal/config"
	passwordinfra "alphaflow/go-service/control-api/internal/infrastructure/password"
	controlpostgres "alphaflow/go-service/control-api/internal/infrastructure/postgres"
	"alphaflow/go-service/control-api/internal/infrastructure/postgres/migrations"
	"alphaflow/go-service/control-api/internal/service"
)

const passwordEnv = "ALPHAFLOW_ADMIN_PASSWORD"

func main() {
	if len(os.Args) < 2 || os.Args[1] != "create-admin" {
		fmt.Fprintln(os.Stderr, "usage: control-api-admin create-admin -config PATH -email EMAIL -display-name NAME")
		os.Exit(2)
	}
	flags := flag.NewFlagSet("create-admin", flag.ExitOnError)
	configPath := flags.String("config", "", "path to control-api config file")
	email := flags.String("email", "", "initial admin email")
	displayName := flags.String("display-name", "", "initial admin display name")
	_ = flags.Parse(os.Args[2:])

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := createAdmin(ctx, createAdminOptions{
		configPath:  *configPath,
		email:       *email,
		displayName: *displayName,
		password:    os.Getenv(passwordEnv),
	}); err != nil {
		slog.Error("create initial admin failed", "error", err)
		os.Exit(1)
	}
	slog.Info("initial admin created", "email", *email)
}

type createAdminOptions struct {
	configPath, email, displayName, password string
}

func createAdmin(ctx context.Context, options createAdminOptions) error {
	if options.password == "" {
		return fmt.Errorf("%s is required", passwordEnv)
	}
	cfg, err := config.Load(options.configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	pool, err := controlpostgres.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	migrationTimeout, err := config.MigrationTimeout(cfg)
	if err != nil {
		return err
	}
	migrationCtx, cancel := context.WithTimeout(ctx, migrationTimeout)
	defer cancel()
	if err := migrations.Run(migrationCtx, pool); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	adminService := service.NewAdminService(controlpostgres.NewAdminStore(pool), passwordinfra.Hasher{})
	result, err := adminService.CreateInitialAdmin(ctx, service.CreateInitialAdminInput{
		Email: options.email, DisplayName: options.displayName, Password: options.password,
	})
	if errors.Is(err, controlpostgres.ErrAlreadyInitialized) {
		return fmt.Errorf("refusing to create admin: %w", err)
	}
	if err != nil {
		return err
	}
	slog.Info("initial admin identifiers created", "user_id", result.UserID, "trading_account_id", result.TradingAccountID)
	return nil
}
