package indicatorcalc

type streamTDSequentialState struct {
	closes    [5]float64
	count     int
	buyCount  int
	sellCount int
}

func (s *streamTDSequentialState) append(closeValue float64) {
	if s == nil {
		return
	}
	if s.count < len(s.closes) {
		s.closes[s.count] = closeValue
		s.count++
		if s.count < len(s.closes) {
			return
		}
	} else {
		copy(s.closes[:len(s.closes)-1], s.closes[1:])
		s.closes[len(s.closes)-1] = closeValue
	}
	switch {
	case closeValue < s.closes[0]:
		s.buyCount++
		s.sellCount = 0
	case closeValue > s.closes[0]:
		s.sellCount++
		s.buyCount = 0
	default:
		s.buyCount, s.sellCount = 0, 0
	}
	if s.buyCount > 9 {
		s.buyCount = 1
	}
	if s.sellCount > 9 {
		s.sellCount = 1
	}
}

func (s *streamTDSequentialState) value() (int, int, string) {
	if s == nil {
		return 0, 0, "none"
	}
	switch {
	case s.buyCount == 9:
		return s.buyCount, s.sellCount, "buy"
	case s.sellCount == 9:
		return s.buyCount, s.sellCount, "sell"
	default:
		return s.buyCount, s.sellCount, "none"
	}
}

func addTDSequentialFeatures(target *ValueSet, values map[string]string, signals map[string]string, closes []float64, basic *basicIndicatorState) {
	buyCount, sellCount, exhaustion, ok := basic.tdSequentialValue()
	if !ok {
		buyCount, sellCount, exhaustion = tdSequential(closes)
	}
	setValueTarget(target, values, "td_buy_setup_count", float64(buyCount), buyCount > 0)
	setValueTarget(target, values, "td_sell_setup_count", float64(sellCount), sellCount > 0)
	signals["td_exhaustion"] = exhaustion
}

func tdSequential(closes []float64) (int, int, string) {
	if len(closes) < 5 {
		return 0, 0, "none"
	}
	buyCount, sellCount := 0, 0
	for index := 4; index < len(closes); index++ {
		switch {
		case closes[index] < closes[index-4]:
			buyCount++
			sellCount = 0
		case closes[index] > closes[index-4]:
			sellCount++
			buyCount = 0
		default:
			buyCount, sellCount = 0, 0
		}
		if buyCount > 9 {
			buyCount = 1
		}
		if sellCount > 9 {
			sellCount = 1
		}
	}
	switch {
	case buyCount == 9:
		return buyCount, sellCount, "buy"
	case sellCount == 9:
		return buyCount, sellCount, "sell"
	default:
		return buyCount, sellCount, "none"
	}
}
