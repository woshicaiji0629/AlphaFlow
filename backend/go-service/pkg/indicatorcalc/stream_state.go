package indicatorcalc

type macdConfig struct {
	fast   int
	slow   int
	signal int
}

type streamMACDState struct {
	fastEMA     streamEMAState
	slowEMA     streamEMAState
	differences []float64
	signalEMA   streamEMAState
	series      []macdPoint
}

type streamEMAState struct {
	period      int
	sum         float64
	count       int
	value       float64
	previous    float64
	hasPrevious bool
	ready       bool
}

type streamRSIState struct {
	period  int
	count   int
	avgGain float64
	avgLoss float64
	ready   bool
	series  []float64
}

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

type basicIndicatorState struct {
	smaPeriods    []int
	smaValues     map[int]float64
	ema           map[int]*streamEMAState
	volumeSMA     map[int]float64
	rsi14         streamRSIState
	atr14         streamATRState
	adx14         streamADXState
	macd          map[macdConfig]*streamMACDState
	obv           float64
	vwapWeighted  float64
	vwapVolumeSum float64
}

func newBasicIndicatorState() *basicIndicatorState {
	emaPeriods := []int{5, 7, 8, 9, 10, 12, 13, 19, 25, 26, 34, 55, 89, 99, 144, 200}
	emas := make(map[int]*streamEMAState, len(emaPeriods))
	for _, period := range emaPeriods {
		emas[period] = newStreamEMAState(period)
	}
	configs := []macdConfig{
		{fast: 12, slow: 26, signal: 9},
		{fast: 7, slow: 19, signal: 9},
	}
	macdStates := make(map[macdConfig]*streamMACDState, len(configs))
	for _, config := range configs {
		macdStates[config] = newStreamMACDState(config)
	}
	return &basicIndicatorState{
		smaPeriods: []int{7, 20, 25, 99},
		smaValues:  map[int]float64{},
		ema:        emas,
		volumeSMA:  map[int]float64{},
		rsi14:      newStreamRSIState(14),
		atr14:      newStreamATRState(14),
		adx14:      newStreamADXState(14),
		macd:       macdStates,
	}
}

func buildBasicIndicatorState(highs []float64, lows []float64, closes []float64, volumes []float64) *basicIndicatorState {
	state := newBasicIndicatorState()
	for index := range closes {
		state.append(highs[:index+1], lows[:index+1], closes[:index+1], volumes[:index+1])
	}
	return state
}

func (s *basicIndicatorState) clone() *basicIndicatorState {
	if s == nil {
		return nil
	}
	cloned := &basicIndicatorState{
		smaPeriods:    append([]int(nil), s.smaPeriods...),
		smaValues:     make(map[int]float64, len(s.smaValues)),
		ema:           make(map[int]*streamEMAState, len(s.ema)),
		volumeSMA:     make(map[int]float64, len(s.volumeSMA)),
		rsi14:         s.rsi14.clone(),
		atr14:         s.atr14.clone(),
		adx14:         s.adx14.clone(),
		macd:          make(map[macdConfig]*streamMACDState, len(s.macd)),
		obv:           s.obv,
		vwapWeighted:  s.vwapWeighted,
		vwapVolumeSum: s.vwapVolumeSum,
	}
	for period, state := range s.ema {
		cloned.ema[period] = state.clone()
	}
	for period, value := range s.smaValues {
		cloned.smaValues[period] = value
	}
	for period, value := range s.volumeSMA {
		cloned.volumeSMA[period] = value
	}
	for config, state := range s.macd {
		cloned.macd[config] = state.clone()
	}
	return cloned
}

func (s *basicIndicatorState) append(highs []float64, lows []float64, closes []float64, volumes []float64) {
	if s == nil || len(closes) == 0 || len(highs) != len(closes) || len(lows) != len(closes) || len(volumes) != len(closes) {
		return
	}
	last := len(closes) - 1
	closeValue := closes[last]
	volume := volumes[last]
	for _, state := range s.ema {
		state.append(closeValue)
	}
	for _, state := range s.macd {
		state.append(closeValue)
	}
	for _, period := range s.smaPeriods {
		if value, ok := sma(closes, period); ok {
			s.smaValues[period] = value
		}
	}
	if value, ok := sma(volumes, 20); ok {
		s.volumeSMA[20] = value
	}
	if last > 0 {
		switch {
		case closeValue > closes[last-1]:
			s.obv += volume
		case closeValue < closes[last-1]:
			s.obv -= volume
		}
		s.rsi14.append(closes[last-1], closeValue)
		s.atr14.append(highs[last], lows[last], closes[last-1])
		s.adx14.append(highs[last], lows[last], highs[last-1], lows[last-1], closes[last-1])
	}
	typical := (highs[last] + lows[last] + closeValue) / 3
	s.vwapWeighted += typical * volume
	s.vwapVolumeSum += volume
}

func (s *basicIndicatorState) sma(period int) (float64, bool) {
	if s == nil {
		return 0, false
	}
	value, ok := s.smaValues[period]
	return value, ok
}

func (s *basicIndicatorState) volumeSMAValue(period int) (float64, bool) {
	if s == nil {
		return 0, false
	}
	value, ok := s.volumeSMA[period]
	return value, ok
}

func (s *basicIndicatorState) emaValue(period int) (float64, bool) {
	if s == nil {
		return 0, false
	}
	state, ok := s.ema[period]
	if !ok || !state.ready {
		return 0, false
	}
	return state.value, true
}

func (s *basicIndicatorState) previousEMAValue(period int) (float64, bool) {
	if s == nil {
		return 0, false
	}
	state, ok := s.ema[period]
	if !ok || !state.hasPrevious {
		return 0, false
	}
	return state.previous, true
}

func (s *basicIndicatorState) rsiSeries14() ([]float64, bool) {
	if s == nil || len(s.rsi14.series) == 0 {
		return nil, false
	}
	return s.rsi14.series, true
}

func (s *basicIndicatorState) atrSeries14() ([]float64, bool) {
	if s == nil || len(s.atr14.series) == 0 {
		return nil, false
	}
	return s.atr14.series, true
}

func (s *basicIndicatorState) adx14Value() (float64, float64, float64, bool) {
	if s == nil || !s.adx14.ready {
		return 0, 0, 0, false
	}
	return s.adx14.value, s.adx14.plusDI, s.adx14.minusDI, true
}

func (s *basicIndicatorState) macdSeries(config macdConfig) ([]macdPoint, bool) {
	if s == nil {
		return nil, false
	}
	state, ok := s.macd[config]
	if !ok || len(state.series) == 0 {
		return nil, false
	}
	return state.series, true
}

func (s *basicIndicatorState) obvValue() (float64, bool) {
	if s == nil {
		return 0, false
	}
	return s.obv, true
}

func (s *basicIndicatorState) vwapValue(fallback float64) (float64, bool) {
	if s == nil {
		return 0, false
	}
	if s.vwapVolumeSum == 0 {
		return fallback, true
	}
	return s.vwapWeighted / s.vwapVolumeSum, true
}

func newStreamEMAState(period int) *streamEMAState {
	return &streamEMAState{period: period}
}

func (s *streamEMAState) clone() *streamEMAState {
	if s == nil {
		return nil
	}
	return &streamEMAState{
		period:      s.period,
		sum:         s.sum,
		count:       s.count,
		value:       s.value,
		previous:    s.previous,
		hasPrevious: s.hasPrevious,
		ready:       s.ready,
	}
}

func (s *streamEMAState) append(value float64) {
	if s == nil || s.period <= 0 {
		return
	}
	if !s.ready {
		s.sum += value
		s.count++
		if s.count == s.period {
			s.value = s.sum / float64(s.period)
			s.ready = true
		}
		return
	}
	multiplier := 2 / float64(s.period+1)
	s.previous = s.value
	s.hasPrevious = true
	s.value = (value-s.value)*multiplier + s.value
}

func newStreamRSIState(period int) streamRSIState {
	return streamRSIState{period: period}
}

func (s streamRSIState) clone() streamRSIState {
	s.series = append([]float64(nil), s.series...)
	return s
}

func (s *streamRSIState) append(previous float64, current float64) {
	if s == nil || s.period <= 0 {
		return
	}
	delta := current - previous
	gain := 0.0
	loss := 0.0
	if delta >= 0 {
		gain = delta
	} else {
		loss = -delta
	}
	if !s.ready {
		s.avgGain += gain
		s.avgLoss += loss
		s.count++
		if s.count == s.period {
			s.avgGain /= float64(s.period)
			s.avgLoss /= float64(s.period)
			s.ready = true
			s.series = append(s.series, rsiFromAverages(s.avgGain, s.avgLoss))
		}
		return
	}
	s.avgGain = (s.avgGain*float64(s.period-1) + gain) / float64(s.period)
	s.avgLoss = (s.avgLoss*float64(s.period-1) + loss) / float64(s.period)
	s.series = append(s.series, rsiFromAverages(s.avgGain, s.avgLoss))
}

func newStreamATRState(period int) streamATRState {
	return streamATRState{period: period}
}

func (s streamATRState) clone() streamATRState {
	s.series = append([]float64(nil), s.series...)
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
	s.dxValues = append([]float64(nil), s.dxValues...)
	return s
}

func (s *streamADXState) append(
	currentHigh float64,
	currentLow float64,
	previousHigh float64,
	previousLow float64,
	previousClose float64,
) {
	if s == nil || s.period <= 0 {
		return
	}
	trueRange := maxFloat(
		currentHigh-currentLow,
		absFloat(currentHigh-previousClose),
		absFloat(currentLow-previousClose),
	)
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

func newStreamMACDState(config macdConfig) *streamMACDState {
	return &streamMACDState{
		fastEMA:   *newStreamEMAState(config.fast),
		slowEMA:   *newStreamEMAState(config.slow),
		signalEMA: *newStreamEMAState(config.signal),
	}
}

func (s *streamMACDState) clone() *streamMACDState {
	if s == nil {
		return nil
	}
	return &streamMACDState{
		fastEMA:     *s.fastEMA.clone(),
		slowEMA:     *s.slowEMA.clone(),
		differences: append([]float64(nil), s.differences...),
		signalEMA:   *s.signalEMA.clone(),
		series:      append([]macdPoint(nil), s.series...),
	}
}

func (s *streamMACDState) append(value float64) {
	if s == nil {
		return
	}
	s.fastEMA.append(value)
	s.slowEMA.append(value)
	if !s.fastEMA.ready || !s.slowEMA.ready {
		return
	}
	difference := s.fastEMA.value - s.slowEMA.value
	s.differences = append(s.differences, difference)
	s.signalEMA.append(difference)
	if !s.signalEMA.ready {
		return
	}
	s.series = append(s.series, macdPoint{
		value:  difference,
		signal: s.signalEMA.value,
		hist:   difference - s.signalEMA.value,
	})
}
