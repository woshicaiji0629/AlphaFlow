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
	case "hammer", "inverted_hammer", "bullish_engulfing", "piercing_line", "morning_star", "three_white_soldiers", "marubozu_bull":
		signals["candle_bias"] = "bull"
	case "shooting_star", "bearish_engulfing", "dark_cloud_cover", "evening_star", "three_black_crows", "marubozu_bear":
		signals["candle_bias"] = "bear"
	default:
		if current.bullish && current.bodyRatio >= 0.45 {
			signals["candle_bias"] = "bull"
		} else if current.bearish && current.bodyRatio >= 0.45 {
			signals["candle_bias"] = "bear"
		}
	}
	switch {
	case pattern == "three_white_soldiers" || pattern == "three_black_crows" || pattern == "morning_star" || pattern == "evening_star":
		signals["candle_strength"] = "strong"
	case current.bodyRatio >= 0.6 || pattern == "bullish_engulfing" || pattern == "bearish_engulfing":
		signals["candle_strength"] = "medium"
	}
}
