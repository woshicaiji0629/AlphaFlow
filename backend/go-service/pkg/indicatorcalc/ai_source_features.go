package indicatorcalc

import "math"

type aiSourceFeatureCache struct {
	points []aiSourceFeatureCachePoint
}

type aiSourceFeatureCachePoint struct {
	ema10      float64
	ema34      float64
	sma30      float64
	stddev30   float64
	stddev20   float64
	volLow100  float64
	volHigh100 float64
}

type aiSourceFeatureCursor struct {
	source        []float64
	ema10         aiSourceEMAState
	ema34         aiSourceEMAState
	sum30         float64
	sumSq30       float64
	sum20         float64
	sumSq20       float64
	volLowWindow  floatMonotonicWindow
	volHighWindow floatMonotonicWindow
}

func newAISourceFeatureCursor(source []float64) aiSourceFeatureCursor {
	return aiSourceFeatureCursor{
		source:        source,
		ema10:         *newAISourceEMAState(10),
		ema34:         *newAISourceEMAState(34),
		volLowWindow:  newFloatMonotonicWindow(false),
		volHighWindow: newFloatMonotonicWindow(true),
	}
}

func (c *aiSourceFeatureCursor) next(index int) aiSourceFeatureCachePoint {
	point := aiSourceFeatureCachePoint{}
	if c == nil || index < 0 || index >= len(c.source) {
		return point
	}
	value := c.source[index]
	point.ema10 = c.ema10.append(value)
	point.ema34 = c.ema34.append(value)
	c.sum30 += value
	c.sumSq30 += value * value
	if index >= 30 {
		drop := c.source[index-30]
		c.sum30 -= drop
		c.sumSq30 -= drop * drop
	}
	if index >= 29 {
		mean := c.sum30 / 30
		point.sma30 = mean
		point.stddev30 = math.Sqrt(math.Max(c.sumSq30/30-mean*mean, 0))
	}
	c.sum20 += value
	c.sumSq20 += value * value
	if index >= 20 {
		drop := c.source[index-20]
		c.sum20 -= drop
		c.sumSq20 -= drop * drop
	}
	if index >= 19 {
		mean := c.sum20 / 20
		point.stddev20 = math.Sqrt(math.Max(c.sumSq20/20-mean*mean, 0))
		c.volLowWindow.push(index, point.stddev20)
		c.volHighWindow.push(index, point.stddev20)
		c.volLowWindow.expireBefore(index - 99)
		c.volHighWindow.expireBefore(index - 99)
		point.volLow100, _ = c.volLowWindow.value()
		point.volHigh100, _ = c.volHighWindow.value()
	}
	return point
}

func newAISourceFeatureCache(source []float64) aiSourceFeatureCache {
	cache := aiSourceFeatureCache{
		points: make([]aiSourceFeatureCachePoint, len(source)),
	}
	ema10 := newAISourceEMAState(10)
	ema34 := newAISourceEMAState(34)
	sum30 := 0.0
	sumSq30 := 0.0
	sum20 := 0.0
	sumSq20 := 0.0
	volLowWindow := newFloatMonotonicWindow(false)
	volHighWindow := newFloatMonotonicWindow(true)
	for index, value := range source {
		point := &cache.points[index]
		point.ema10 = ema10.append(value)
		point.ema34 = ema34.append(value)
		sum30 += value
		sumSq30 += value * value
		if index >= 30 {
			drop := source[index-30]
			sum30 -= drop
			sumSq30 -= drop * drop
		}
		if index >= 29 {
			mean := sum30 / 30
			point.sma30 = mean
			point.stddev30 = math.Sqrt(math.Max(sumSq30/30-mean*mean, 0))
		}
		sum20 += value
		sumSq20 += value * value
		if index >= 20 {
			drop := source[index-20]
			sum20 -= drop
			sumSq20 -= drop * drop
		}
		if index >= 19 {
			mean := sum20 / 20
			point.stddev20 = math.Sqrt(math.Max(sumSq20/20-mean*mean, 0))
			volLowWindow.push(index, point.stddev20)
			volHighWindow.push(index, point.stddev20)
			volLowWindow.expireBefore(index - 99)
			volHighWindow.expireBefore(index - 99)
			point.volLow100, _ = volLowWindow.value()
			point.volHigh100, _ = volHighWindow.value()
		}
	}
	return cache
}

func aiSourceFeaturesFromCache(cache aiSourceFeatureCache, source []float64, highs []float64, lows []float64, index int, atrValue float64) ([6]float64, bool) {
	if index < 0 || index >= len(cache.points) {
		return [6]float64{}, false
	}
	return aiSourceFeaturesFromPoint(cache.points[index], source, highs, lows, index, atrValue)
}

func aiSourceFeaturesFromPoint(point aiSourceFeatureCachePoint, source []float64, highs []float64, lows []float64, index int, atrValue float64) ([6]float64, bool) {
	var result [6]float64
	if index < 100 || index >= len(source) || atrValue <= 0 {
		return result, false
	}
	fast := point.ema10
	slow := point.ema34
	mean := point.sma30
	dev := point.stddev30
	if dev == 0 || source[index-14] == 0 {
		return result, false
	}
	volNow := point.stddev20
	volLow := point.volLow100
	volHigh := point.volHigh100
	priceRange := highs[index] - lows[index]
	if priceRange == 0 {
		priceRange = 1
	}
	result[0] = clampFloat((fast-slow)/atrValue, -3, 3) / 3
	result[1] = clampFloat(-(source[index]-mean)/dev, -3, 3) / 3
	result[2] = clampFloat(((source[index]/source[index-14])-1)/0.05, -3, 3) / 3
	result[3] = scaleValue01(volNow, volLow, volHigh)*2 - 1
	result[4] = clampFloat(((source[index]-lows[index])/priceRange)*2-1, -1, 1)
	result[5] = clampFloat((source[index]-source[index-3])/atrValue, -3, 3) / 3
	return result, true
}

func aiSourceFeatures(source []float64, highs []float64, lows []float64, index int, atrValue float64) ([6]float64, bool) {
	var result [6]float64
	if index < 100 || index >= len(source) || atrValue <= 0 {
		return result, false
	}
	fast, okFast := ema(source[:index+1], 10)
	slow, okSlow := ema(source[:index+1], 34)
	mean, okMean := sma(source[:index+1], 30)
	dev, okDev := standardDeviation(source[:index+1], 30)
	if !okFast || !okSlow || !okMean || !okDev || dev == 0 || source[index-14] == 0 {
		return result, false
	}
	volNow, okVol := standardDeviation(source[:index+1], 20)
	if !okVol {
		return result, false
	}
	volLow := volNow
	volHigh := volNow
	for lookback := index - 99; lookback <= index; lookback++ {
		vol, ok := standardDeviation(source[:lookback+1], 20)
		if !ok {
			continue
		}
		volLow = math.Min(volLow, vol)
		volHigh = math.Max(volHigh, vol)
	}
	priceRange := highs[index] - lows[index]
	if priceRange == 0 {
		priceRange = 1
	}
	result[0] = clampFloat((fast-slow)/atrValue, -3, 3) / 3
	result[1] = clampFloat(-(source[index]-mean)/dev, -3, 3) / 3
	result[2] = clampFloat(((source[index]/source[index-14])-1)/0.05, -3, 3) / 3
	result[3] = scaleValue01(volNow, volLow, volHigh)*2 - 1
	result[4] = clampFloat(((source[index]-lows[index])/priceRange)*2-1, -1, 1)
	result[5] = clampFloat((source[index]-source[index-3])/atrValue, -3, 3) / 3
	return result, true
}
