package indicatorcalc

type basicIndicatorState struct {
	smaPeriods    []int
	smaValues     map[int]float64
	ema           map[int]*streamEMAState
	volumeSMA     map[int]float64
	rsi14         streamRSIState
	atr14         streamATRState
	adaptiveST    streamAdaptiveSupertrendState
	adx14         streamADXState
	waveTrend     streamWaveTrendState
	moneyFlow     streamMoneyFlowState
	macd          map[macdConfig]*streamMACDState
	stc           streamSTCState
	obv           float64
	vwapWeighted  float64
	vwapVolumeSum float64
}

func (s *basicIndicatorState) trimSeries(limit int) {
	if s == nil || limit <= 0 {
		return
	}
	s.rsi14.series = trimTail(s.rsi14.series, limit)
	s.atr14.series = trimTail(s.atr14.series, limit)
	s.adaptiveST.atr.series = trimTail(s.adaptiveST.atr.series, maxInt(s.adaptiveST.trainingPeriod, limit))
	s.adx14.dxValues = trimTail(s.adx14.dxValues, maxInt(s.adx14.period, limit))
	for _, state := range s.macd {
		state.differences = trimTail(state.differences, limit)
		state.series = trimTail(state.series, limit)
	}
}

func trimTail[T any](values []T, limit int) []T {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	copy(values, values[len(values)-limit:])
	return values[:limit]
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
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
		adaptiveST: newStreamAdaptiveSupertrendState(10, 3, 100),
		adx14:      newStreamADXState(14),
		waveTrend:  newStreamWaveTrendState(10, 21),
		moneyFlow:  streamMoneyFlowState{},
		macd:       macdStates,
		stc:        newStreamSTCState(),
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
	return s.cloneWithExtraCapacity(0)
}

func (s *basicIndicatorState) cloneWithExtraCapacity(extra int) *basicIndicatorState {
	if s == nil {
		return nil
	}
	cloned := &basicIndicatorState{
		smaPeriods:    append([]int(nil), s.smaPeriods...),
		smaValues:     make(map[int]float64, len(s.smaValues)),
		ema:           make(map[int]*streamEMAState, len(s.ema)),
		volumeSMA:     make(map[int]float64, len(s.volumeSMA)),
		rsi14:         s.rsi14.cloneWithExtraCapacity(extra),
		atr14:         s.atr14.cloneWithExtraCapacity(extra),
		adaptiveST:    s.adaptiveST.cloneWithExtraCapacity(extra),
		adx14:         s.adx14.cloneWithExtraCapacity(extra),
		waveTrend:     s.waveTrend.clone(),
		moneyFlow:     s.moneyFlow,
		macd:          make(map[macdConfig]*streamMACDState, len(s.macd)),
		stc:           s.stc,
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
		cloned.macd[config] = state.cloneWithExtraCapacity(extra)
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
	s.stc.append(closeValue)
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
		s.adaptiveST.append(highs[last], lows[last], closes[last-1], closeValue)
		s.adx14.append(highs[last], lows[last], highs[last-1], lows[last-1], closes[last-1])
		s.moneyFlow.append(highs[last], lows[last], closeValue, volume, closes[last-1], true)
	} else {
		s.moneyFlow.append(highs[last], lows[last], closeValue, volume, 0, false)
	}
	typical := (highs[last] + lows[last] + closeValue) / 3
	s.waveTrend.append(typical)
	s.vwapWeighted += typical * volume
	s.vwapVolumeSum += volume
}

func (s *basicIndicatorState) adaptiveSupertrendValue() (adaptiveSupertrendState, bool) {
	if s == nil || s.adaptiveST.pointCount < 2 {
		return adaptiveSupertrendState{}, false
	}
	stream := s.adaptiveST
	return adaptiveSupertrendState{
		points: []trendPoint{stream.previousPoint, stream.currentPoint}, assignedATR: stream.cluster.assignedATR,
		highCentroid: stream.cluster.highCentroid, midCentroid: stream.cluster.midCentroid,
		lowCentroid: stream.cluster.lowCentroid, cluster: stream.cluster.cluster,
	}, true
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

func (s *basicIndicatorState) waveTrendValue() (float64, float64, float64, float64, float64, bool) {
	if s == nil {
		return 0, 0, 0, 0, 0, false
	}
	return s.waveTrend.value()
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

func (s *basicIndicatorState) moneyFlowValues() (float64, float64, float64, float64, float64, float64, bool) {
	if s == nil {
		return 0, 0, 0, 0, 0, 0, false
	}
	return s.moneyFlow.values()
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
