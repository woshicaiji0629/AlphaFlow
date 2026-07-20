package indicatorcalc

func addTDSequentialFeatures(target *ValueSet, values map[string]string, signals map[string]string, closes []float64) {
	buyCount, sellCount, exhaustion := tdSequential(closes)
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
