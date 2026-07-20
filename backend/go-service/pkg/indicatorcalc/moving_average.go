package indicatorcalc

import (
	"math"
)

func addMovingAverageFeatures(values map[string]string, signals map[string]string, closes []float64, volumes []float64, basic *basicIndicatorState) {
	addMovingAverageFeaturesToSet(nil, values, signals, closes, volumes, basic)
}

func addMovingAverageFeaturesToSet(target *ValueSet, values map[string]string, signals map[string]string, closes []float64, volumes []float64, basic *basicIndicatorState) {
	hma21, ok := hma(closes, 21)
	setValueTarget(target, values, "hma21", hma21, ok)
	vwma20, ok := vwma(closes, volumes, 20)
	setValueTarget(target, values, "vwma20", vwma20, ok)
	dema21, tema21, demaOK, temaOK := demaTema(closes, 21)
	setValueTarget(target, values, "dema21", dema21, demaOK)
	setValueTarget(target, values, "tema21", tema21, temaOK)
	kama10, ok := kama(closes, 10, 2, 30)
	setValueTarget(target, values, "kama10", kama10, ok)
	addAlligatorFeaturesToSet(target, values, signals, closes)

	if len(closes) >= 30 {
		recentHMA, okRecent := hma21, ok
		previousHMA, okPrevious := hma(closes[:len(closes)-3], 21)
		if okRecent && okPrevious && previousHMA != 0 {
			setValueTarget(target, values, "hma21_slope3_pct", percentDistance(recentHMA, previousHMA), true)
		}
	}

	ema7, ok7 := emaFromStateOrSeries(basic, closes, 7)
	ema25, ok25 := emaFromStateOrSeries(basic, closes, 25)
	ema99, ok99 := emaFromStateOrSeries(basic, closes, 99)
	last := closes[len(closes)-1]
	if ok7 && ok25 && ok99 {
		spread := (ema7 - ema99) / last * 100
		setValueTarget(target, values, "ema_spread_pct", spread, last != 0)
		signals["ma_state"] = movingAverageState(ema7, ema25, ema99, last)
		signals["ma_arrangement"] = movingAverageArrangement(ema7, ema25, ema99)
		setValueTarget(target, values, "ma_trend_strength", math.Abs(spread), true)
		addMovingAverageStructureFeaturesToSet(target, values, signals, closes, basic, ema7, ema25, ema99)
	}
	addEZEMASuiteFeaturesToSet(target, values, signals, closes, basic)
	addScriptDualMovingAverageToSet(target, values, signals, closes, volumes)
	addScriptMovingAverageSignalToSet(target, values, signals, closes, basic)
	addEMDFeaturesToSet(target, values, signals, closes, 25, 1)
}

func addAlligatorFeaturesToSet(target *ValueSet, values map[string]string, signals map[string]string, closes []float64) {
	jaw, teeth, lips, ok := alligator(closes)
	if !ok {
		return
	}
	last := closes[len(closes)-1]
	setValueTarget(target, values, "alligator_jaw", jaw, true)
	setValueTarget(target, values, "alligator_teeth", teeth, true)
	setValueTarget(target, values, "alligator_lips", lips, true)
	spread := (maxFloat(jaw, teeth, lips) - minFloat(jaw, teeth, lips)) / last * 100
	setValueTarget(target, values, "alligator_spread_pct", spread, last != 0)
	signals["alligator_direction"] = alligatorDirection(jaw, teeth, lips)
	signals["alligator_state"] = alligatorState(spread)
}
