package indicatorcalc

import (
	"math"
	"strconv"
)

type ValueSet struct {
	numeric map[string]float64
}

func NewValueSet(capacity int) *ValueSet {
	if capacity < 0 {
		capacity = 0
	}
	return &ValueSet{numeric: make(map[string]float64, capacity)}
}

func ValueSetFromStrings(values map[string]string) *ValueSet {
	set := NewValueSet(len(values))
	for name, text := range values {
		value, err := strconv.ParseFloat(text, 64)
		if err == nil {
			set.Set(name, value)
		}
	}
	return set
}

func (s *ValueSet) Set(name string, value float64) {
	if s == nil || name == "" || math.IsNaN(value) || math.IsInf(value, 0) {
		return
	}
	s.numeric[name] = value
}

func (s *ValueSet) Numeric() map[string]float64 {
	if s == nil {
		return nil
	}
	return s.numeric
}

func (s *ValueSet) EncodeStrings() map[string]string {
	if s == nil {
		return nil
	}
	encoded := make(map[string]string, len(s.numeric))
	for name, value := range s.numeric {
		encoded[name] = format(value)
	}
	return encoded
}

func (s *ValueSet) MergeLegacy(legacy map[string]string) (map[string]string, map[string]float64) {
	if len(legacy) == 0 {
		return s.EncodeStrings(), s.Numeric()
	}
	if legacy == nil {
		legacy = make(map[string]string, len(s.numeric))
	}
	for name, value := range s.numeric {
		legacy[name] = format(value)
	}
	numeric := ValueSetFromStrings(legacy).Numeric()
	for name, value := range s.numeric {
		numeric[name] = value
	}
	return legacy, numeric
}
