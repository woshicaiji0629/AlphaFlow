package indicatorcalc

type streamATRState struct {
	period int
	sum    float64
	count  int
	value  float64
	ready  bool
	series []float64
}

type streamADXState struct {
	period          int
	smoothedTR      float64
	smoothedPlusDM  float64
	smoothedMinusDM float64
	dxValues        []float64
	dxSum           float64
	value           float64
	plusDI          float64
	minusDI         float64
	ready           bool
}

func newStreamATRState(period int) streamATRState {
	return streamATRState{period: period}
}

func (s streamATRState) clone() streamATRState {
	return s.cloneWithExtraCapacity(0)
}

func (s streamATRState) cloneWithExtraCapacity(extra int) streamATRState {
	s.series = cloneSliceWithExtra(s.series, extra)
	return s
}

func (s *streamATRState) append(high float64, low float64, previousClose float64) {
	if s == nil || s.period <= 0 {
		return
	}
	trueRange := maxFloat(high-low, absFloat(high-previousClose), absFloat(low-previousClose))
	if !s.ready {
		s.sum += trueRange
		s.count++
		if s.count == s.period {
			s.value = s.sum / float64(s.period)
			s.ready = true
			s.series = append(s.series, s.value)
		}
		return
	}
	s.value = (s.value*float64(s.period-1) + trueRange) / float64(s.period)
	s.series = append(s.series, s.value)
}

func newStreamADXState(period int) streamADXState {
	return streamADXState{period: period}
}

func (s streamADXState) clone() streamADXState {
	return s.cloneWithExtraCapacity(0)
}

func (s streamADXState) cloneWithExtraCapacity(extra int) streamADXState {
	s.dxValues = cloneSliceWithExtra(s.dxValues, extra)
	return s
}

func (s *streamADXState) append(currentHigh float64, currentLow float64, previousHigh float64, previousLow float64, previousClose float64) {
	if s == nil || s.period <= 0 {
		return
	}
	trueRange := maxFloat(currentHigh-currentLow, absFloat(currentHigh-previousClose), absFloat(currentLow-previousClose))
	upMove := directionalMovementPlus(currentHigh, previousHigh, currentLow, previousLow)
	downMove := directionalMovementMinus(currentHigh, previousHigh, currentLow, previousLow)
	s.smoothedTR = s.smoothedTR - s.smoothedTR/float64(s.period) + trueRange
	s.smoothedPlusDM = s.smoothedPlusDM - s.smoothedPlusDM/float64(s.period) + upMove
	s.smoothedMinusDM = s.smoothedMinusDM - s.smoothedMinusDM/float64(s.period) + downMove
	var dx float64
	s.plusDI, s.minusDI, dx = directionalIndex(s.smoothedTR, s.smoothedPlusDM, s.smoothedMinusDM)
	s.dxValues = append(s.dxValues, dx)
	s.dxSum += dx
	if len(s.dxValues) > s.period {
		s.dxSum -= s.dxValues[len(s.dxValues)-s.period-1]
	}
	if len(s.dxValues) >= s.period {
		s.value = s.dxSum / float64(s.period)
		s.ready = true
	}
}
