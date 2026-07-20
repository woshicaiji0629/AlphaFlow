package indicatorcalc

import model "alphaflow/go-service/pkg/marketmodel"

type CalculationWindow struct {
	limit     int
	klines    []model.Kline
	opens     []float64
	highs     []float64
	lows      []float64
	closes    []float64
	volumes   []float64
	parseErr  error
	basic     *basicIndicatorState
	aiPrefix  *aiSourceState
	aiPreview *aiSourceResult
	stream    bool
	close20   *rollingWindow
	high20    *rollingWindow
	low20     *rollingWindow
}

func NewCalculationWindow(limit int) *CalculationWindow {
	return &CalculationWindow{limit: limit}
}

func NewCalculationWindowFromKlines(klines []model.Kline, limit int) *CalculationWindow {
	window := NewCalculationWindow(limit)
	window.Reset(klines)
	return window
}

func (w *CalculationWindow) Clone() *CalculationWindow {
	return w.cloneWithExtraCapacity(0, true)
}

// CloneForAppend returns an isolated clone with room for one additional kline.
// It avoids a second allocation when realtime preview immediately appends a
// temporary closed kline.
func (w *CalculationWindow) CloneForAppend() *CalculationWindow {
	return w.cloneWithExtraCapacity(1, false)
}

func (w *CalculationWindow) cloneWithExtraCapacity(extra int, cloneAIPrefix bool) *CalculationWindow {
	if w == nil {
		return nil
	}
	cloned := &CalculationWindow{
		limit:     w.limit,
		klines:    cloneSliceWithExtra(w.klines, extra),
		opens:     cloneSliceWithExtra(w.opens, extra),
		highs:     cloneSliceWithExtra(w.highs, extra),
		lows:      cloneSliceWithExtra(w.lows, extra),
		closes:    cloneSliceWithExtra(w.closes, extra),
		volumes:   cloneSliceWithExtra(w.volumes, extra),
		parseErr:  w.parseErr,
		basic:     w.basic.cloneWithExtraCapacity(extra),
		aiPreview: w.aiPreview,
		stream:    w.stream,
		close20:   w.close20.clone(),
		high20:    w.high20.clone(),
		low20:     w.low20.clone(),
	}
	if cloneAIPrefix {
		cloned.aiPrefix = w.aiPrefix
	}
	return cloned
}

func (w *CalculationWindow) RealtimePreview(kline model.Kline) *CalculationWindow {
	result, ok := w.previewAISource(kline)
	preview := w.CloneForAppend()
	kline.IsClosed = true
	preview.Append([]model.Kline{kline})
	if ok {
		preview.aiPreview = &result
	}
	return preview
}

func cloneSliceWithExtra[T any](values []T, extra int) []T {
	if extra < 0 {
		extra = 0
	}
	if len(values) == 0 && extra == 0 {
		return nil
	}
	cloned := make([]T, len(values), len(values)+extra)
	copy(cloned, values)
	return cloned
}

func (w *CalculationWindow) EnableBasicState() {
	if w == nil {
		return
	}
	w.stream = true
	w.rebuildBasicState()
}

func (w *CalculationWindow) Reset(klines []model.Kline) {
	w.aiPrefix = nil
	w.aiPreview = nil
	w.klines = w.klines[:0]
	for _, kline := range klines {
		if !kline.IsClosed {
			continue
		}
		w.klines = append(w.klines, kline)
	}
	w.trim()
	w.rebuildSeries()
	w.basic = nil
}

func (w *CalculationWindow) Append(klines []model.Kline) {
	w.append(klines, w.stream)
}

func (w *CalculationWindow) append(klines []model.Kline, maintainBasicState bool) {
	for _, kline := range klines {
		if !kline.IsClosed {
			continue
		}
		w.klines = append(w.klines, kline)
		if w.parseErr == nil {
			w.appendSeries(kline)
			if maintainBasicState && w.parseErr == nil && w.basic != nil {
				w.basic.append(w.highs, w.lows, w.closes, w.volumes)
			}
			if maintainBasicState && w.aiPrefix != nil {
				w.appendAISourceState()
			}
		}
	}
	trimmed := w.trim()
	if w.parseErr != nil {
		w.rebuildSeries()
	}
	if !maintainBasicState {
		w.basic = nil
		return
	}
	if w.parseErr != nil {
		w.rebuildBasicState()
		return
	}
	if w.basic == nil {
		w.rebuildBasicState()
		return
	}
	if trimmed {
		w.basic.trimSeries(w.limit)
	}
}

func (w *CalculationWindow) Klines() []model.Kline {
	return w.klines
}

func (w *CalculationWindow) LastOpenTime() (int64, bool) {
	if len(w.klines) == 0 {
		return 0, false
	}
	return w.klines[len(w.klines)-1].OpenTime, true
}

func (w *CalculationWindow) Series() ([]float64, []float64, []float64, []float64, []float64, error) {
	if w.parseErr != nil {
		return nil, nil, nil, nil, nil, w.parseErr
	}
	return w.opens, w.highs, w.lows, w.closes, w.volumes, nil
}

func (w *CalculationWindow) trim() bool {
	if w.limit <= 0 || len(w.klines) <= w.limit {
		return false
	}
	drop := len(w.klines) - w.limit
	w.klines = w.klines[len(w.klines)-w.limit:]
	w.opens = trimFloatSeries(w.opens, drop)
	w.highs = trimFloatSeries(w.highs, drop)
	w.lows = trimFloatSeries(w.lows, drop)
	w.closes = trimFloatSeries(w.closes, drop)
	w.volumes = trimFloatSeries(w.volumes, drop)
	return true
}

func trimFloatSeries(values []float64, drop int) []float64 {
	if drop <= 0 || len(values) == 0 {
		return values
	}
	if drop >= len(values) {
		return values[:0]
	}
	return values[drop:]
}

func (w *CalculationWindow) rebuildSeries() {
	w.opens = w.opens[:0]
	w.highs = w.highs[:0]
	w.lows = w.lows[:0]
	w.closes = w.closes[:0]
	w.volumes = w.volumes[:0]
	w.parseErr = nil
	w.close20 = newRollingWindow(20)
	w.high20 = newRollingWindow(20)
	w.low20 = newRollingWindow(20)

	for _, kline := range w.klines {
		open, err := parse(kline.Open)
		if err != nil {
			w.parseErr = err
			return
		}
		high, err := parse(kline.High)
		if err != nil {
			w.parseErr = err
			return
		}
		low, err := parse(kline.Low)
		if err != nil {
			w.parseErr = err
			return
		}
		closeValue, err := parse(kline.Close)
		if err != nil {
			w.parseErr = err
			return
		}
		volume, err := parse(kline.Volume)
		if err != nil {
			w.parseErr = err
			return
		}
		w.opens = append(w.opens, open)
		w.highs = append(w.highs, high)
		w.lows = append(w.lows, low)
		w.closes = append(w.closes, closeValue)
		w.volumes = append(w.volumes, volume)
		w.close20.append(closeValue)
		w.high20.append(high)
		w.low20.append(low)
	}
}

func (w *CalculationWindow) rebuildBasicState() {
	if w.parseErr != nil {
		w.basic = nil
		return
	}
	w.basic = buildBasicIndicatorState(w.highs, w.lows, w.closes, w.volumes)
}

func (w *CalculationWindow) appendSeries(kline model.Kline) {
	open, err := parse(kline.Open)
	if err != nil {
		w.parseErr = err
		return
	}
	high, err := parse(kline.High)
	if err != nil {
		w.parseErr = err
		return
	}
	low, err := parse(kline.Low)
	if err != nil {
		w.parseErr = err
		return
	}
	closeValue, err := parse(kline.Close)
	if err != nil {
		w.parseErr = err
		return
	}
	volume, err := parse(kline.Volume)
	if err != nil {
		w.parseErr = err
		return
	}
	w.opens = append(w.opens, open)
	w.highs = append(w.highs, high)
	w.lows = append(w.lows, low)
	w.closes = append(w.closes, closeValue)
	w.volumes = append(w.volumes, volume)
	if w.close20 == nil {
		w.rebuildRollingWindows()
		return
	}
	w.close20.append(closeValue)
	w.high20.append(high)
	w.low20.append(low)
}

func (w *CalculationWindow) rebuildRollingWindows() {
	w.close20 = newRollingWindow(20)
	w.high20 = newRollingWindow(20)
	w.low20 = newRollingWindow(20)
	for index := range w.closes {
		w.close20.append(w.closes[index])
		w.high20.append(w.highs[index])
		w.low20.append(w.lows[index])
	}
}
