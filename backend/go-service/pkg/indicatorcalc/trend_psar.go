package indicatorcalc

type streamPSARState struct {
	step          float64
	maxStep       float64
	count         int
	uptrend       bool
	sar           float64
	ep            float64
	acceleration  float64
	previousHigh  float64
	previousLow   float64
	previousClose float64
	priorHigh     float64
	priorLow      float64
}

func newStreamPSARState(step float64, maxStep float64) streamPSARState {
	return streamPSARState{step: step, maxStep: maxStep}
}

func (s *streamPSARState) append(high float64, low float64, closeValue float64) {
	if s == nil || s.step <= 0 || s.maxStep < s.step {
		return
	}
	switch s.count {
	case 0:
		s.previousHigh = high
		s.previousLow = low
		s.previousClose = closeValue
		s.count = 1
		return
	case 1:
		s.uptrend = closeValue >= s.previousClose
		s.sar = s.previousLow
		s.ep = s.previousHigh
		if !s.uptrend {
			s.sar = s.previousHigh
			s.ep = s.previousLow
		}
		s.acceleration = s.step
		s.advance(high, low, false)
	default:
		s.advance(high, low, true)
	}
	s.priorHigh, s.priorLow = s.previousHigh, s.previousLow
	s.previousHigh, s.previousLow, s.previousClose = high, low, closeValue
	s.count++
}

func (s *streamPSARState) advance(high float64, low float64, constrain bool) {
	s.sar = s.sar + s.acceleration*(s.ep-s.sar)
	if s.uptrend {
		if constrain {
			s.sar = minFloat(s.sar, s.previousLow, s.priorLow)
		}
		if low < s.sar {
			s.uptrend = false
			s.sar = s.ep
			s.ep = low
			s.acceleration = s.step
			return
		}
		if high > s.ep {
			s.ep = high
			s.acceleration = minFloat(s.acceleration+s.step, s.maxStep)
		}
		return
	}
	if constrain {
		s.sar = maxFloat(s.sar, s.previousHigh, s.priorHigh)
	}
	if high > s.sar {
		s.uptrend = true
		s.sar = s.ep
		s.ep = high
		s.acceleration = s.step
		return
	}
	if low < s.ep {
		s.ep = low
		s.acceleration = minFloat(s.acceleration+s.step, s.maxStep)
	}
}

func (s *streamPSARState) value() (float64, string, bool) {
	if s == nil || s.count < 3 {
		return 0, "", false
	}
	if s.uptrend {
		return s.sar, "up", true
	}
	return s.sar, "down", true
}

func addPSARFeatures(values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	addPSARFeaturesToSet(nil, values, signals, highs, lows, closes)
}

func addPSARFeaturesToSet(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64) {
	addPSARFeaturesWithStateToSet(target, values, signals, highs, lows, closes, nil)
}

func addPSARFeaturesWithStateToSet(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, basic *basicIndicatorState) {
	value, direction, ok := 0.0, "", false
	if basic != nil {
		value, direction, ok = basic.psarValue()
	}
	if !ok {
		value, direction, ok = psar(highs, lows, closes, 0.02, 0.2)
	}
	if !ok {
		return
	}
	last := closes[len(closes)-1]
	setValueTarget(target, values, "psar", value, true)
	setValueTarget(target, values, "psar_distance_pct", percentDistance(last, value), value != 0)
	signals["psar_direction"] = direction
}

func psar(highs []float64, lows []float64, closes []float64, step float64, maxStep float64) (float64, string, bool) {
	if len(closes) < 3 || step <= 0 || maxStep < step {
		return 0, "", false
	}
	uptrend := closes[1] >= closes[0]
	sar := lows[0]
	ep := highs[0]
	if !uptrend {
		sar = highs[0]
		ep = lows[0]
	}
	acceleration := step
	for index := 1; index < len(closes); index++ {
		sar = sar + acceleration*(ep-sar)
		if uptrend {
			if index >= 2 {
				sar = minFloat(sar, lows[index-1], lows[index-2])
			}
			if lows[index] < sar {
				uptrend = false
				sar = ep
				ep = lows[index]
				acceleration = step
				continue
			}
			if highs[index] > ep {
				ep = highs[index]
				acceleration = minFloat(acceleration+step, maxStep)
			}
			continue
		}
		if index >= 2 {
			sar = maxFloat(sar, highs[index-1], highs[index-2])
		}
		if highs[index] > sar {
			uptrend = true
			sar = ep
			ep = highs[index]
			acceleration = step
			continue
		}
		if lows[index] < ep {
			ep = lows[index]
			acceleration = minFloat(acceleration+step, maxStep)
		}
	}
	if uptrend {
		return sar, "up", true
	}
	return sar, "down", true
}
