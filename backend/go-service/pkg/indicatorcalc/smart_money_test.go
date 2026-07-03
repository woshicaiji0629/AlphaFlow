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
