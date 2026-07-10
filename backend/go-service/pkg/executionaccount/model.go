package executionaccount

import (
	"fmt"
	"strings"
)

type Environment string

const (
	EnvironmentTestnet Environment = "testnet"
	EnvironmentLive    Environment = "live"
)

type PositionMode string

const (
	PositionModeOneWay PositionMode = "one_way"
	PositionModeHedge  PositionMode = "hedge"
)

type MarginMode string

const (
	MarginModeCross    MarginMode = "cross"
	MarginModeIsolated MarginMode = "isolated"
)

type Account struct {
	ID                   string       `json:"id"`
	Name                 string       `json:"name"`
	Exchange             string       `json:"exchange"`
	Environment          Environment  `json:"environment"`
	Market               string       `json:"market"`
	PositionMode         PositionMode `json:"position_mode"`
	MarginMode           MarginMode   `json:"margin_mode"`
	CredentialCiphertext string       `json:"-"`
	Enabled              bool         `json:"enabled"`
	TradingEnabled       bool         `json:"trading_enabled"`
	LiveConfirmed        bool         `json:"live_confirmed"`
}
type Credential struct {
	APIKey     string `json:"api_key"`
	APISecret  string `json:"api_secret"`
	Passphrase string `json:"passphrase,omitempty"`
}

func (a Account) Validate() error {
	if strings.TrimSpace(a.ID) == "" {
		return fmt.Errorf("account id is required")
	}
	if strings.TrimSpace(a.Exchange) == "" {
		return fmt.Errorf("exchange is required")
	}
	if a.Environment != EnvironmentTestnet && a.Environment != EnvironmentLive {
		return fmt.Errorf("unsupported environment %q", a.Environment)
	}
	if a.Environment == EnvironmentLive && a.TradingEnabled && !a.LiveConfirmed {
		return fmt.Errorf("live trading requires explicit confirmation")
	}
	return nil
}

func (c Credential) Validate(exchange string) error {
	if strings.TrimSpace(c.APIKey) == "" || strings.TrimSpace(c.APISecret) == "" {
		return fmt.Errorf("api key and secret are required")
	}
	if strings.EqualFold(exchange, "bitget") && strings.TrimSpace(c.Passphrase) == "" {
		return fmt.Errorf("bitget passphrase is required")
	}
	return nil
}
