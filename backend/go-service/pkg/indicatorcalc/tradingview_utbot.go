package indicatorcalc

import "math"

type streamUTBotState struct {
	multiplier        float64
	stop              float64
	direction         string
	previousDirection string
	previousClose     float64
	initialized       bool
	ready             bool
}

func newStreamUTBotState(multiplier float64) streamUTBotState {
	return streamUTBotState{multiplier: multiplier}
}

func (s *streamUTBotState) append(closeValue float64, atrValue float64, atrReady bool) {
	if s == nil || s.multiplier <= 0 || !atrReady {
		return
	}
	loss := s.multiplier * atrValue
	if !s.initialized {
		s.stop = closeValue - loss
		s.direction = "up"
		s.previousDirection = "up"
		s.previousClose = closeValue
		s.initialized = true
		return
	}
	previousStop := s.stop
	s.previousDirection = s.direction
	switch {
	case closeValue > previousStop && s.previousClose > previousStop:
		s.stop, s.direction = math.Max(previousStop, closeValue-loss), "up"
	case closeValue < previousStop && s.previousClose < previousStop:
		s.stop, s.direction = math.Min(previousStop, closeValue+loss), "down"
	case closeValue > previousStop:
		s.stop, s.direction = closeValue-loss, "up"
	default:
		s.stop, s.direction = closeValue+loss, "down"
	}
	s.previousClose = closeValue
	s.ready = true
}

func (s *streamUTBotState) value() (float64, string, string, bool) {
	if s == nil || !s.ready {
		return 0, "", "", false
	}
	return s.stop, s.direction, s.previousDirection, true
}

func addUTBotFeatures(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, multiplier float64) {
	atrValues, ok := atrSeries(highs, lows, closes, period)
	if !ok {
		return
	}
	addUTBotFeaturesWithATR(target, values, signals, closes, period, multiplier, atrValues, nil)
}

func addUTBotFeaturesWithATR(target *ValueSet, values map[string]string, signals map[string]string, closes []float64, period int, multiplier float64, atrValues []float64, basic *basicIndicatorState) {
	stop, direction, previousDirection, ok := basic.utBotValue(period, multiplier)
	if !ok {
		stop, direction, previousDirection, ok = utBotWithATR(closes, period, multiplier, atrValues)
	}
	if !ok {
		return
	}
	setValueTarget(target, values, "ut_stop", stop, true)
	setValueTarget(target, values, "ut_stop_distance_pct", absFloat(percentDistance(closes[len(closes)-1], stop)), stop != 0)
	signals["ut_direction"] = direction
	signals["ut_signal"] = directionFlipSignal(previousDirection, direction)
}

func utBot(highs []float64, lows []float64, closes []float64, period int, multiplier float64) (float64, string, string, bool) {
	atrValues, ok := atrSeries(highs, lows, closes, period)
	if !ok {
		return 0, "", "", false
	}
	return utBotWithATR(closes, period, multiplier, atrValues)
}

func utBotWithATR(closes []float64, period int, multiplier float64, atrValues []float64) (float64, string, string, bool) {
	if len(atrValues) < 2 || len(closes) <= period {
		return 0, "", "", false
	}
	stop := closes[period] - multiplier*atrValues[0]
	direction, previousDirection := "up", "up"
	for index := period + 1; index < len(closes); index++ {
		previousStop := stop
		previousDirection = direction
		loss := multiplier * atrValues[index-period]
		if closes[index] > previousStop && closes[index-1] > previousStop {
			stop, direction = math.Max(previousStop, closes[index]-loss), "up"
			continue
		}
		if closes[index] < previousStop && closes[index-1] < previousStop {
			stop, direction = math.Min(previousStop, closes[index]+loss), "down"
			continue
		}
		if closes[index] > previousStop {
			stop, direction = closes[index]-loss, "up"
		} else {
			stop, direction = closes[index]+loss, "down"
		}
	}
	return stop, direction, previousDirection, true
}
