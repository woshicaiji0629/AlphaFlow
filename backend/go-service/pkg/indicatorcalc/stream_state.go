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

type streamAdaptiveSupertrendState struct {
	atr            streamATRState
	trainingPeriod int
	multiplier     float64
	finalUpper     float64
	finalLower     float64
	direction      string
	previousPoint  trendPoint
	currentPoint   trendPoint
	cluster        volatilityCluster
	pointCount     int
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

type streamWaveTrendState struct {
	channelEMA   streamEMAState
	deviationEMA streamEMAState
	ciEMA        streamEMAState
	values       [5]float64
	count        int
}

type streamMoneyFlowState struct {
	obv      float64
	obvLast5 [5]float64
	obvCount int
	pvt      float64
	pvtLast5 [5]float64
	pvtCount int
	adLine   float64
	adLast5  [5]float64
	adCount  int
}

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

func newStreamAdaptiveSupertrendState(period int, multiplier float64, trainingPeriod int) streamAdaptiveSupertrendState {
	return streamAdaptiveSupertrendState{atr: newStreamATRState(period), multiplier: multiplier, trainingPeriod: trainingPeriod}
}

func (s streamAdaptiveSupertrendState) cloneWithExtraCapacity(extra int) streamAdaptiveSupertrendState {
	s.atr = s.atr.cloneWithExtraCapacity(extra)
	return s
}

func (s *streamAdaptiveSupertrendState) append(high float64, low float64, previousClose float64, closeValue float64) {
	if s == nil {
		return
	}
	s.atr.append(high, low, previousClose)
	if len(s.atr.series) < s.trainingPeriod {
		return
	}
	values := s.atr.series[len(s.atr.series)-s.trainingPeriod:]
	cluster, ok := adaptiveVolatilityCluster(values, s.atr.value)
	if !ok {
		return
	}
	mid := (high + low) / 2
	basicUpper := mid + s.multiplier*cluster.assignedATR
	basicLower := mid - s.multiplier*cluster.assignedATR
	if s.pointCount == 0 {
		s.finalUpper = basicUpper
		s.finalLower = basicLower
		s.direction = "down"
		if closeValue >= mid {
			s.direction = "up"
		}
	} else {
		if basicUpper < s.finalUpper || previousClose > s.finalUpper {
			s.finalUpper = basicUpper
		}
		if basicLower > s.finalLower || previousClose < s.finalLower {
			s.finalLower = basicLower
		}
		if s.direction == "down" && closeValue > s.finalUpper {
			s.direction = "up"
		} else if s.direction == "up" && closeValue < s.finalLower {
			s.direction = "down"
		}
	}
	s.previousPoint = s.currentPoint
	s.currentPoint = supertrendPoint(s.finalUpper, s.finalLower, s.direction)
	s.cluster = cluster
	s.pointCount++
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
	return s.cloneWithExtraCapacity(0)
}

func (s streamRSIState) cloneWithExtraCapacity(extra int) streamRSIState {
	s.series = cloneSliceWithExtra(s.series, extra)
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

func newStreamWaveTrendState(channelLength int, averageLength int) streamWaveTrendState {
	return streamWaveTrendState{
		channelEMA:   *newStreamEMAState(channelLength),
		deviationEMA: *newStreamEMAState(channelLength),
		ciEMA:        *newStreamEMAState(averageLength),
	}
}

func (s streamWaveTrendState) clone() streamWaveTrendState {
	return s
}

func (s *streamWaveTrendState) append(typical float64) {
	if s == nil {
		return
	}
	s.channelEMA.append(typical)
	if !s.channelEMA.ready {
		return
	}
	deviation := absFloat(typical - s.channelEMA.value)
	s.deviationEMA.append(deviation)
	if !s.deviationEMA.ready {
		return
	}
	ci := 0.0
	if s.deviationEMA.value != 0 {
		ci = (typical - s.channelEMA.value) / (0.015 * s.deviationEMA.value)
	}
	s.ciEMA.append(ci)
	if !s.ciEMA.ready {
		return
	}
	if s.count < len(s.values) {
		s.values[s.count] = s.ciEMA.value
		s.count++
		return
	}
	copy(s.values[:], s.values[1:])
	s.values[len(s.values)-1] = s.ciEMA.value
}

func (s *streamWaveTrendState) value() (float64, float64, float64, float64, float64, bool) {
	if s == nil || s.count < len(s.values) {
		return 0, 0, 0, 0, 0, false
	}
	wt1 := s.values[4]
	previousWT1 := s.values[3]
	wt2 := (s.values[1] + s.values[2] + s.values[3] + s.values[4]) / 4
	previousWT2 := (s.values[0] + s.values[1] + s.values[2] + s.values[3]) / 4
	return wt1, wt2, previousWT1, previousWT2, previousWT1 - previousWT2, true
}

func (s *streamMoneyFlowState) append(high float64, low float64, closeValue float64, volume float64, previousClose float64, hasPrevious bool) {
	if s == nil {
		return
	}
	if hasPrevious {
		switch {
		case closeValue > previousClose:
			s.obv += volume
		case closeValue < previousClose:
			s.obv -= volume
		}
		if previousClose != 0 {
			s.pvt += (closeValue - previousClose) / previousClose * volume
		}
	}
	s.adLine += moneyFlowVolume(high, low, closeValue, volume)
	s.obvCount = appendFixedFloat5(&s.obvLast5, s.obvCount, s.obv)
	s.pvtCount = appendFixedFloat5(&s.pvtLast5, s.pvtCount, s.pvt)
	s.adCount = appendFixedFloat5(&s.adLast5, s.adCount, s.adLine)
}

func (s *streamMoneyFlowState) values() (float64, float64, float64, float64, float64, float64, bool) {
	if s == nil || s.obvCount < len(s.obvLast5) || s.pvtCount < len(s.pvtLast5) || s.adCount < len(s.adLast5) {
		return 0, 0, 0, 0, 0, 0, false
	}
	return s.obv, slopeFixedFloat5(s.obvLast5), s.pvt, slopeFixedFloat5(s.pvtLast5), s.adLine, slopeFixedFloat5(s.adLast5), true
}

func appendFixedFloat5(values *[5]float64, count int, value float64) int {
	if count < len(values) {
		values[count] = value
		return count + 1
	}
	copy(values[:], values[1:])
	values[len(values)-1] = value
	return count
}

func slopeFixedFloat5(values [5]float64) float64 {
	return values[len(values)-1] - values[0]
}

func newStreamMACDState(config macdConfig) *streamMACDState {
	return &streamMACDState{
		fastEMA:   *newStreamEMAState(config.fast),
		slowEMA:   *newStreamEMAState(config.slow),
		signalEMA: *newStreamEMAState(config.signal),
	}
}

func (s *streamMACDState) clone() *streamMACDState {
	return s.cloneWithExtraCapacity(0)
}

func (s *streamMACDState) cloneWithExtraCapacity(extra int) *streamMACDState {
	if s == nil {
		return nil
	}
	return &streamMACDState{
		fastEMA:     *s.fastEMA.clone(),
		slowEMA:     *s.slowEMA.clone(),
		differences: cloneSliceWithExtra(s.differences, extra),
		signalEMA:   *s.signalEMA.clone(),
		series:      cloneSliceWithExtra(s.series, extra),
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
