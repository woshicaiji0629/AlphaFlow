package indicatorcalc

import model "alphaflow/go-service/pkg/marketmodel"

func (w *CalculationWindow) prepareAISourcePrefix() bool {
	if w == nil || len(w.closes) < 140 {
		return false
	}
	if w.aiPrefix != nil {
		return true
	}
	start := 0
	if w.limit > 0 && len(w.closes) >= w.limit {
		start = len(w.closes) - w.limit + 1
	}
	opens := w.opens[start:]
	highs := w.highs[start:]
	lows := w.lows[start:]
	closes := w.closes[start:]
	cfg := defaultAISourceConfig()
	atr14, ok14 := atrSeries(highs, lows, closes, 14)
	stATR, okST := atrSeries(highs, lows, closes, cfg.stLength)
	if !ok14 || !okST {
		return false
	}
	input := aiSourceInput{
		sources: [4][]float64{opens, highs, lows, closes}, highs: highs, lows: lows, closes: closes,
		atr14: atr14, atr14Offset: len(closes) - len(atr14),
		stATR: stATR, stATROffset: len(closes) - len(stATR), config: cfg,
	}
	state := newAISourceState(input)
	for index := range closes {
		state.append(input, index, appendAISourceState)
	}
	w.aiPrefix = state
	if result, ok := state.result(closes); ok {
		w.aiPreview = &result
	}
	return true
}

func (w *CalculationWindow) PrepareAISourcePrefix() bool {
	return w.prepareAISourcePrefix()
}

func (w *CalculationWindow) previewAISource(kline model.Kline) (aiSourceResult, bool) {
	if !w.prepareAISourcePrefix() {
		return aiSourceResult{}, false
	}
	open, err := parse(kline.Open)
	if err != nil {
		return aiSourceResult{}, false
	}
	high, err := parse(kline.High)
	if err != nil {
		return aiSourceResult{}, false
	}
	low, err := parse(kline.Low)
	if err != nil {
		return aiSourceResult{}, false
	}
	closeValue, err := parse(kline.Close)
	if err != nil {
		return aiSourceResult{}, false
	}
	cfg := defaultAISourceConfig()
	atr14Value, ok14 := nextAISourceATR(w.aiPrefix.input, high, low, 14)
	stATRValue, okST := nextAISourceATR(w.aiPrefix.input, high, low, cfg.stLength)
	if !ok14 || !okST {
		return aiSourceResult{}, false
	}
	state := w.aiPrefix.cloneWithExtraCapacity(1)
	return state.appendClosed(open, high, low, closeValue, atr14Value, stATRValue)
}

func nextAISourceATR(input aiSourceInput, high float64, low float64, period int) (float64, bool) {
	if period <= 0 || len(input.closes) == 0 {
		return 0, false
	}
	series := input.atr14
	if period == input.config.stLength {
		series = input.stATR
	}
	if len(series) == 0 {
		return 0, false
	}
	previousATR := series[len(series)-1]
	previousClose := input.closes[len(input.closes)-1]
	trueRange := maxFloat(high-low, absFloat(high-previousClose), absFloat(low-previousClose))
	return (previousATR*float64(period-1) + trueRange) / float64(period), true
}

func (w *CalculationWindow) appendAISourceState() {
	if w == nil || w.aiPrefix == nil || len(w.closes) == 0 {
		return
	}
	last := len(w.closes) - 1
	atr14Value, ok14 := atr(w.highs, w.lows, w.closes, 14)
	stATRValue, okST := atr(w.highs, w.lows, w.closes, defaultAISourceConfig().stLength)
	if !ok14 || !okST {
		w.aiPrefix = nil
		w.aiPreview = nil
		return
	}
	result, ok := w.aiPrefix.appendClosed(w.opens[last], w.highs[last], w.lows[last], w.closes[last], atr14Value, stATRValue)
	if ok {
		w.aiPreview = &result
	}
}
