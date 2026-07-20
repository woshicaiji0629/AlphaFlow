package indicatorcalc

import (
	"math"
	"strconv"
	"strings"
)

func finalizeValueSet(values *ValueSet, legacy map[string]string, encode bool) (map[string]string, map[string]float64) {
	if !encode && len(legacy) == 0 {
		return nil, values.Numeric()
	}
	return values.MergeLegacy(legacy)
}

func numericValues(values map[string]string) map[string]float64 {
	return ValueSetFromStrings(values).Numeric()
}

func setValue(values map[string]string, name string, value float64, ok bool) {
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
		return
	}
	values[name] = format(value)
}

func setValueSet(values *ValueSet, name string, value float64, ok bool) {
	if !ok {
		return
	}
	values.Set(name, value)
}

func setValueTarget(target *ValueSet, legacy map[string]string, name string, value float64, ok bool) {
	if target != nil {
		setValueSet(target, name, value, ok)
		return
	}
	setValue(legacy, name, value, ok)
}

func sum(values []float64) float64 {
	var result float64
	for _, value := range values {
		result += value
	}
	return result
}

func parse(value string) (float64, error) {
	text := strings.TrimSpace(value)
	if text == "" {
		return 0, nil
	}
	return strconv.ParseFloat(text, 64)
}

func format(value float64) string {
	text := strconv.FormatFloat(value, 'f', 8, 64)
	text = strings.TrimRight(text, "0")
	text = strings.TrimRight(text, ".")
	if text == "" || text == "-0" {
		return "0"
	}
	return text
}
