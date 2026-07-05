package symbolspec

import (
	"fmt"
	"math"
	"strings"
)

const (
	QuantityUnitBase     = "base"
	QuantityUnitContract = "contract"
)

type Key struct {
	Exchange string
	Market   string
	Symbol   string
}

type Capability struct {
	Exchange     string
	Market       string
	Symbol       string
	PriceTick    float64
	QuantityStep float64
	MinQuantity  float64
	MinNotional  float64
	ContractSize float64
	QuantityUnit string
}

type NormalizedOrder struct {
	Quantity float64
	Notional float64
}

func NewKey(exchange string, market string, symbol string) Key {
	return Key{
		Exchange: strings.ToLower(strings.TrimSpace(exchange)),
		Market:   strings.ToLower(strings.TrimSpace(market)),
		Symbol:   strings.ToUpper(strings.TrimSpace(symbol)),
	}
}

func (c Capability) Key() Key {
	return NewKey(c.Exchange, c.Market, c.Symbol)
}

func Normalize(capability Capability) Capability {
	capability.Exchange = strings.ToLower(strings.TrimSpace(capability.Exchange))
	capability.Market = strings.ToLower(strings.TrimSpace(capability.Market))
	capability.Symbol = strings.ToUpper(strings.TrimSpace(capability.Symbol))
	capability.QuantityUnit = strings.ToLower(strings.TrimSpace(capability.QuantityUnit))
	if capability.QuantityUnit == "" {
		capability.QuantityUnit = QuantityUnitBase
	}
	if capability.ContractSize <= 0 {
		capability.ContractSize = 1
	}
	return capability
}

func Validate(capability Capability) error {
	capability = Normalize(capability)
	if capability.Exchange == "" {
		return fmt.Errorf("exchange cannot be empty")
	}
	if capability.Market == "" {
		return fmt.Errorf("market cannot be empty")
	}
	if capability.Symbol == "" {
		return fmt.Errorf("symbol cannot be empty")
	}
	if capability.PriceTick < 0 {
		return fmt.Errorf("price_tick cannot be negative")
	}
	if capability.QuantityStep < 0 {
		return fmt.Errorf("quantity_step cannot be negative")
	}
	if capability.MinQuantity < 0 {
		return fmt.Errorf("min_quantity cannot be negative")
	}
	if capability.MinNotional < 0 {
		return fmt.Errorf("min_notional cannot be negative")
	}
	if capability.ContractSize <= 0 {
		return fmt.Errorf("contract_size must be positive")
	}
	switch capability.QuantityUnit {
	case QuantityUnitBase, QuantityUnitContract:
		return nil
	default:
		return fmt.Errorf("unsupported quantity_unit %q", capability.QuantityUnit)
	}
}

func NormalizeQuoteOrder(capability Capability, price float64, quoteNotional float64) (NormalizedOrder, error) {
	capability = Normalize(capability)
	if err := Validate(capability); err != nil {
		return NormalizedOrder{}, err
	}
	if price <= 0 {
		return NormalizedOrder{}, fmt.Errorf("price must be positive")
	}
	if quoteNotional <= 0 {
		return NormalizedOrder{}, fmt.Errorf("quote notional must be positive")
	}
	quantity := quoteNotional / price
	if capability.QuantityUnit == QuantityUnitContract {
		quantity = quoteNotional / (price * capability.ContractSize)
	}
	quantity = floorToStep(quantity, capability.QuantityStep)
	notional := Notional(capability, price, quantity)
	if capability.MinQuantity > 0 && quantity < capability.MinQuantity {
		return NormalizedOrder{}, fmt.Errorf("quantity %g is below min_quantity %g", quantity, capability.MinQuantity)
	}
	if capability.MinNotional > 0 && notional < capability.MinNotional {
		return NormalizedOrder{}, fmt.Errorf("notional %g is below min_notional %g", notional, capability.MinNotional)
	}
	return NormalizedOrder{
		Quantity: quantity,
		Notional: notional,
	}, nil
}

func Notional(capability Capability, price float64, quantity float64) float64 {
	capability = Normalize(capability)
	if capability.QuantityUnit == QuantityUnitContract {
		return price * quantity * capability.ContractSize
	}
	return price * quantity
}

func floorToStep(quantity float64, step float64) float64 {
	if step <= 0 {
		return quantity
	}
	return math.Floor(quantity/step+1e-12) * step
}
