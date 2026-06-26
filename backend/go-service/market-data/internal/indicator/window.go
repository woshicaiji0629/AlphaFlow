package indicator

import "alphaflow/go-service/market-data/internal/model"

type CalculationWindow struct {
	limit    int
	klines   []model.Kline
	opens    []float64
	highs    []float64
	lows     []float64
	closes   []float64
	volumes  []float64
	parseErr error
}

func NewCalculationWindow(limit int) *CalculationWindow {
	return &CalculationWindow{limit: limit}
}

func NewCalculationWindowFromKlines(klines []model.Kline, limit int) *CalculationWindow {
	window := NewCalculationWindow(limit)
	window.Reset(klines)
	return window
}

func (w *CalculationWindow) Reset(klines []model.Kline) {
	w.klines = w.klines[:0]
	for _, kline := range klines {
		if !kline.IsClosed {
			continue
		}
		w.klines = append(w.klines, kline)
	}
	w.trim()
	w.rebuildSeries()
}

func (w *CalculationWindow) Append(klines []model.Kline) {
	for _, kline := range klines {
		if !kline.IsClosed {
			continue
		}
		w.klines = append(w.klines, kline)
		if w.parseErr == nil {
			w.appendSeries(kline)
		}
	}
	w.trim()
	if w.parseErr != nil {
		w.rebuildSeries()
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

func (w *CalculationWindow) trim() {
	if w.limit <= 0 || len(w.klines) <= w.limit {
		return
	}
	drop := len(w.klines) - w.limit
	w.klines = w.klines[len(w.klines)-w.limit:]
	w.opens = trimFloatSeries(w.opens, drop)
	w.highs = trimFloatSeries(w.highs, drop)
	w.lows = trimFloatSeries(w.lows, drop)
	w.closes = trimFloatSeries(w.closes, drop)
	w.volumes = trimFloatSeries(w.volumes, drop)
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
	}
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
}
