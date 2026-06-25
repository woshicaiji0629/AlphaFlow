package indicator

import "testing"

func TestSupportResistanceUsesPivotClusters(t *testing.T) {
	highs := []float64{105, 108, 106, 110, 107, 111, 108, 112, 109, 113, 110, 112, 109, 111, 108}
	lows := []float64{95, 98, 96, 99, 96, 100, 97, 101, 97, 100, 96, 99, 95, 98, 97}
	closes := []float64{100, 104, 101, 105, 102, 106, 103, 107, 104, 108, 105, 107, 104, 106, 104}

	values := map[string]string{}
	signals := map[string]string{}
	addSupportResistance(values, signals, highs, lows, closes)

	if values["support_1"] == "" {
		t.Fatalf("expected support_1, got %#v", values)
	}
	if values["resistance_1"] == "" {
		t.Fatalf("expected resistance_1, got %#v", values)
	}
	if values["support_strength"] == "" {
		t.Fatalf("expected support strength, got %#v", values)
	}
	if values["resistance_strength"] == "" {
		t.Fatalf("expected resistance strength, got %#v", values)
	}
	if signals["sr_position"] == "" {
		t.Fatalf("expected sr_position, got %#v", signals)
	}
}

func TestClusterLevelsFiltersWrongSideOfPrice(t *testing.T) {
	pivots := []priceLevel{
		{price: 90, touches: 1, recency: 1},
		{price: 91, touches: 1, recency: 2},
		{price: 110, touches: 1, recency: 3},
	}

	supports := clusterLevels(pivots, 2, 100, false)
	if len(supports) != 1 {
		t.Fatalf("supports = %d, want 1: %#v", len(supports), supports)
	}
	if supports[0].touches != 2 {
		t.Fatalf("support touches = %d, want 2", supports[0].touches)
	}

	resistances := clusterLevels(pivots, 2, 100, true)
	if len(resistances) != 1 {
		t.Fatalf("resistances = %d, want 1: %#v", len(resistances), resistances)
	}
	if resistances[0].price != 110 {
		t.Fatalf("resistance price = %v, want 110", resistances[0].price)
	}
}
