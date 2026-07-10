package executionadapter

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"alphaflow/go-service/pkg/executionaccount"
)

func EnsureTradingEnabled(account executionaccount.Account) error {
	if err := account.Validate(); err != nil {
		return err
	}
	if !account.Enabled {
		return fmt.Errorf("account %s is disabled", account.ID)
	}
	if !account.TradingEnabled {
		return fmt.Errorf("trading is disabled for account %s", account.ID)
	}
	if account.Environment == executionaccount.EnvironmentLive && !account.LiveConfirmed {
		return fmt.Errorf("live trading is not confirmed")
	}
	return nil
}

func ClientOrderID(prefix, intentID string, maxLength int) string {
	clean := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, intentID)
	candidate := prefix + clean
	if len(candidate) <= maxLength {
		return candidate
	}
	sum := sha256.Sum256([]byte(intentID))
	hash := hex.EncodeToString(sum[:])
	if maxLength <= len(prefix) {
		return prefix[:maxLength]
	}
	return prefix + hash[:maxLength-len(prefix)]
}
