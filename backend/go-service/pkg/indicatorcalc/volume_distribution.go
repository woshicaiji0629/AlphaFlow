package indicatorcalc

import "math"

func priceVolumeBuckets(highs []float64, lows []float64, closes []float64, volumes []float64, lookback int, bins int, bucketSteps int) ([]float64, float64, float64, float64, bool) {
	if lookback <= 0 || bins < 2 || bucketSteps <= 0 || len(closes) < lookback ||
		len(highs) != len(closes) || len(lows) != len(closes) || len(volumes) != len(closes) {
		return nil, 0, 0, 0, false
	}
	start := len(closes) - lookback
	rangeHigh, rangeLow, ok := rangeHighLow(highs, lows, start, len(closes))
	if !ok || rangeHigh <= rangeLow {
		return nil, 0, 0, 0, false
	}
	bucketSize := (rangeHigh - rangeLow) / float64(bucketSteps)
	if bucketSize <= 0 {
		return nil, 0, 0, 0, false
	}
	bucketVolumes := make([]float64, bins)
	for index := start; index < len(closes); index++ {
		lowBucket := priceVolumeBucketIndex(lows[index], rangeLow, bucketSize, bins)
		highBucket := priceVolumeBucketIndex(highs[index], rangeLow, bucketSize, bins)
		if highBucket < lowBucket {
			continue
		}
		coveredBuckets := highBucket - lowBucket + 1
		volumePerBucket := volumes[index] / float64(coveredBuckets)
		for bucket := lowBucket; bucket <= highBucket; bucket++ {
			bucketVolumes[bucket] += volumePerBucket
		}
	}
	return bucketVolumes, rangeHigh, rangeLow, bucketSize, true
}

func rangeHighLow(highs []float64, lows []float64, start int, end int) (float64, float64, bool) {
	if start < 0 || end > len(highs) || end > len(lows) || start >= end {
		return 0, 0, false
	}
	rangeHigh := highs[start]
	rangeLow := lows[start]
	for index := start + 1; index < end; index++ {
		rangeHigh = math.Max(rangeHigh, highs[index])
		rangeLow = math.Min(rangeLow, lows[index])
	}
	return rangeHigh, rangeLow, true
}

func priceVolumeBucketIndex(price float64, rangeLow float64, bucketSize float64, bins int) int {
	index := int(math.Floor((price - rangeLow) / bucketSize))
	return clampInt(index, 0, bins-1)
}

func clampInt(value int, minimum int, maximum int) int {
	switch {
	case value < minimum:
		return minimum
	case value > maximum:
		return maximum
	default:
		return value
	}
}
