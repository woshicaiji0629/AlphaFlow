package indicatorcalc

import "testing"

func TestSmartMoneyDetectsBreakOfStructureUp(t *testing.T) {
	values := map[string]string{}
	signals := map[string]string{}
	opens := []float64{9, 10, 11, 14, 12, 11, 10, 11, 12, 13, 14, 15}
	highs := []float64{10, 11, 12, 15, 13, 12, 11, 12, 13, 14, 13, 16}
	lows := []float64{8, 9, 10, 11, 10, 9, 8, 9, 10, 11, 10, 12}
	closes := []float64{9.5, 10.5, 11.5, 12, 11, 10, 9, 11.5, 12.5, 13.5, 13, 15.5}

	addSmartMoney(values, signals, opens, highs, lows, closes)

	if signals["market_structure"] != "bos_up" {
		t.Fatalf("market_structure = %q, want bos_up", signals["market_structure"])
	}
	if signals["structure_event"] != "bos_up" {
		t.Fatalf("structure_event = %q, want bos_up", signals["structure_event"])
	}
	if signals["structure_bias"] != "bull" {
		t.Fatalf("structure_bias = %q, want bull", signals["structure_bias"])
	}
	if values["swing_high"] != "15" {
		t.Fatalf("swing_high = %q, want 15", values["swing_high"])
	}
	if values["order_block_high"] == "" || values["order_block_low"] == "" || values["order_block_mid"] == "" {
		t.Fatalf("missing order block values: %#v", values)
	}
	for _, key := range []string{
		"internal_swing_high",
		"internal_swing_low",
		"premium_level",
		"discount_level",
		"equilibrium_level",
	} {
		if values[key] == "" {
			t.Fatalf("missing %s in %#v", key, values)
		}
	}
	for _, key := range []string{
		"internal_structure_event",
		"internal_structure_bias",
		"swing_high_strength",
		"swing_low_strength",
		"internal_swing_high_strength",
		"internal_swing_low_strength",
		"equal_high_low",
		"fvg_direction",
		"fvg_position",
		"premium_discount_zone",
	} {
		if signals[key] == "" {
			t.Fatalf("missing %s in %#v", key, signals)
		}
	}
}

func TestSwingStrengthLabels(t *testing.T) {
	high, low := swingStrengthLabels(swingTrendUp)
	if high != "weak" || low != "strong" {
		t.Fatalf("up strength = %q/%q, want weak/strong", high, low)
	}
	high, low = swingStrengthLabels(swingTrendDown)
	if high != "strong" || low != "weak" {
		t.Fatalf("down strength = %q/%q, want strong/weak", high, low)
	}
	high, low = swingStrengthLabels(swingTrendRange)
	if high != "unknown" || low != "unknown" {
		t.Fatalf("range strength = %q/%q, want unknown/unknown", high, low)
	}
}

func TestSmartMoneyDetectsLiquiditySweepHigh(t *testing.T) {
	values := map[string]string{}
	signals := map[string]string{}
	opens := []float64{9, 10, 11, 14, 12, 11, 10, 11, 12, 13, 14, 14.5}
	highs := []float64{10, 11, 12, 15, 13, 12, 11, 12, 13, 14, 13, 16}
	lows := []float64{8, 9, 10, 11, 10, 9, 8, 9, 10, 11, 10, 12}
	closes := []float64{9.5, 10.5, 11.5, 12, 11, 10, 9, 11.5, 12.5, 13.5, 13, 14}

	addSmartMoney(values, signals, opens, highs, lows, closes)

	if signals["smart_money"] != "liquidity_sweep_high" {
		t.Fatalf("smart_money = %q, want liquidity_sweep_high; signals=%#v", signals["smart_money"], signals)
	}
	if signals["structure_event"] != "sweep_high" {
		t.Fatalf("structure_event = %q, want sweep_high", signals["structure_event"])
	}
	if signals["market_structure"] != "range" {
		t.Fatalf("market_structure = %q, want range", signals["market_structure"])
	}
	if signals["liquidity_sweep_type"] != "wick_high" {
		t.Fatalf("liquidity_sweep_type = %q, want wick_high", signals["liquidity_sweep_type"])
	}
	if values["liquidity_sweep_level"] != "15" || values["liquidity_sweep_top"] != "16" || values["liquidity_sweep_bottom"] != "15" {
		t.Fatalf("unexpected liquidity sweep values: %#v", values)
	}
	if values["liquidity_sweep_age"] != "0" {
		t.Fatalf("liquidity_sweep_age = %q, want 0", values["liquidity_sweep_age"])
	}
}

func TestSmartMoneyDetectsLiquidityRetestHigh(t *testing.T) {
	values := map[string]string{}
	signals := map[string]string{}
	opens := []float64{9, 10, 11, 12, 11, 10, 13, 15.8, 15.2}
	highs := []float64{10, 11, 15, 13, 12, 14, 16, 16.5, 16}
	lows := []float64{8, 9, 10, 9, 8, 10, 14.2, 15.2, 14.5}
	closes := []float64{9.5, 10.5, 12, 11, 10, 13, 15.5, 16, 15.5}

	addSmartMoney(values, signals, opens, highs, lows, closes)

	if signals["liquidity_sweep_type"] != "retest_high" {
		t.Fatalf("liquidity_sweep_type = %q, want retest_high; signals=%#v values=%#v", signals["liquidity_sweep_type"], signals, values)
	}
	if values["liquidity_sweep_level"] != "15" {
		t.Fatalf("liquidity_sweep_level = %q, want 15", values["liquidity_sweep_level"])
	}
	if values["liquidity_sweep_top"] != "15" || values["liquidity_sweep_bottom"] != "14.5" {
		t.Fatalf("unexpected liquidity sweep area: %#v", values)
	}
}

func TestMomentumSupplyDemandDetectsDemandRetest(t *testing.T) {
	opens, highs, lows, closes := momentumSupplyDemandSeries(40)
	highs[30] = 102
	lows[30] = 98
	for index := 32; index <= 35; index++ {
		opens[index] = 100 + float64(index-32)
		closes[index] = opens[index] + 3
		highs[index] = closes[index] + 0.5
		lows[index] = opens[index] - 0.5
	}
	opens[39] = 101
	closes[39] = 101.5
	highs[39] = 102
	lows[39] = 100

	state, ok := detectMomentumSupplyDemandZones(opens, highs, lows, closes, 40, 4, 4, 0.5, 20, 10)

	if !ok || !state.demand.ok {
		t.Fatalf("missing demand zone: %#v", state)
	}
	if state.demand.top != 102 || state.demand.bottom != 98 {
		t.Fatalf("demand zone = %v/%v, want 102/98", state.demand.top, state.demand.bottom)
	}
	if state.position != "in_demand" {
		t.Fatalf("position = %q, want in_demand", state.position)
	}
	if state.retestEvent != "demand_retest" {
		t.Fatalf("retest = %q, want demand_retest", state.retestEvent)
	}
}

func TestMomentumSupplyDemandDetectsSupplyAndBreak(t *testing.T) {
	opens, highs, lows, closes := momentumSupplyDemandSeries(40)
	highs[30] = 102
	lows[30] = 98
	for index := 32; index <= 35; index++ {
		opens[index] = 105 - float64(index-32)
		closes[index] = opens[index] - 3
		highs[index] = opens[index] + 0.5
		lows[index] = closes[index] - 0.5
	}
	opens[39] = 101
	closes[39] = 103
	highs[39] = 103.5
	lows[39] = 100.5

	state, ok := detectMomentumSupplyDemandZones(opens, highs, lows, closes, 40, 4, 4, 0.5, 20, 10)

	if !ok || !state.supply.ok {
		t.Fatalf("missing supply zone: %#v", state)
	}
	if state.supply.top != 102 || state.supply.bottom != 98 {
		t.Fatalf("supply zone = %v/%v, want 102/98", state.supply.top, state.supply.bottom)
	}
	if state.breakEvent != "supply_break" {
		t.Fatalf("break = %q, want supply_break", state.breakEvent)
	}
	if state.position != "above_supply" {
		t.Fatalf("position = %q, want above_supply", state.position)
	}
}

func TestOrderBlockUsesRecentOppositeCandle(t *testing.T) {
	values := map[string]string{}
	signals := map[string]string{}
	opens := []float64{9, 10, 11, 14, 12, 11, 10, 11, 14, 13, 12, 15}
	highs := []float64{10, 11, 12, 15, 13, 12, 11, 12, 14.5, 14, 13, 16}
	lows := []float64{8, 9, 10, 11, 10, 9, 8, 9, 11, 11, 10, 12}
	closes := []float64{9.5, 10.5, 11.5, 12, 11, 10, 9, 11.5, 12, 13.5, 12.5, 15.5}

	addSmartMoney(values, signals, opens, highs, lows, closes)

	if signals["market_structure"] != "bos_up" {
		t.Fatalf("market_structure = %q, want bos_up", signals["market_structure"])
	}
	if values["order_block_high"] != "14.5" {
		t.Fatalf("order_block_high = %q, want 14.5", values["order_block_high"])
	}
	if values["order_block_low"] != "11" {
		t.Fatalf("order_block_low = %q, want 11", values["order_block_low"])
	}
}

func TestSmartMoneyDetectsFairValueGap(t *testing.T) {
	values := map[string]string{}
	signals := map[string]string{}
	opens := []float64{9, 10, 10.5, 11, 11.5, 12, 13}
	highs := []float64{10, 11, 11.5, 12, 12.5, 13, 15}
	lows := []float64{8, 9, 9.5, 10, 10.5, 11, 13.5}
	closes := []float64{9.5, 10.5, 11, 11.5, 12, 12.8, 14}

	addSmartMoney(values, signals, opens, highs, lows, closes)

	if signals["fvg_direction"] != "bull" {
		t.Fatalf("fvg_direction = %q, want bull; values=%#v signals=%#v", signals["fvg_direction"], values, signals)
	}
	if values["fvg_top"] == "" || values["fvg_bottom"] == "" || values["fvg_mid"] == "" {
		t.Fatalf("missing fvg values: %#v", values)
	}
}

func momentumSupplyDemandSeries(length int) ([]float64, []float64, []float64, []float64) {
	opens := make([]float64, 0, length)
	highs := make([]float64, 0, length)
	lows := make([]float64, 0, length)
	closes := make([]float64, 0, length)
	for index := 0; index < length; index++ {
		openValue := 100.0
		closeValue := 100.2
		opens = append(opens, openValue)
		highs = append(highs, 101)
		lows = append(lows, 99)
		closes = append(closes, closeValue)
	}
	return opens, highs, lows, closes
}
