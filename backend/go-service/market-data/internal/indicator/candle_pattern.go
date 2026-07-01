package indicator

import "math"

func addCandlePatterns(signals map[string]string, opens []float64, highs []float64, lows []float64, closes []float64) {
	signals["candle_pattern"] = "none"
	signals["candle_bias"] = "neutral"
	signals["candle_strength"] = "weak"
	last := len(closes) - 1
	current, ok := candle(opens, highs, lows, closes, last)
	if !ok {
		return
	}

	pattern := singleCandlePattern(current)
	if len(closes) >= 2 {
		previous, ok := candle(opens, highs, lows, closes, last-1)
		if ok {
			pattern = preferPattern(pattern, doubleCandlePattern(previous, current))
		}
	}
	if len(closes) >= 3 {
		first, okFirst := candle(opens, highs, lows, closes, last-2)
		second, okSecond := candle(opens, highs, lows, closes, last-1)
		if okFirst && okSecond {
			pattern = preferPattern(pattern, tripleCandlePattern(first, second, current))
		}
	}
	pattern = preferPattern(pattern, scriptCandlePattern(opens, highs, lows, closes))
	if pattern != "" {
		signals["candle_pattern"] = pattern
	}
	if current.upperRatio >= 0.45 || current.lowerRatio >= 0.45 {
		signals["pin_bar"] = "true"
	}
	setCandleContext(signals, pattern, current)
}

type candleInfo struct {
	open       float64
	high       float64
	low        float64
	close      float64
	body       float64
	bodyRatio  float64
	rangeValue float64
	upperRatio float64
	lowerRatio float64
	bullish    bool
	bearish    bool
}

func candle(opens []float64, highs []float64, lows []float64, closes []float64, index int) (candleInfo, bool) {
	rangeValue := highs[index] - lows[index]
	if rangeValue <= 0 {
		return candleInfo{}, false
	}
	body := math.Abs(closes[index] - opens[index])
	upper := highs[index] - math.Max(opens[index], closes[index])
	lower := math.Min(opens[index], closes[index]) - lows[index]
	return candleInfo{
		open:       opens[index],
		high:       highs[index],
		low:        lows[index],
		close:      closes[index],
		body:       body,
		bodyRatio:  body / rangeValue,
		rangeValue: rangeValue,
		upperRatio: upper / rangeValue,
		lowerRatio: lower / rangeValue,
		bullish:    closes[index] > opens[index],
		bearish:    closes[index] < opens[index],
	}, true
}

func singleCandlePattern(current candleInfo) string {
	switch {
	case current.lowerRatio >= 0.55 && current.upperRatio <= 0.2 && current.bodyRatio <= 0.35:
		return "hammer"
	case current.upperRatio >= 0.55 && current.lowerRatio <= 0.2 && current.bodyRatio <= 0.35:
		if current.bearish {
			return "shooting_star"
		}
		return "inverted_hammer"
	case current.bodyRatio <= 0.1:
		return "doji"
	case current.bullish && current.bodyRatio >= 0.8:
		return "marubozu_bull"
	case current.bearish && current.bodyRatio >= 0.8:
		return "marubozu_bear"
	default:
		return ""
	}
}

func doubleCandlePattern(previous candleInfo, current candleInfo) string {
	previousMid := (previous.open + previous.close) / 2
	switch {
	case current.bullish && previous.bearish && current.close >= previous.open && current.open <= previous.close:
		return "bullish_engulfing"
	case current.bearish && previous.bullish && current.close <= previous.open && current.open >= previous.close:
		return "bearish_engulfing"
	case current.high < previous.high && current.low > previous.low:
		return "inside_bar"
	case current.high > previous.high && current.low < previous.low:
		return "outside_bar"
	case previous.bearish && current.bullish && current.open < previous.low && current.close > previousMid && current.close < previous.open:
		return "piercing_line"
	case previous.bullish && current.bearish && current.open > previous.high && current.close < previousMid && current.close > previous.open:
		return "dark_cloud_cover"
	default:
		return ""
	}
}

func tripleCandlePattern(first candleInfo, second candleInfo, third candleInfo) string {
	firstMid := (first.open + first.close) / 2
	switch {
	case first.bearish && first.bodyRatio >= 0.45 && second.bodyRatio <= 0.3 && third.bullish && third.close > firstMid:
		return "morning_star"
	case first.bullish && first.bodyRatio >= 0.45 && second.bodyRatio <= 0.3 && third.bearish && third.close < firstMid:
		return "evening_star"
	case first.bullish && second.bullish && third.bullish &&
		first.bodyRatio >= 0.45 && second.bodyRatio >= 0.45 && third.bodyRatio >= 0.45 &&
		second.close > first.close && third.close > second.close:
		return "three_white_soldiers"
	case first.bearish && second.bearish && third.bearish &&
		first.bodyRatio >= 0.45 && second.bodyRatio >= 0.45 && third.bodyRatio >= 0.45 &&
		second.close < first.close && third.close < second.close:
		return "three_black_crows"
	default:
		return ""
	}
}

func preferPattern(current string, next string) string {
	if next == "" {
		return current
	}
	return next
}

func setCandleContext(signals map[string]string, pattern string, current candleInfo) {
	switch pattern {
	case "hammer", "inverted_hammer", "bullish_engulfing", "piercing_line", "morning_star", "three_white_soldiers", "marubozu_bull", "doji_bottom", "bottom_formation", "red_three_soldiers":
		signals["candle_bias"] = "bull"
	case "shooting_star", "bearish_engulfing", "dark_cloud_cover", "evening_star", "three_black_crows", "marubozu_bear", "hanging_man", "doji_top", "top_formation", "black_three_soldiers":
		signals["candle_bias"] = "bear"
	default:
		if current.bullish && current.bodyRatio >= 0.45 {
			signals["candle_bias"] = "bull"
		} else if current.bearish && current.bodyRatio >= 0.45 {
			signals["candle_bias"] = "bear"
		}
	}
	switch {
	case pattern == "three_white_soldiers" || pattern == "three_black_crows" || pattern == "morning_star" || pattern == "evening_star" || pattern == "red_three_soldiers" || pattern == "black_three_soldiers":
		signals["candle_strength"] = "strong"
	case current.bodyRatio >= 0.6 || pattern == "bullish_engulfing" || pattern == "bearish_engulfing":
		signals["candle_strength"] = "medium"
	}
}

func scriptCandlePattern(opens []float64, highs []float64, lows []float64, closes []float64) string {
	last := len(closes) - 1
	if last < 2 {
		return ""
	}
	highestHigh := highs[last-1] > highest(highs, last-2, 5)
	lowestLow := lows[last-1] < lowest(lows, last-2, 5)
	amplitude := 0.0
	if closes[last-1] != 0 {
		amplitude = (highs[last] - lows[last]) / closes[last-1]
	}
	solid := math.Abs(closes[last] - opens[last])
	wave := 0.0
	if opens[last] != 0 {
		wave = math.Abs(closes[last]-opens[last]) / opens[last]
	}
	upperShadow := highs[last] - math.Max(opens[last], closes[last])
	lowerShadow := math.Min(closes[last], opens[last]) - lows[last]

	switch {
	case lowerShadow >= 2*solid && amplitude > 0.005 && lows[last] < lowest(lows, last-1, 10) && upperShadow < 0.5*solid:
		return "hammer"
	case lowerShadow >= 2*solid && amplitude > 0.005 && highs[last-1] > highest(highs, last-2, 10) && upperShadow < 0.5*solid:
		return "hanging_man"
	case upperShadow >= 2*solid && amplitude > 0.005 && highs[last] > highest(highs, last-1, 10) && lowerShadow < 0.5*solid:
		return "shooting_star"
	case absRatio(highs[last], highs[last-1]) <= 0.000015 && highestHigh:
		return "doji_top"
	case absRatio(lows[last], lows[last-1]) <= 0.000015 && lowestLow:
		return "doji_bottom"
	case darkCloudCover(opens, closes, highestHigh, last):
		return "dark_cloud_cover"
	case piercingLine(opens, closes, lowestLow, last):
		return "piercing_line"
	case redThreeSoldiers(opens, closes, last):
		return "red_three_soldiers"
	case blackThreeSoldiers(opens, closes, last):
		return "black_three_soldiers"
	case topFormation(opens, highs, lows, closes, last):
		return "top_formation"
	case bottomFormation(highs, lows, closes, last):
		return "bottom_formation"
	case bullishScriptEngulfing(opens, highs, lows, closes, wave, last):
		return "bullish_engulfing"
	case bearishScriptEngulfing(opens, highs, lows, closes, wave, last):
		return "bearish_engulfing"
	default:
		return ""
	}
}

func darkCloudCover(opens []float64, closes []float64, highestHigh bool, last int) bool {
	return opens[last-1] < closes[last-1] && opens[last] > closes[last] && opens[last] > closes[last-1] &&
		closes[last] > opens[last-1] && closes[last] < (opens[last-1]+closes[last-1])/2 && highestHigh
}

func piercingLine(opens []float64, closes []float64, lowestLow bool, last int) bool {
	return opens[last-1] > closes[last-1] && opens[last] < closes[last] && opens[last] < closes[last-1] &&
		closes[last] < opens[last-1] && closes[last] > (opens[last-1]+closes[last-1])/2 && lowestLow
}

func bullishScriptEngulfing(opens []float64, highs []float64, lows []float64, closes []float64, wave float64, last int) bool {
	return opens[last-1] > closes[last-1] && closes[last] > opens[last] && closes[last] > opens[last-1] &&
		opens[last] < closes[last-1] && wave > 0.005 && lows[last] < lows[last-1] && highs[last] >= highs[last-1]
}

func bearishScriptEngulfing(opens []float64, highs []float64, lows []float64, closes []float64, wave float64, last int) bool {
	return opens[last-1] < closes[last-1] && closes[last] < opens[last] && closes[last] < opens[last-1] &&
		opens[last] > closes[last-1] && wave > 0.005 && highs[last] > highs[last-1] && lows[last] <= lows[last-1]
}

func redThreeSoldiers(opens []float64, closes []float64, last int) bool {
	return closes[last] > opens[last] && closes[last-1] > opens[last-1] && closes[last-2] > opens[last-2] &&
		closes[last] > closes[last-1] && closes[last-1] > closes[last-2] &&
		bodyWave(opens[last-2], closes[last-2]) > 0.003 && bodyWave(opens[last-1], closes[last-1]) > 0.003 && bodyWave(opens[last], closes[last]) > 0.003
}

func blackThreeSoldiers(opens []float64, closes []float64, last int) bool {
	return closes[last] < opens[last] && closes[last-1] < opens[last-1] && closes[last-2] < opens[last-2] &&
		closes[last] < closes[last-1] && closes[last-1] < closes[last-2] &&
		bodyWave(opens[last-2], closes[last-2]) > 0.003 && bodyWave(opens[last-1], closes[last-1]) > 0.003 && bodyWave(opens[last], closes[last]) > 0.003
}

func topFormation(opens []float64, highs []float64, lows []float64, closes []float64, last int) bool {
	return highs[last-1] > highs[last-2] && highs[last-1] > highs[last] &&
		highs[last-1] > highest(highs, last-2, 10) && closes[last] < closes[last-1] &&
		opens[last-1] > opens[last-2] && lows[last-2] < lows[last-1] && lows[last-1] > lows[last]
}

func bottomFormation(highs []float64, lows []float64, closes []float64, last int) bool {
	return lows[last-1] < lows[last-2] && lows[last-1] < lows[last] &&
		lows[last-1] < lowest(lows, last-2, 10) && closes[last] > highs[last-1] &&
		highs[last-2] > highs[last-1] && highs[last] > highs[last-1]
}

func bodyWave(open float64, closeValue float64) float64 {
	if open == 0 {
		return 0
	}
	return math.Abs(closeValue-open) / open
}

func absRatio(current float64, previous float64) float64 {
	if previous == 0 {
		return 0
	}
	return math.Abs(current-previous) / previous
}

func highest(values []float64, end int, length int) float64 {
	if end < 0 {
		end = 0
	}
	start := end - length + 1
	if start < 0 {
		start = 0
	}
	result := values[start]
	for index := start + 1; index <= end; index++ {
		if values[index] > result {
			result = values[index]
		}
	}
	return result
}

func lowest(values []float64, end int, length int) float64 {
	if end < 0 {
		end = 0
	}
	start := end - length + 1
	if start < 0 {
		start = 0
	}
	result := values[start]
	for index := start + 1; index <= end; index++ {
		if values[index] < result {
			result = values[index]
		}
	}
	return result
}
