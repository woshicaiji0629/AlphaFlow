package indicatorcalc

func addVolumeProfileFeaturesToSet(target *ValueSet, values map[string]string, signals map[string]string, highs, lows, closes, volumes []float64, lookback, bins int, valueAreaPct float64) {
	profile, ok := volumeProfile(highs, lows, closes, volumes, lookback, bins, valueAreaPct)
	if !ok {
		return
	}
	last := closes[len(closes)-1]
	setValueTarget(target, values, "volume_profile_poc", profile.poc, true)
	setValueTarget(target, values, "volume_profile_vah", profile.vah, true)
	setValueTarget(target, values, "volume_profile_val", profile.val, true)
	setValueTarget(target, values, "volume_profile_range_high", profile.rangeHigh, true)
	setValueTarget(target, values, "volume_profile_range_low", profile.rangeLow, true)
	setValueTarget(target, values, "volume_profile_value_area_pct", valueAreaPct, true)
	setValueTarget(target, values, "volume_profile_poc_distance_pct", percentDistance(last, profile.poc), profile.poc != 0)
	setValueTarget(target, values, "volume_profile_vah_distance_pct", percentDistance(last, profile.vah), profile.vah != 0)
	setValueTarget(target, values, "volume_profile_val_distance_pct", percentDistance(last, profile.val), profile.val != 0)
	signals["volume_profile_position"] = volumeProfilePosition(last, profile.vah, profile.val)
	signals["volume_profile_poc_side"] = volumeProfilePOCSide(last, profile.poc)
	signals["volume_profile_value_area_state"] = volumeProfileValueAreaState(last, profile.vah, profile.val)
}

type volumeProfileResult struct {
	poc       float64
	vah       float64
	val       float64
	rangeHigh float64
	rangeLow  float64
}

func volumeProfile(highs []float64, lows []float64, closes []float64, volumes []float64, lookback int, bins int, valueAreaPct float64) (volumeProfileResult, bool) {
	bucketVolumes, rangeHigh, rangeLow, bucketSize, ok := priceVolumeBuckets(highs, lows, closes, volumes, lookback, bins, bins-1)
	if !ok {
		return volumeProfileResult{}, false
	}
	maxIndex := 0
	totalVolume := 0.0
	for index, volume := range bucketVolumes {
		totalVolume += volume
		if volume > bucketVolumes[maxIndex] {
			maxIndex = index
		}
	}
	if totalVolume == 0 {
		return volumeProfileResult{}, false
	}
	valueAreaDown, valueAreaUp := volumeProfileValueArea(bucketVolumes, maxIndex, totalVolume, valueAreaPct)
	return volumeProfileResult{
		poc:       rangeLow + bucketSize*float64(maxIndex),
		vah:       rangeLow + bucketSize*float64(valueAreaUp),
		val:       rangeLow + bucketSize*float64(valueAreaDown),
		rangeHigh: rangeHigh,
		rangeLow:  rangeLow,
	}, true
}

func volumeProfileValueArea(bucketVolumes []float64, maxIndex int, totalVolume float64, valueAreaPct float64) (int, int) {
	targetVolume := totalVolume * valueAreaPct / 100
	up := maxIndex
	down := maxIndex
	valueAreaVolume := bucketVolumes[maxIndex]
	for valueAreaVolume < targetVolume {
		upVolume := 0.0
		if up < len(bucketVolumes)-1 {
			upVolume = bucketVolumes[up+1]
		}
		downVolume := 0.0
		if down > 0 {
			downVolume = bucketVolumes[down-1]
		}
		if upVolume == 0 && downVolume == 0 {
			break
		}
		if upVolume >= downVolume {
			valueAreaVolume += upVolume
			up++
		} else {
			valueAreaVolume += downVolume
			down--
		}
	}
	return down, up
}

func volumeProfilePosition(price float64, vah float64, val float64) string {
	switch {
	case price > vah:
		return "above_value_area"
	case price < val:
		return "below_value_area"
	default:
		return "inside_value_area"
	}
}

func volumeProfilePOCSide(price float64, poc float64) string {
	const threshold = 0.00000001
	switch {
	case price > poc+threshold:
		return "above"
	case price < poc-threshold:
		return "below"
	default:
		return "at"
	}
}

func volumeProfileValueAreaState(price float64, vah float64, val float64) string {
	switch {
	case price > vah:
		return "upper_breakout"
	case price < val:
		return "lower_breakdown"
	default:
		return "balanced"
	}
}
