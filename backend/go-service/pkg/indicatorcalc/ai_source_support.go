package indicatorcalc

import "math"

func aiSourceBestID(scores [4]float64) int {
	best := 0
	for index := 1; index < len(scores); index++ {
		if scores[index] > scores[best] {
			best = index
		}
	}
	return best
}

func aiSourceName(id int) string {
	switch id {
	case aiSourceOpen:
		return "open"
	case aiSourceHigh:
		return "high"
	case aiSourceLow:
		return "low"
	default:
		return "close"
	}
}

func atrValueAt(index int, values []float64, offset int) float64 {
	atrIndex := index - offset
	if atrIndex < 0 || atrIndex >= len(values) {
		return 0
	}
	return values[atrIndex]
}

func emaLast(values []float64, period int) float64 {
	value, ok := ema(values, minInt(period, len(values)))
	if !ok {
		return values[len(values)-1]
	}
	return value
}

type aiSourceEMAState struct {
	period int
	sum    float64
	count  int
	value  float64
	ready  bool
}

func newAISourceEMAState(period int) *aiSourceEMAState {
	return &aiSourceEMAState{period: period}
}

func (s *aiSourceEMAState) append(value float64) float64 {
	if s == nil || s.period <= 1 {
		return value
	}
	if !s.ready {
		s.sum += value
		s.count++
		s.value = s.sum / float64(s.count)
		if s.count >= s.period {
			s.ready = true
		}
		return s.value
	}
	multiplier := 2 / float64(s.period+1)
	s.value = (value-s.value)*multiplier + s.value
	return s.value
}

func scaleValue01(value float64, low float64, high float64) float64 {
	if high == low {
		return 0.5
	}
	return clampFloat((value-low)/(high-low), 0, 1)
}

func clampFloat(value float64, low float64, high float64) float64 {
	return math.Max(low, math.Min(value, high))
}

func normScoreFloat(value float64) float64 {
	return 1 / (1 + math.Exp(-clampFloat(value, -8, 8)))
}

func sumArray(values []float64) float64 {
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total
}

func aiSourceBoolSignal(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
