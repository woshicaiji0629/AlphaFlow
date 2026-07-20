package indicatorcalc

import model "alphaflow/go-service/pkg/marketmodel"

type CalculationWindow struct {
	limit            int
	klines           []model.Kline
	opens            []float64
	highs            []float64
	lows             []float64
	closes           []float64
	volumes          []float64
	parseErr         error
	basic            *basicIndicatorState
	aiPrefix         *aiSourceState
	aiPreview        *aiSourceResult
	stream           bool
	close20          *rollingWindow
	high20           *rollingWindow
	low20            *rollingWindow
	quality          windowQualityState
	previewBase      []model.Kline
	previewBaseStart int
	previewKline     *model.Kline
}

type windowQualityState struct {
	invalidCount    int
	gapCount        int
	zeroVolumeCount int
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
	w.materializePreviewKlines()
	return w.cloneWithExtraCapacity(0, true, true)
}

// CloneForAppend returns an isolated clone with room for one additional kline.
// It avoids a second allocation when realtime preview immediately appends a
// temporary closed kline.
func (w *CalculationWindow) CloneForAppend() *CalculationWindow {
	w.materializePreviewKlines()
	return w.cloneWithExtraCapacity(1, false, true)
}

func (w *CalculationWindow) cloneWithExtraCapacity(extra int, cloneAIPrefix bool, cloneKlines bool) *CalculationWindow {
	if w == nil {
		return nil
	}
	cloned := &CalculationWindow{
		limit:     w.limit,
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
		quality:   w.quality,
	}
	if cloneKlines {
		cloned.klines = cloneSliceWithExtra(w.klines, extra)
	}
	if cloneAIPrefix {
		cloned.aiPrefix = w.aiPrefix
	}
	return cloned
}

func (w *CalculationWindow) RealtimePreview(kline model.Kline) *CalculationWindow {
	w.materializePreviewKlines()
	result, ok := w.previewAISource(kline)
	preview := w.cloneWithoutKlines(1)
	kline.IsClosed = true
	drop := 0
	if preview.limit > 0 && len(w.klines)+1 > preview.limit {
		drop = len(w.klines) + 1 - preview.limit
	}
	preview.previewBase = w.klines
	preview.previewBaseStart = drop
	preview.previewKline = &kline
	preview.quality = w.quality
	if drop > 0 {
		removeQualityPrefix(&preview.quality, w.klines, w.opens, w.highs, w.lows, w.closes, w.volumes, drop)
	}
	preview.appendSeries(kline)
	if preview.parseErr == nil {
		last := len(preview.closes) - 1
		preview.basic.append(preview.highs, preview.lows, preview.closes, preview.volumes)
		preview.basic.appendHeikinAshi(preview.opens[last], preview.highs[last], preview.lows[last], preview.closes[last])
		preview.addQualityForKline(kline, preview.opens[last], preview.highs[last], preview.lows[last], preview.closes[last], preview.volumes[last], w.lastStoredKline())
		if drop > 0 {
			preview.opens = trimFloatSeries(preview.opens, drop)
			preview.highs = trimFloatSeries(preview.highs, drop)
			preview.lows = trimFloatSeries(preview.lows, drop)
			preview.closes = trimFloatSeries(preview.closes, drop)
			preview.volumes = trimFloatSeries(preview.volumes, drop)
			preview.basic.trimSeries(preview.limit)
		}
	}
	if ok {
		preview.aiPreview = &result
	}
	return preview
}

func (w *CalculationWindow) cloneWithoutKlines(extra int) *CalculationWindow {
	return w.cloneWithExtraCapacity(extra, false, false)
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
	w.materializePreviewKlines()
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
				last := len(w.closes) - 1
				w.basic.appendHeikinAshi(w.opens[last], w.highs[last], w.lows[last], w.closes[last])
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
	w.materializePreviewKlines()
	return w.klines
}

func (w *CalculationWindow) materializePreviewKlines() {
	if w == nil || w.previewKline == nil {
		return
	}
	count := len(w.previewBase) - w.previewBaseStart + 1
	materialized := make([]model.Kline, 0, count)
	materialized = append(materialized, w.previewBase[w.previewBaseStart:]...)
	materialized = append(materialized, *w.previewKline)
	w.klines = materialized
	w.previewBase = nil
	w.previewBaseStart = 0
	w.previewKline = nil
}

func (w *CalculationWindow) klineCount() int {
	if w != nil && w.previewKline != nil {
		return len(w.previewBase) - w.previewBaseStart + 1
	}
	if w == nil {
		return 0
	}
	return len(w.klines)
}

func (w *CalculationWindow) lastStoredKline() model.Kline {
	if w != nil && len(w.klines) > 0 {
		return w.klines[len(w.klines)-1]
	}
	return model.Kline{}
}

func (w *CalculationWindow) lastKline() (model.Kline, bool) {
	if w == nil {
		return model.Kline{}, false
	}
	if w.previewKline != nil {
		return *w.previewKline, true
	}
	if len(w.klines) == 0 {
		return model.Kline{}, false
	}
	return w.klines[len(w.klines)-1], true
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
	if w.parseErr == nil {
		w.trimQuality(drop)
	}
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
	w.quality = windowQualityState{}

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
		w.appendQuality(len(w.closes) - 1)
	}
}

func (w *CalculationWindow) rebuildBasicState() {
	if w.parseErr != nil {
		w.basic = nil
		return
	}
	w.basic = buildBasicIndicatorStateWithOpens(w.opens, w.highs, w.lows, w.closes, w.volumes)
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
	w.appendQuality(len(w.closes) - 1)
	if w.close20 == nil {
		w.rebuildRollingWindows()
		return
	}
	w.close20.append(closeValue)
	w.high20.append(high)
	w.low20.append(low)
}

func (w *CalculationWindow) appendQuality(index int) {
	if w == nil || index < 0 || index >= len(w.klines) || index >= len(w.opens) || index >= len(w.highs) || index >= len(w.lows) || index >= len(w.closes) || index >= len(w.volumes) {
		return
	}
	if seriesKlineInvalid(w.klines[index], w.opens[index], w.highs[index], w.lows[index], w.closes[index]) {
		w.quality.invalidCount++
	}
	if w.volumes[index] == 0 {
		w.quality.zeroVolumeCount++
	}
	if index > 0 && klinesHaveGap(w.klines[index-1], w.klines[index]) {
		w.quality.gapCount++
	}
}

func (w *CalculationWindow) trimQuality(drop int) {
	if w == nil || drop <= 0 || len(w.klines) != len(w.closes) {
		return
	}
	if drop > len(w.klines) {
		drop = len(w.klines)
	}
	removeQualityPrefix(&w.quality, w.klines, w.opens, w.highs, w.lows, w.closes, w.volumes, drop)
}

func removeQualityPrefix(quality *windowQualityState, klines []model.Kline, opens []float64, highs []float64, lows []float64, closes []float64, volumes []float64, drop int) {
	if quality == nil || drop <= 0 || len(klines) != len(closes) {
		return
	}
	if drop > len(klines) {
		drop = len(klines)
	}
	for index := 0; index < drop; index++ {
		if seriesKlineInvalid(klines[index], opens[index], highs[index], lows[index], closes[index]) {
			quality.invalidCount--
		}
		if volumes[index] == 0 {
			quality.zeroVolumeCount--
		}
	}
	for right := 1; right <= drop && right < len(klines); right++ {
		if klinesHaveGap(klines[right-1], klines[right]) {
			quality.gapCount--
		}
	}
}

func (w *CalculationWindow) addQualityForKline(kline model.Kline, open float64, high float64, low float64, closeValue float64, volume float64, previous model.Kline) {
	if seriesKlineInvalid(kline, open, high, low, closeValue) {
		w.quality.invalidCount++
	}
	if volume == 0 {
		w.quality.zeroVolumeCount++
	}
	if klinesHaveGap(previous, kline) {
		w.quality.gapCount++
	}
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
