package indicatorcalc

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	FeatureSchemaVersion = "indicators.v1"
	CalculatorVersion    = "go-indicator.v1"
)

type FeatureKind string

const (
	FeatureKindIndicator FeatureKind = "indicator"
	FeatureKindDerived   FeatureKind = "derived"
	FeatureKindSignal    FeatureKind = "signal"
)

type ValueType string

const (
	ValueTypeFloat  ValueType = "float"
	ValueTypeString ValueType = "string"
)

type FeatureDefinition struct {
	Name        string
	Kind        FeatureKind
	ValueType   ValueType
	Unit        string
	Description string
	WarmupBars  int
}

type FeatureMetadata struct {
	SchemaVersion     string `json:"schema_version"`
	CalculatorVersion string `json:"calculator_version"`
	ParameterHash     string `json:"parameter_hash"`
}

var coreFeatureDefinitions = []FeatureDefinition{
	{Name: "ema7", Kind: FeatureKindIndicator, ValueType: ValueTypeFloat, Unit: "price", Description: "7-period exponential moving average", WarmupBars: 7},
	{Name: "ema25", Kind: FeatureKindIndicator, ValueType: ValueTypeFloat, Unit: "price", Description: "25-period exponential moving average", WarmupBars: 25},
	{Name: "ema99", Kind: FeatureKindIndicator, ValueType: ValueTypeFloat, Unit: "price", Description: "99-period exponential moving average", WarmupBars: 99},
	{Name: "rsi14", Kind: FeatureKindIndicator, ValueType: ValueTypeFloat, Unit: "ratio", Description: "14-period relative strength index", WarmupBars: 14},
	{Name: "atr14", Kind: FeatureKindIndicator, ValueType: ValueTypeFloat, Unit: "price", Description: "14-period average true range", WarmupBars: 14},
	{Name: "macd", Kind: FeatureKindIndicator, ValueType: ValueTypeFloat, Description: "MACD line", WarmupBars: 26},
	{Name: "supertrend", Kind: FeatureKindIndicator, ValueType: ValueTypeFloat, Unit: "price", Description: "Supertrend price level", WarmupBars: 10},
	{Name: "supertrend_direction", Kind: FeatureKindSignal, ValueType: ValueTypeString, Description: "Supertrend direction", WarmupBars: 10},
	{Name: "data_quality", Kind: FeatureKindSignal, ValueType: ValueTypeString, Description: "Input data quality classification", WarmupBars: 1},
}

func CoreFeatureDefinitions() []FeatureDefinition {
	return append([]FeatureDefinition(nil), coreFeatureDefinitions...)
}

func ValidateFeatureDefinitions(definitions []FeatureDefinition) error {
	seen := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		name := strings.TrimSpace(definition.Name)
		if name == "" {
			return fmt.Errorf("feature name is required")
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate feature definition %q", name)
		}
		seen[name] = struct{}{}
		if definition.Kind == "" {
			return fmt.Errorf("feature %q kind is required", name)
		}
		if definition.ValueType == "" {
			return fmt.Errorf("feature %q value type is required", name)
		}
		if definition.WarmupBars < 0 {
			return fmt.Errorf("feature %q warmup bars cannot be negative", name)
		}
	}
	return nil
}

func Metadata(options Options) FeatureMetadata {
	return FeatureMetadata{
		SchemaVersion:     FeatureSchemaVersion,
		CalculatorVersion: CalculatorVersion,
		ParameterHash:     parameterHash(options),
	}
}

func parameterHash(options Options) string {
	if len(options.SMAPeriods) == 0 && len(options.EMAPeriods) == 0 && len(options.WMAPeriods) == 0 {
		options = DefaultOptions()
	}
	parts := []string{
		"sma=" + periodList(options.SMAPeriods),
		"ema=" + periodList(options.EMAPeriods),
		"wma=" + periodList(options.WMAPeriods),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:8])
}

func periodList(periods []int) string {
	copied := append([]int(nil), periods...)
	sort.Ints(copied)
	values := make([]string, 0, len(copied))
	for _, period := range copied {
		values = append(values, strconv.Itoa(period))
	}
	return strings.Join(values, ",")
}
