package indicatorcalc

func addSupplyDemandRangeFeaturesToSet(target *ValueSet, values map[string]string, signals map[string]string, highs, lows, closes, volumes []float64, lookback, bins int, thresholdPct float64) {
	zone, ok := supplyDemandRange(highs, lows, closes, volumes, lookback, bins, thresholdPct)
	if !ok {
		return
	}
	last := closes[len(closes)-1]
	setValueTarget(target, values, "supply_zone_top", zone.supplyTop, true)
	setValueTarget(target, values, "supply_zone_bottom", zone.supplyBottom, true)
	setValueTarget(target, values, "supply_zone_avg", zone.supplyAvg, true)
	setValueTarget(target, values, "supply_zone_wavg", zone.supplyWAvg, true)
	setValueTarget(target, values, "demand_zone_top", zone.demandTop, true)
	setValueTarget(target, values, "demand_zone_bottom", zone.demandBottom, true)
	setValueTarget(target, values, "demand_zone_avg", zone.demandAvg, true)
	setValueTarget(target, values, "demand_zone_wavg", zone.demandWAvg, true)
	setValueTarget(target, values, "supply_demand_equilibrium", zone.equilibrium, true)
	setValueTarget(target, values, "supply_demand_weighted_equilibrium", zone.weightedEquilibrium, true)
	signals["supply_demand_position"] = supplyDemandPosition(last, zone)
}

type supplyDemandRangeResult struct {
	supplyTop           float64
	supplyBottom        float64
	supplyAvg           float64
	supplyWAvg          float64
	demandTop           float64
	demandBottom        float64
	demandAvg           float64
	demandWAvg          float64
	equilibrium         float64
	weightedEquilibrium float64
}

func supplyDemandRange(highs []float64, lows []float64, closes []float64, volumes []float64, lookback int, bins int, thresholdPct float64) (supplyDemandRangeResult, bool) {
	if thresholdPct <= 0 {
		return supplyDemandRangeResult{}, false
	}
	bucketVolumes, rangeHigh, rangeLow, bucketSize, ok := priceVolumeBuckets(highs, lows, closes, volumes, lookback, bins, bins)
	if !ok {
		return supplyDemandRangeResult{}, false
	}
	totalVolume := sum(bucketVolumes)
	if totalVolume == 0 {
		return supplyDemandRangeResult{}, false
	}
	targetVolume := totalVolume * thresholdPct / 100
	supplyIndex, supplyWAvg, okSupply := supplyDemandBoundary(bucketVolumes, rangeLow, bucketSize, targetVolume, true)
	demandIndex, demandWAvg, okDemand := supplyDemandBoundary(bucketVolumes, rangeLow, bucketSize, targetVolume, false)
	if !okSupply || !okDemand {
		return supplyDemandRangeResult{}, false
	}
	result := supplyDemandRangeResult{
		supplyTop:    rangeHigh,
		supplyBottom: rangeLow + bucketSize*float64(supplyIndex),
		demandTop:    rangeLow + bucketSize*float64(demandIndex+1),
		demandBottom: rangeLow,
		supplyWAvg:   supplyWAvg,
		demandWAvg:   demandWAvg,
	}
	result.supplyAvg = (result.supplyTop + result.supplyBottom) / 2
	result.demandAvg = (result.demandTop + result.demandBottom) / 2
	result.equilibrium = (rangeHigh + rangeLow) / 2
	result.weightedEquilibrium = (result.supplyWAvg + result.demandWAvg) / 2
	return result, true
}

func supplyDemandBoundary(bucketVolumes []float64, rangeLow float64, bucketSize float64, targetVolume float64, fromHigh bool) (int, float64, bool) {
	if len(bucketVolumes) == 0 || targetVolume <= 0 {
		return 0, 0, false
	}
	var volumeSum float64
	var weightedSum float64
	if fromHigh {
		for index := len(bucketVolumes) - 1; index >= 0; index-- {
			center := rangeLow + bucketSize*(float64(index)+0.5)
			volume := bucketVolumes[index]
			volumeSum += volume
			weightedSum += center * volume
			if volumeSum >= targetVolume {
				return index, weightedSum / volumeSum, true
			}
		}
		return 0, weightedSum / volumeSum, volumeSum > 0
	}
	for index, volume := range bucketVolumes {
		center := rangeLow + bucketSize*(float64(index)+0.5)
		volumeSum += volume
		weightedSum += center * volume
		if volumeSum >= targetVolume {
			return index, weightedSum / volumeSum, true
		}
	}
	return len(bucketVolumes) - 1, weightedSum / volumeSum, volumeSum > 0
}

func supplyDemandPosition(price float64, zone supplyDemandRangeResult) string {
	switch {
	case price > zone.supplyTop:
		return "above_supply"
	case price >= zone.supplyBottom:
		return "in_supply"
	case price <= zone.demandBottom:
		return "below_demand"
	case price <= zone.demandTop:
		return "in_demand"
	default:
		return "between_zones"
	}
}
