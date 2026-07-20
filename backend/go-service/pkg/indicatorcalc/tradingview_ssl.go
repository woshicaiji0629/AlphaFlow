package indicatorcalc

type streamSSL10State struct {
	highs             [10]float64
	lows              [10]float64
	windowCount       int
	sampleCount       int
	upper             float64
	lower             float64
	direction         string
	previousDirection string
}

func newStreamSSL10State() streamSSL10State {
	return streamSSL10State{direction: "neutral", previousDirection: "neutral"}
}

func (s *streamSSL10State) append(high float64, low float64, closeValue float64) {
	if s == nil {
		return
	}
	s.sampleCount++
	if s.windowCount < len(s.highs) {
		s.highs[s.windowCount] = high
		s.lows[s.windowCount] = low
		s.windowCount++
		if s.windowCount < len(s.highs) {
			return
		}
	} else {
		copy(s.highs[:len(s.highs)-1], s.highs[1:])
		copy(s.lows[:len(s.lows)-1], s.lows[1:])
		s.highs[len(s.highs)-1] = high
		s.lows[len(s.lows)-1] = low
	}
	var highSum, lowSum float64
	for index := range s.highs {
		highSum += s.highs[index]
		lowSum += s.lows[index]
	}
	highMA := highSum / float64(len(s.highs))
	lowMA := lowSum / float64(len(s.lows))
	s.previousDirection = s.direction
	switch {
	case closeValue > highMA:
		s.direction = "bull"
	case closeValue < lowMA:
		s.direction = "bear"
	}
	if s.direction == "bear" {
		s.upper, s.lower = lowMA, highMA
	} else {
		s.upper, s.lower = highMA, lowMA
	}
}

func (s *streamSSL10State) value() (float64, float64, string, string, bool) {
	if s == nil || s.sampleCount < len(s.highs)+1 {
		return 0, 0, "", "", false
	}
	return s.upper, s.lower, s.direction, s.previousDirection, true
}

func addSSLChannelFeatures(target *ValueSet, values map[string]string, signals map[string]string, highs []float64, lows []float64, closes []float64, period int, basic *basicIndicatorState) {
	upper, lower, direction, previousDirection, ok := basic.ssl10Value(period)
	if !ok {
		upper, lower, direction, previousDirection, ok = sslChannel(highs, lows, closes, period)
	}
	if !ok {
		return
	}
	setValueTarget(target, values, "ssl_upper", upper, true)
	setValueTarget(target, values, "ssl_lower", lower, true)
	setValueTarget(target, values, "ssl_width_pct", (upper-lower)/closes[len(closes)-1]*100, closes[len(closes)-1] != 0)
	signals["ssl_direction"] = direction
	signals["ssl_cross"] = directionFlipCross(previousDirection, direction)
}

func sslChannel(highs []float64, lows []float64, closes []float64, period int) (float64, float64, string, string, bool) {
	if period <= 0 || len(closes) < period+1 || len(highs) != len(closes) || len(lows) != len(closes) {
		return 0, 0, "", "", false
	}
	direction := "neutral"
	previousDirection := direction
	var upper float64
	var lower float64
	for end := period; end <= len(closes); end++ {
		highMA, okHigh := sma(highs[:end], period)
		lowMA, okLow := sma(lows[:end], period)
		if !okHigh || !okLow {
			return 0, 0, "", "", false
		}
		previousDirection = direction
		switch {
		case closes[end-1] > highMA:
			direction = "bull"
		case closes[end-1] < lowMA:
			direction = "bear"
		}
		if direction == "bear" {
			upper, lower = lowMA, highMA
		} else {
			upper, lower = highMA, lowMA
		}
	}
	return upper, lower, direction, previousDirection, true
}
