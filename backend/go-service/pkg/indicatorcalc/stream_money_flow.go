package indicatorcalc

type streamMoneyFlowState struct {
	obv      float64
	obvLast5 [5]float64
	obvCount int
	pvt      float64
	pvtLast5 [5]float64
	pvtCount int
	adLine   float64
	adLast5  [5]float64
	adCount  int
}

func (s *streamMoneyFlowState) append(high float64, low float64, closeValue float64, volume float64, previousClose float64, hasPrevious bool) {
	if s == nil {
		return
	}
	if hasPrevious {
		switch {
		case closeValue > previousClose:
			s.obv += volume
		case closeValue < previousClose:
			s.obv -= volume
		}
		if previousClose != 0 {
			s.pvt += (closeValue - previousClose) / previousClose * volume
		}
	}
	s.adLine += moneyFlowVolume(high, low, closeValue, volume)
	s.obvCount = appendFixedFloat5(&s.obvLast5, s.obvCount, s.obv)
	s.pvtCount = appendFixedFloat5(&s.pvtLast5, s.pvtCount, s.pvt)
	s.adCount = appendFixedFloat5(&s.adLast5, s.adCount, s.adLine)
}

func (s *streamMoneyFlowState) values() (float64, float64, float64, float64, float64, float64, bool) {
	if s == nil || s.obvCount < len(s.obvLast5) || s.pvtCount < len(s.pvtLast5) || s.adCount < len(s.adLast5) {
		return 0, 0, 0, 0, 0, 0, false
	}
	return s.obv, slopeFixedFloat5(s.obvLast5), s.pvt, slopeFixedFloat5(s.pvtLast5), s.adLine, slopeFixedFloat5(s.adLast5), true
}

func appendFixedFloat5(values *[5]float64, count int, value float64) int {
	if count < len(values) {
		values[count] = value
		return count + 1
	}
	copy(values[:], values[1:])
	values[len(values)-1] = value
	return count
}

func slopeFixedFloat5(values [5]float64) float64 {
	return values[len(values)-1] - values[0]
}
