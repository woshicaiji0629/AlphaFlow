package indicatorcalc

var basicEMAPeriods = []int{5, 7, 8, 9, 10, 12, 13, 19, 21, 25, 26, 34, 55, 89, 99, 144, 200}
var basicSMAPeriods = []int{7, 20, 25, 99}
var basicVolumeSMAPeriods = []int{20}
var basicMACDConfigs = []macdConfig{{fast: 12, slow: 26, signal: 9}, {fast: 7, slow: 19, signal: 9}}

type basicSMAState struct {
	period int
	value  float64
	ready  bool
}

type basicMACDState struct {
	config macdConfig
	state  streamMACDState
}

type basicIndicatorState struct {
	smaStates        []basicSMAState
	ema              []streamEMAState
	demaTema21       streamDEMATEMAState
	volumeSMA        []basicSMAState
	rsi14            streamRSIState
	atr14            streamATRState
	atr22            streamATRState
	adaptiveST       streamAdaptiveSupertrendState
	alphaTrend14     streamAlphaTrendState
	dynamicSwingVWAP streamDynamicSwingVWAPState
	volumeFlow130    streamVolumeFlowIndicatorState
	heikinAshi       streamHeikinAshiState
	adx14            streamADXState
	waveTrend        streamWaveTrendState
	moneyFlow        streamMoneyFlowState
	macd             []basicMACDState
	stc              streamSTCState
	psar             streamPSARState
	kama10           streamKAMAState
	hma21            streamHMA21State
	emd25            streamEMDState
	ssl10            streamSSL10State
	rangeFilter100   streamRangeFilterState
	williamsVixFix   streamWilliamsVixFixState
	tdSequential     streamTDSequentialState
	nadarayaWatson   streamNadarayaWatsonState
	utBot10          streamUTBotState
	qqe6             streamQQEState
	alligatorJaw     streamSMMAState
	alligatorTeeth   streamSMMAState
	alligatorLips    streamSMMAState
	obv              float64
	vwapWeighted     float64
	vwapVolumeSum    float64
}

func (s *basicIndicatorState) trimSeries(limit int) {
	if s == nil || limit <= 0 {
		return
	}
	s.rsi14.series = trimTail(s.rsi14.series, limit)
	s.atr14.series = trimTail(s.atr14.series, limit)
	s.atr22.series = trimTail(s.atr22.series, limit)
	s.adaptiveST.atr.series = trimTail(s.adaptiveST.atr.series, maxInt(s.adaptiveST.trainingPeriod, limit))
	s.alphaTrend14.points = trimTail(s.alphaTrend14.points, maxInt(4, limit-s.alphaTrend14.period))
	s.adx14.dxValues = trimTail(s.adx14.dxValues, maxInt(s.adx14.period, limit))
	for index := range s.macd {
		s.macd[index].state.differences = trimTail(s.macd[index].state.differences, limit)
		s.macd[index].state.series = trimTail(s.macd[index].state.series, limit)
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
	emas := make([]streamEMAState, len(basicEMAPeriods))
	for index, period := range basicEMAPeriods {
		emas[index] = streamEMAState{period: period}
	}
	smaStates := make([]basicSMAState, len(basicSMAPeriods))
	for index, period := range basicSMAPeriods {
		smaStates[index].period = period
	}
	volumeSMAStates := make([]basicSMAState, len(basicVolumeSMAPeriods))
	for index, period := range basicVolumeSMAPeriods {
		volumeSMAStates[index].period = period
	}
	macdStates := make([]basicMACDState, len(basicMACDConfigs))
	for index, config := range basicMACDConfigs {
		macdStates[index] = basicMACDState{config: config, state: *newStreamMACDState(config)}
	}
	return &basicIndicatorState{
		smaStates:        smaStates,
		ema:              emas,
		demaTema21:       newStreamDEMATEMAState(21),
		volumeSMA:        volumeSMAStates,
		rsi14:            newStreamRSIState(14),
		atr14:            newStreamATRState(14),
		atr22:            newStreamATRState(22),
		adaptiveST:       newStreamAdaptiveSupertrendState(10, 3, 100),
		alphaTrend14:     newStreamAlphaTrendState(14, 1),
		dynamicSwingVWAP: newStreamDynamicSwingVWAPState(dynamicSwingVWAPPeriod, dynamicSwingVWAPBaseAPT),
		volumeFlow130:    newStreamVolumeFlowIndicatorState(130, 0.2, 2.5, 5),
		adx14:            newStreamADXState(14),
		waveTrend:        newStreamWaveTrendState(10, 21),
		moneyFlow:        streamMoneyFlowState{},
		macd:             macdStates,
		stc:              newStreamSTCState(),
		psar:             newStreamPSARState(0.02, 0.2),
		emd25:            newStreamEMDState(25),
		ssl10:            newStreamSSL10State(),
		rangeFilter100:   newStreamRangeFilterState(100, 3),
		nadarayaWatson:   newStreamNadarayaWatsonState(8),
		utBot10:          newStreamUTBotState(1),
		qqe6:             newStreamQQEState(),
		alligatorJaw:     newStreamSMMAState(13),
		alligatorTeeth:   newStreamSMMAState(8),
		alligatorLips:    newStreamSMMAState(5),
	}
}

func buildBasicIndicatorState(highs []float64, lows []float64, closes []float64, volumes []float64) *basicIndicatorState {
	state := newBasicIndicatorState()
	for index := range closes {
		state.append(highs[:index+1], lows[:index+1], closes[:index+1], volumes[:index+1])
	}
	return state
}

func buildBasicIndicatorStateWithOpens(opens []float64, highs []float64, lows []float64, closes []float64, volumes []float64) *basicIndicatorState {
	state := buildBasicIndicatorState(highs, lows, closes, volumes)
	if len(opens) != len(closes) || len(highs) != len(closes) || len(lows) != len(closes) {
		return state
	}
	for index := range closes {
		state.heikinAshi.append(opens[index], highs[index], lows[index], closes[index])
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
		smaStates:        append([]basicSMAState(nil), s.smaStates...),
		ema:              append([]streamEMAState(nil), s.ema...),
		demaTema21:       s.demaTema21,
		volumeSMA:        append([]basicSMAState(nil), s.volumeSMA...),
		rsi14:            s.rsi14.cloneWithExtraCapacity(extra),
		atr14:            s.atr14.cloneWithExtraCapacity(extra),
		atr22:            s.atr22.cloneWithExtraCapacity(extra),
		adaptiveST:       s.adaptiveST.cloneWithExtraCapacity(extra),
		alphaTrend14:     s.alphaTrend14.cloneWithExtraCapacity(extra),
		dynamicSwingVWAP: s.dynamicSwingVWAP,
		volumeFlow130:    s.volumeFlow130,
		heikinAshi:       s.heikinAshi,
		adx14:            s.adx14.cloneWithExtraCapacity(extra),
		waveTrend:        s.waveTrend.clone(),
		moneyFlow:        s.moneyFlow,
		macd:             append([]basicMACDState(nil), s.macd...),
		stc:              s.stc,
		psar:             s.psar,
		kama10:           s.kama10,
		hma21:            s.hma21,
		emd25:            s.emd25,
		ssl10:            s.ssl10,
		rangeFilter100:   s.rangeFilter100,
		williamsVixFix:   s.williamsVixFix,
		tdSequential:     s.tdSequential,
		nadarayaWatson:   s.nadarayaWatson,
		utBot10:          s.utBot10,
		qqe6:             s.qqe6,
		alligatorJaw:     s.alligatorJaw,
		alligatorTeeth:   s.alligatorTeeth,
		alligatorLips:    s.alligatorLips,
		obv:              s.obv,
		vwapWeighted:     s.vwapWeighted,
		vwapVolumeSum:    s.vwapVolumeSum,
	}
	for index := range s.macd {
		cloned.macd[index].state.differences = cloneSliceWithExtra(s.macd[index].state.differences, extra)
		cloned.macd[index].state.series = cloneSliceWithExtra(s.macd[index].state.series, extra)
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
	var ema21 *streamEMAState
	for index := range s.ema {
		s.ema[index].append(closeValue)
		if s.ema[index].period == 21 {
			ema21 = &s.ema[index]
		}
	}
	s.demaTema21.append(ema21)
	for index := range s.macd {
		s.macd[index].state.append(closeValue)
	}
	s.stc.append(closeValue)
	s.psar.append(highs[last], lows[last], closeValue)
	s.kama10.append(closeValue)
	s.hma21.append(closeValue)
	s.emd25.append(closeValue)
	s.ssl10.append(highs[last], lows[last], closeValue)
	s.rangeFilter100.append(closeValue)
	s.williamsVixFix.append(lows[last], closeValue)
	s.tdSequential.append(closeValue)
	s.nadarayaWatson.append(closeValue)
	s.alligatorJaw.append(closeValue)
	s.alligatorTeeth.append(closeValue)
	s.alligatorLips.append(closeValue)
	s.alphaTrend14.append(highs, lows, closes, volumes)
	s.dynamicSwingVWAP.append(highs, lows, closes, volumes)
	s.volumeFlow130.append(highs, lows, closes, volumes)
	for index := range s.smaStates {
		if value, ok := sma(closes, s.smaStates[index].period); ok {
			s.smaStates[index].value = value
			s.smaStates[index].ready = true
		}
	}
	for index := range s.volumeSMA {
		if value, ok := sma(volumes, s.volumeSMA[index].period); ok {
			s.volumeSMA[index].value = value
			s.volumeSMA[index].ready = true
		}
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
		s.atr22.append(highs[last], lows[last], closes[last-1])
		s.adaptiveST.append(highs[last], lows[last], closes[last-1], closeValue)
		s.utBot10.append(closeValue, s.adaptiveST.atr.value, s.adaptiveST.atr.ready)
		s.qqe6.append(closes[last-1], closeValue)
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

func (s *basicIndicatorState) atrValue(period int) (float64, bool) {
	if s == nil {
		return 0, false
	}
	switch period {
	case 14:
		return s.atr14.value, s.atr14.ready
	case 22:
		return s.atr22.value, s.atr22.ready
	default:
		return 0, false
	}
}

func (s *basicIndicatorState) alphaTrendValues(period int, multiplier float64) ([]trendPoint, float64, bool) {
	if s == nil || period != s.alphaTrend14.period || multiplier != s.alphaTrend14.multiplier {
		return nil, 0, false
	}
	return s.alphaTrend14.value()
}

func (s *basicIndicatorState) dynamicSwingVWAPValue(period int, baseAPT float64, useAdapt bool, volBias float64) (dynamicSwingVWAPState, bool) {
	if s == nil {
		return dynamicSwingVWAPState{}, false
	}
	return s.dynamicSwingVWAP.value(period, baseAPT, useAdapt, volBias)
}

func (s *basicIndicatorState) volumeFlowIndicatorValue(length int, coef float64, volumeCoef float64, signalLength int) (volumeFlowIndicatorResult, bool) {
	if s == nil {
		return volumeFlowIndicatorResult{}, false
	}
	return s.volumeFlow130.value(length, coef, volumeCoef, signalLength)
}

func (s *basicIndicatorState) appendHeikinAshi(open float64, high float64, low float64, closeValue float64) {
	if s == nil {
		return
	}
	s.heikinAshi.append(open, high, low, closeValue)
}

func (s *basicIndicatorState) heikinAshiValue() (heikinAshiCandle, bool) {
	if s == nil {
		return heikinAshiCandle{}, false
	}
	return s.heikinAshi.value()
}

func (s *basicIndicatorState) psarValue() (float64, string, bool) {
	if s == nil {
		return 0, "", false
	}
	return s.psar.value()
}

func (s *basicIndicatorState) kama10Value() (float64, bool) {
	if s == nil {
		return 0, false
	}
	return s.kama10.value()
}

func (s *basicIndicatorState) hma21Value() (float64, bool) {
	if s == nil {
		return 0, false
	}
	return s.hma21.value()
}

func (s *basicIndicatorState) hma21Previous3Value() (float64, bool) {
	if s == nil {
		return 0, false
	}
	return s.hma21.previous3()
}

func (s *basicIndicatorState) emd25Value(period int) (float64, float64, float64, float64, bool) {
	if s == nil || period != 25 {
		return 0, 0, 0, 0, false
	}
	return s.emd25.value()
}

func (s *basicIndicatorState) ssl10Value(period int) (float64, float64, string, string, bool) {
	if s == nil || period != 10 {
		return 0, 0, "", "", false
	}
	return s.ssl10.value()
}

func (s *basicIndicatorState) rangeFilterValue(period int, multiplier float64) (float64, float64, float64, string, bool) {
	if s == nil || period != 100 || multiplier != 3 {
		return 0, 0, 0, "", false
	}
	return s.rangeFilter100.value()
}

func (s *basicIndicatorState) williamsVixFixValue(period int, bbLength int, bbMultiplier float64, lookback int, percentileHigh float64) (williamsVixFixResult, bool) {
	if s == nil || period != 22 || bbLength != 20 || bbMultiplier != 2 || lookback != 50 || percentileHigh != 0.85 {
		return williamsVixFixResult{}, false
	}
	return s.williamsVixFix.value()
}

func (s *basicIndicatorState) tdSequentialValue() (int, int, string, bool) {
	if s == nil {
		return 0, 0, "", false
	}
	buyCount, sellCount, exhaustion := s.tdSequential.value()
	return buyCount, sellCount, exhaustion, true
}

func (s *basicIndicatorState) nadarayaWatsonValue(length int, bandwidth float64) (float64, float64, float64, bool) {
	if s == nil || length != 50 || bandwidth != 8 {
		return 0, 0, 0, false
	}
	return s.nadarayaWatson.value()
}

func (s *basicIndicatorState) utBotValue(period int, multiplier float64) (float64, string, string, bool) {
	if s == nil || period != 10 || multiplier != 1 {
		return 0, "", "", false
	}
	return s.utBot10.value()
}

func (s *basicIndicatorState) qqe6State() *streamQQEState {
	if s == nil {
		return nil
	}
	return &s.qqe6
}

func (s *basicIndicatorState) alligatorValue() (float64, float64, float64, bool) {
	if s == nil {
		return 0, 0, 0, false
	}
	jaw, jawOK := s.alligatorJaw.value()
	teeth, teethOK := s.alligatorTeeth.value()
	lips, lipsOK := s.alligatorLips.value()
	return jaw, teeth, lips, jawOK && teethOK && lipsOK
}

func (s *basicIndicatorState) sma(period int) (float64, bool) {
	if s == nil {
		return 0, false
	}
	state := findBasicSMAState(s.smaStates, period)
	if state == nil || !state.ready {
		return 0, false
	}
	return state.value, true
}

func (s *basicIndicatorState) volumeSMAValue(period int) (float64, bool) {
	if s == nil {
		return 0, false
	}
	state := findBasicSMAState(s.volumeSMA, period)
	if state == nil || !state.ready {
		return 0, false
	}
	return state.value, true
}

func findBasicSMAState(states []basicSMAState, period int) *basicSMAState {
	for index := range states {
		if states[index].period == period {
			return &states[index]
		}
	}
	return nil
}

func (s *basicIndicatorState) emaValue(period int) (float64, bool) {
	state, ok := s.emaState(period)
	if !ok || !state.ready {
		return 0, false
	}
	return state.value, true
}

func (s *basicIndicatorState) demaTema21Value() (float64, float64, bool, bool) {
	if s == nil {
		return 0, 0, false, false
	}
	state, _ := s.emaState(21)
	return s.demaTema21.value(state)
}

func (s *basicIndicatorState) previousEMAValue(period int) (float64, bool) {
	return s.emaHistoricalValue(period, 1)
}

func (s *basicIndicatorState) emaHistoricalValue(period int, offset int) (float64, bool) {
	state, ok := s.emaState(period)
	if !ok {
		return 0, false
	}
	return state.historicalValue(offset)
}

func (s *basicIndicatorState) emaState(period int) (*streamEMAState, bool) {
	if s == nil {
		return nil, false
	}
	for index := range s.ema {
		if s.ema[index].period == period {
			return &s.ema[index], true
		}
	}
	return nil, false
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
	for index := range s.macd {
		if s.macd[index].config == config && len(s.macd[index].state.series) > 0 {
			return s.macd[index].state.series, true
		}
	}
	return nil, false
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
