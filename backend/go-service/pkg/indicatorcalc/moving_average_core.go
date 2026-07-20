package indicatorcalc

import "math"

type streamKAMAState struct {
	values  [11]float64
	count   int
	current float64
	ready   bool
}

type streamSMMAState struct {
	period  int
	sum     float64
	count   int
	current float64
	ready   bool
}

type streamHMA21State struct {
	closes          [21]float64
	closeCount      int
	differences     [4]float64
	differenceCount int
	values          [4]float64
	valueCount      int
}

func (s *streamHMA21State) append(value float64) {
	if s.closeCount < len(s.closes) {
		s.closes[s.closeCount] = value
		s.closeCount++
		if s.closeCount < len(s.closes) {
			return
		}
	} else {
		copy(s.closes[:len(s.closes)-1], s.closes[1:])
		s.closes[len(s.closes)-1] = value
	}
	half, _ := wma(s.closes[len(s.closes)-10:], 10)
	full, _ := wma(s.closes[:], 21)
	s.appendDifference(2*half - full)
}

func (s *streamHMA21State) appendDifference(value float64) {
	if s.differenceCount < len(s.differences) {
		s.differences[s.differenceCount] = value
		s.differenceCount++
		if s.differenceCount < len(s.differences) {
			return
		}
	} else {
		copy(s.differences[:len(s.differences)-1], s.differences[1:])
		s.differences[len(s.differences)-1] = value
	}
	hmaValue, _ := wma(s.differences[:], len(s.differences))
	if s.valueCount < len(s.values) {
		s.values[s.valueCount] = hmaValue
		s.valueCount++
		return
	}
	copy(s.values[:len(s.values)-1], s.values[1:])
	s.values[len(s.values)-1] = hmaValue
}

func (s *streamHMA21State) value() (float64, bool) {
	if s == nil || s.valueCount == 0 {
		return 0, false
	}
	return s.values[s.valueCount-1], true
}

func (s *streamHMA21State) previous3() (float64, bool) {
	if s == nil || s.valueCount < len(s.values) {
		return 0, false
	}
	return s.values[0], true
}

func newStreamSMMAState(period int) streamSMMAState {
	return streamSMMAState{period: period}
}

func (s *streamSMMAState) append(value float64) {
	if s == nil || s.period <= 0 {
		return
	}
	if !s.ready {
		s.sum += value
		s.count++
		if s.count == s.period {
			s.current = s.sum / float64(s.period)
			s.ready = true
		}
		return
	}
	s.current = (s.current*float64(s.period-1) + value) / float64(s.period)
}

func (s *streamSMMAState) value() (float64, bool) {
	if s == nil || !s.ready {
		return 0, false
	}
	return s.current, true
}

func (s *streamKAMAState) append(value float64) {
	const period = 10
	if s.count < len(s.values) {
		s.values[s.count] = value
		s.count++
		if s.count == len(s.values) {
			s.current = value
			s.ready = true
		}
		return
	}
	copy(s.values[:period], s.values[1:])
	s.values[period] = value
	change := math.Abs(value - s.values[0])
	var volatility float64
	for index := 1; index <= period; index++ {
		volatility += math.Abs(s.values[index] - s.values[index-1])
	}
	efficiency := 0.0
	if volatility != 0 {
		efficiency = change / volatility
	}
	fastSC := 2.0 / 3.0
	slowSC := 2.0 / 31.0
	smoothing := math.Pow(efficiency*(fastSC-slowSC)+slowSC, 2)
	s.current += smoothing * (value - s.current)
}

func (s *streamKAMAState) value() (float64, bool) {
	if s == nil || !s.ready {
		return 0, false
	}
	return s.current, true
}

func hma(values []float64, period int) (float64, bool) {
	if period <= 1 || len(values) < period {
		return 0, false
	}
	half := period / 2
	sqrtPeriod := int(math.Sqrt(float64(period)))
	if sqrtPeriod < 1 {
		return 0, false
	}
	differenceCount := len(values) - period + 1
	if differenceCount < sqrtPeriod {
		return 0, false
	}
	startEnd := len(values) - sqrtPeriod + 1
	differences := make([]float64, 0, sqrtPeriod)
	for end := startEnd; end <= len(values); end++ {
		halfWMA, okHalf := wma(values[end-half:end], half)
		fullWMA, okFull := wma(values[end-period:end], period)
		if !okHalf || !okFull {
			return 0, false
		}
		differences = append(differences, 2*halfWMA-fullWMA)
	}
	return wma(differences, sqrtPeriod)
}

func vwma(values []float64, volumes []float64, period int) (float64, bool) {
	if period <= 0 || len(values) < period || len(volumes) != len(values) {
		return 0, false
	}
	start := len(values) - period
	var weighted float64
	var volumeSum float64
	for index := start; index < len(values); index++ {
		weighted += values[index] * volumes[index]
		volumeSum += volumes[index]
	}
	if volumeSum == 0 {
		return 0, false
	}
	return weighted / volumeSum, true
}

func dema(values []float64, period int) (float64, bool) {
	ema1 := newStreamEMAState(period)
	ema2 := newStreamEMAState(period)
	for _, value := range values {
		ema1.append(value)
		if ema1.ready {
			ema2.append(ema1.value)
		}
	}
	if !ema1.ready || !ema2.ready {
		return 0, false
	}
	return 2*ema1.value - ema2.value, true
}

func tema(values []float64, period int) (float64, bool) {
	ema1 := newStreamEMAState(period)
	ema2 := newStreamEMAState(period)
	ema3 := newStreamEMAState(period)
	for _, value := range values {
		ema1.append(value)
		if ema1.ready {
			ema2.append(ema1.value)
		}
		if ema2.ready {
			ema3.append(ema2.value)
		}
	}
	if !ema1.ready || !ema2.ready || !ema3.ready {
		return 0, false
	}
	return 3*ema1.value - 3*ema2.value + ema3.value, true
}

func demaTema(values []float64, period int) (float64, float64, bool, bool) {
	ema1 := newStreamEMAState(period)
	ema2 := newStreamEMAState(period)
	ema3 := newStreamEMAState(period)
	for _, value := range values {
		ema1.append(value)
		if ema1.ready {
			ema2.append(ema1.value)
		}
		if ema2.ready {
			ema3.append(ema2.value)
		}
	}
	demaOK := ema1.ready && ema2.ready
	temaOK := demaOK && ema3.ready
	var demaValue, temaValue float64
	if demaOK {
		demaValue = 2*ema1.value - ema2.value
	}
	if temaOK {
		temaValue = 3*ema1.value - 3*ema2.value + ema3.value
	}
	return demaValue, temaValue, demaOK, temaOK
}

func tilsonT3(values []float64, period int, factor float64) (float64, bool) {
	first, ok := gd(values, period, factor)
	if !ok {
		return 0, false
	}
	second, ok := gd(first, period, factor)
	if !ok {
		return 0, false
	}
	third, ok := gd(second, period, factor)
	if !ok {
		return 0, false
	}
	return third[len(third)-1], true
}

func gd(values []float64, period int, factor float64) ([]float64, bool) {
	first, ok := emaSeries(values, period)
	if !ok {
		return nil, false
	}
	second, ok := emaSeries(first, period)
	if !ok {
		return nil, false
	}
	offset := len(first) - len(second)
	result := make([]float64, 0, len(second))
	for index, secondValue := range second {
		result = append(result, first[index+offset]*(1+factor)-secondValue*factor)
	}
	return result, true
}

func kama(values []float64, period int, fast int, slow int) (float64, bool) {
	if period <= 0 || fast <= 0 || slow <= 0 || len(values) <= period {
		return 0, false
	}
	fastSC := 2.0 / float64(fast+1)
	slowSC := 2.0 / float64(slow+1)
	current := values[period]
	for index := period + 1; index < len(values); index++ {
		change := math.Abs(values[index] - values[index-period])
		var volatility float64
		for offset := index - period + 1; offset <= index; offset++ {
			volatility += math.Abs(values[offset] - values[offset-1])
		}
		efficiency := 0.0
		if volatility != 0 {
			efficiency = change / volatility
		}
		smoothing := math.Pow(efficiency*(fastSC-slowSC)+slowSC, 2)
		current = current + smoothing*(values[index]-current)
	}
	return current, true
}

func alligator(values []float64) (float64, float64, float64, bool) {
	jaw, okJaw := smma(values, 13)
	teeth, okTeeth := smma(values, 8)
	lips, okLips := smma(values, 5)
	if !okJaw || !okTeeth || !okLips {
		return 0, 0, 0, false
	}
	return jaw, teeth, lips, true
}

func smma(values []float64, period int) (float64, bool) {
	if period <= 0 || len(values) < period {
		return 0, false
	}
	current, _ := sma(values[:period], period)
	for index := period; index < len(values); index++ {
		current = (current*float64(period-1) + values[index]) / float64(period)
	}
	return current, true
}

func smmaSeries(values []float64, period int) ([]float64, bool) {
	if period <= 0 || len(values) < period {
		return nil, false
	}
	result := make([]float64, 0, len(values)-period+1)
	current, _ := sma(values[:period], period)
	result = append(result, current)
	for index := period; index < len(values); index++ {
		current = (current*float64(period-1) + values[index]) / float64(period)
		result = append(result, current)
	}
	return result, true
}

func movingAverageByType(values []float64, volumes []float64, period int, maType int, t3Factor float64) (float64, bool) {
	switch maType {
	case 1:
		return sma(values, period)
	case 2:
		return ema(values, period)
	case 3:
		return wma(values, period)
	case 4:
		return hma(values, period)
	case 5:
		return vwma(values, volumes, period)
	case 6:
		return smma(values, period)
	case 7:
		return tema(values, period)
	default:
		return tilsonT3(values, period, t3Factor)
	}
}

func emaFromStateOrSeries(basic *basicIndicatorState, closes []float64, period int) (float64, bool) {
	if value, ok := basic.emaValue(period); ok {
		return value, true
	}
	return ema(closes, period)
}

func previousEMAFromStateOrSeries(basic *basicIndicatorState, closes []float64, period int, offset int) (float64, bool) {
	if value, ok := basic.emaHistoricalValue(period, offset); ok {
		return value, true
	}
	if len(closes) <= offset {
		return 0, false
	}
	return ema(closes[:len(closes)-offset], period)
}
