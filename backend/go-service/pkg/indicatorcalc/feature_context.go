package indicatorcalc

import "math"

type cachedFloatSeries struct {
	values []float64
	ok     bool
}

type cachedFloat struct {
	value float64
	ok    bool
}

type cachedBollinger struct {
	upper  float64
	middle float64
	lower  float64
	ok     bool
}

type cachedMinMax struct {
	high float64
	low  float64
	ok   bool
}

// featureContext caches reusable numeric foundations for one CalculateWindow
// call. It is intentionally internal: strategies consume stable output fields,
// while complex indicators share unformatted numeric series here.
type featureContext struct {
	highs  []float64
	lows   []float64
	closes []float64
	basic  *basicIndicatorState
	window *CalculationWindow

	atrByPeriod   map[int]cachedFloatSeries
	trueRange     cachedFloatSeries
	emaByPeriod   map[int]cachedFloat
	bbByPeriod    map[int]cachedBollinger
	rangeByPeriod map[int]cachedMinMax
}

func (c *featureContext) trueRangeSeries() ([]float64, bool) {
	if c == nil || len(c.closes) == 0 || len(c.highs) != len(c.closes) || len(c.lows) != len(c.closes) {
		return nil, false
	}
	if c.trueRange.values != nil {
		return c.trueRange.values, c.trueRange.ok
	}
	values := trueRangeSeries(c.highs, c.lows, c.closes)
	c.trueRange = cachedFloatSeries{values: values, ok: len(values) == len(c.closes)}
	return c.trueRange.values, c.trueRange.ok
}

func newFeatureContext(highs []float64, lows []float64, closes []float64, basic *basicIndicatorState) *featureContext {
	return newFeatureContextWithWindow(highs, lows, closes, basic, nil)
}

func newFeatureContextWithWindow(highs []float64, lows []float64, closes []float64, basic *basicIndicatorState, window *CalculationWindow) *featureContext {
	return &featureContext{
		highs: highs, lows: lows, closes: closes, basic: basic, window: window,
		atrByPeriod:   make(map[int]cachedFloatSeries),
		emaByPeriod:   make(map[int]cachedFloat),
		bbByPeriod:    make(map[int]cachedBollinger),
		rangeByPeriod: make(map[int]cachedMinMax),
	}
}

func (c *featureContext) bollinger(period int, multiplier float64) (float64, float64, float64, bool) {
	if c == nil || multiplier != 2 {
		return 0, 0, 0, false
	}
	if cached, ok := c.bbByPeriod[period]; ok {
		return cached.upper, cached.middle, cached.lower, cached.ok
	}
	if period == 20 && c.window != nil && c.window.close20 != nil {
		middle, variance, ok := c.window.close20.meanVariance()
		if ok {
			deviation := math.Sqrt(variance)
			upper, lower := middle+multiplier*deviation, middle-multiplier*deviation
			c.bbByPeriod[period] = cachedBollinger{upper: upper, middle: middle, lower: lower, ok: true}
			return upper, middle, lower, true
		}
	}
	upper, middle, lower, ok := bollinger(c.closes, period, multiplier)
	c.bbByPeriod[period] = cachedBollinger{upper: upper, middle: middle, lower: lower, ok: ok}
	return upper, middle, lower, ok
}

func (c *featureContext) donchian(period int) (float64, float64, bool) {
	if c == nil {
		return 0, 0, false
	}
	if cached, ok := c.rangeByPeriod[period]; ok {
		return cached.high, cached.low, cached.ok
	}
	if period == 20 && c.window != nil && c.window.high20 != nil && c.window.low20 != nil {
		high, _, highOK := c.window.high20.rangeValues()
		_, low, lowOK := c.window.low20.rangeValues()
		if highOK && lowOK {
			c.rangeByPeriod[period] = cachedMinMax{high: high, low: low, ok: true}
			return high, low, true
		}
	}
	high, low, ok := donchian(c.highs, c.lows, period)
	c.rangeByPeriod[period] = cachedMinMax{high: high, low: low, ok: ok}
	return high, low, ok
}

func (c *featureContext) emaValue(period int) (float64, bool) {
	if c == nil {
		return 0, false
	}
	if cached, ok := c.emaByPeriod[period]; ok {
		return cached.value, cached.ok
	}
	value, ok := 0.0, false
	if c.basic != nil {
		value, ok = c.basic.emaValue(period)
	}
	if !ok {
		value, ok = ema(c.closes, period)
	}
	c.emaByPeriod[period] = cachedFloat{value: value, ok: ok}
	return value, ok
}

func (c *featureContext) emaHistoricalValue(period int, offset int) (float64, bool) {
	if c == nil {
		return 0, false
	}
	return previousEMAFromStateOrSeries(c.basic, c.closes, period, offset)
}

func (c *featureContext) atrSeries(period int) ([]float64, bool) {
	if c == nil {
		return nil, false
	}
	if cached, ok := c.atrByPeriod[period]; ok {
		return cached.values, cached.ok
	}
	var values []float64
	var ok bool
	if period == 14 && c.basic != nil {
		values, ok = c.basic.atrSeries14()
	}
	if !ok {
		trueRanges, rangesOK := c.trueRangeSeries()
		if rangesOK && len(trueRanges) > 1 {
			values, ok = atrSeriesFromTrueRanges(trueRanges[1:], period)
		}
	}
	c.atrByPeriod[period] = cachedFloatSeries{values: values, ok: ok}
	return values, ok
}

func (c *featureContext) atrValue(period int) (float64, bool) {
	values, ok := c.atrSeries(period)
	if !ok || len(values) == 0 {
		return 0, false
	}
	return values[len(values)-1], true
}
