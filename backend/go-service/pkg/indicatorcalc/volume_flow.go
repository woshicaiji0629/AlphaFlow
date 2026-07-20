package indicatorcalc

import "math"

type volumeFlowIndicatorResult struct {
	value          float64
	signal         float64
	hist           float64
	previousValue  float64
	previousSignal float64
	volumeCutoff   float64
	priceCutoff    float64
}

func addVolumeFlowIndicatorFeaturesToSet(target *ValueSet, values map[string]string, signals map[string]string, highs, lows, closes, volumes []float64, length int, coef, volumeCoef float64, signalLength int) {
	result, ok := volumeFlowIndicatorCompact(highs, lows, closes, volumes, length, coef, volumeCoef, signalLength)
	if !ok {
		result, ok = volumeFlowIndicator(highs, lows, closes, volumes, length, coef, volumeCoef, signalLength)
	}
	if !ok {
		return
	}
	setValueTarget(target, values, "vfi", result.value, true)
	setValueTarget(target, values, "vfi_signal", result.signal, true)
	setValueTarget(target, values, "vfi_hist", result.hist, true)
	setValueTarget(target, values, "vfi_volume_cutoff", result.volumeCutoff, true)
	setValueTarget(target, values, "vfi_price_cutoff", result.priceCutoff, true)
	signals["vfi_state"] = vfiState(result.value)
	signals["vfi_cross"] = crossSignal(result.previousValue, result.previousSignal, result.value, result.signal)
	signals["vfi_momentum"] = vfiMomentum(result.hist)
}

func volumeFlowIndicatorCompact(highs []float64, lows []float64, closes []float64, volumes []float64, length int, coef float64, volumeCoef float64, signalLength int) (volumeFlowIndicatorResult, bool) {
	if length <= 0 || coef <= 0 || volumeCoef <= 0 || signalLength <= 0 ||
		len(closes) != len(highs) || len(closes) != len(lows) || len(closes) != len(volumes) ||
		len(closes) < length*2+signalLength {
		return volumeFlowIndicatorResult{}, false
	}
	signalEMA := newStreamEMAState(signalLength)
	var vcpSum float64
	invalidCount := 1
	interWindow := make([]float64, 30)
	var interSum float64
	var interSumSq float64
	vcpWindow := make([]float64, length)
	validWindow := make([]bool, length)
	previousTypical := (highs[0] + lows[0] + closes[0]) / 3
	var volumeSum float64
	vfiCount := 0
	previousValue := 0.0
	currentValue := 0.0
	lastPriceCutoff := 0.0
	lastVolumeCutoff := 0.0
	for index := 1; index < len(closes); index++ {
		volumeSum += volumes[index-1]
		if dropVolume := index - length - 1; dropVolume >= 0 {
			volumeSum -= volumes[dropVolume]
		}

		typical := (highs[index] + lows[index] + closes[index]) / 3
		interValue := 0.0
		if typical > 0 && previousTypical > 0 {
			interValue = math.Log(typical) - math.Log(previousTypical)
		}
		interSlot := index % len(interWindow)
		previousInter := interWindow[interSlot]
		interSum -= previousInter
		interSumSq -= previousInter * previousInter
		interWindow[interSlot] = interValue
		interSum += interValue
		interSumSq += interValue * interValue

		vcpValue := 0.0
		validVCP := false
		priceCutoff := 0.0
		volumeCutoff := 0.0
		if index >= length && index >= len(interWindow) && volumeSum != 0 {
			mean := interSum / float64(len(interWindow))
			variance := interSumSq/float64(len(interWindow)) - mean*mean
			if variance < 0 {
				variance = 0
			}
			volatility := math.Sqrt(variance)
			volumeAverage := volumeSum / float64(length)
			priceCutoff = coef * volatility * closes[index]
			volumeCutoff = volumeAverage * volumeCoef
			cappedVolume := volumes[index]
			if cappedVolume > volumeCutoff {
				cappedVolume = volumeCutoff
			}
			moneyFlow := typical - previousTypical
			switch {
			case moneyFlow > priceCutoff:
				vcpValue = cappedVolume
			case moneyFlow < -priceCutoff:
				vcpValue = -cappedVolume
			default:
				vcpValue = 0
			}
			validVCP = true
		}

		slot := index % length
		if index >= length {
			vcpSum -= vcpWindow[slot]
			if !validWindow[slot] {
				invalidCount--
			}
		}
		vcpWindow[slot] = vcpValue
		validWindow[slot] = validVCP
		vcpSum += vcpValue
		if !validVCP {
			invalidCount++
		}
		previousTypical = typical

		if index < length-1 || invalidCount > 0 || volumeSum == 0 {
			continue
		}
		volumeAverage := volumeSum / float64(length)
		value := vcpSum / volumeAverage
		if vfiCount > 0 {
			previousValue = currentValue
		}
		currentValue = value
		vfiCount++
		signalEMA.append(value)
		if !signalEMA.ready {
			continue
		}
		lastPriceCutoff = priceCutoff
		lastVolumeCutoff = volumeCutoff
	}
	if vfiCount < signalLength+1 || !signalEMA.ready || !signalEMA.hasPrevious {
		return volumeFlowIndicatorResult{}, false
	}
	return volumeFlowIndicatorResult{
		value:          currentValue,
		signal:         signalEMA.value,
		hist:           currentValue - signalEMA.value,
		previousValue:  previousValue,
		previousSignal: signalEMA.previous,
		volumeCutoff:   lastVolumeCutoff,
		priceCutoff:    lastPriceCutoff,
	}, true
}

func volumeFlowIndicator(highs []float64, lows []float64, closes []float64, volumes []float64, length int, coef float64, volumeCoef float64, signalLength int) (volumeFlowIndicatorResult, bool) {
	if length <= 0 || coef <= 0 || volumeCoef <= 0 || signalLength <= 0 ||
		len(closes) != len(highs) || len(closes) != len(lows) || len(closes) != len(volumes) ||
		len(closes) < length*2+signalLength {
		return volumeFlowIndicatorResult{}, false
	}
	typicals := make([]float64, len(closes))
	inter := make([]float64, len(closes))
	for index := range closes {
		typicals[index] = (highs[index] + lows[index] + closes[index]) / 3
		if index == 0 || typicals[index] <= 0 || typicals[index-1] <= 0 {
			continue
		}
		inter[index] = math.Log(typicals[index]) - math.Log(typicals[index-1])
	}
	vcp := make([]float64, len(closes))
	validVCP := make([]bool, len(closes))
	priceCutoffs := make([]float64, len(closes))
	volumeCutoffs := make([]float64, len(closes))
	for index := 1; index < len(closes); index++ {
		if index < length || index < 30 {
			continue
		}
		volatility, ok := standardDeviation(inter[:index+1], 30)
		if !ok {
			continue
		}
		volumeAverage, ok := sma(volumes[index-length:index], length)
		if !ok || volumeAverage == 0 {
			continue
		}
		priceCutoff := coef * volatility * closes[index]
		volumeCutoff := volumeAverage * volumeCoef
		cappedVolume := volumes[index]
		if cappedVolume > volumeCutoff {
			cappedVolume = volumeCutoff
		}
		moneyFlow := typicals[index] - typicals[index-1]
		switch {
		case moneyFlow > priceCutoff:
			vcp[index] = cappedVolume
		case moneyFlow < -priceCutoff:
			vcp[index] = -cappedVolume
		default:
			vcp[index] = 0
		}
		validVCP[index] = true
		priceCutoffs[index] = priceCutoff
		volumeCutoffs[index] = volumeCutoff
	}
	vfiValues := []float64{}
	priceCutoffValues := []float64{}
	volumeCutoffValues := []float64{}
	for index := 0; index < len(closes); index++ {
		start := index - length + 1
		if start < 0 {
			continue
		}
		valid := true
		for vcpIndex := start; vcpIndex <= index; vcpIndex++ {
			if !validVCP[vcpIndex] {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		volumeAverage, ok := sma(volumes[index-length:index], length)
		if !ok || volumeAverage == 0 {
			continue
		}
		vfiValues = append(vfiValues, sum(vcp[start:index+1])/volumeAverage)
		priceCutoffValues = append(priceCutoffValues, priceCutoffs[index])
		volumeCutoffValues = append(volumeCutoffValues, volumeCutoffs[index])
	}
	if len(vfiValues) < signalLength+1 {
		return volumeFlowIndicatorResult{}, false
	}
	signalValues, ok := emaSeries(vfiValues, signalLength)
	if !ok || len(signalValues) < 2 {
		return volumeFlowIndicatorResult{}, false
	}
	offset := len(vfiValues) - len(signalValues)
	last := len(vfiValues) - 1
	prev := last - 1
	lastSignal := signalValues[len(signalValues)-1]
	prevSignal := signalValues[prev-offset]
	return volumeFlowIndicatorResult{
		value:          vfiValues[last],
		signal:         lastSignal,
		hist:           vfiValues[last] - lastSignal,
		previousValue:  vfiValues[prev],
		previousSignal: prevSignal,
		volumeCutoff:   volumeCutoffValues[len(volumeCutoffValues)-1],
		priceCutoff:    priceCutoffValues[len(priceCutoffValues)-1],
	}, true
}

func vfiState(value float64) string {
	switch {
	case value > 0:
		return "inflow"
	case value < 0:
		return "outflow"
	default:
		return "neutral"
	}
}

func vfiMomentum(hist float64) string {
	switch {
	case hist > 0:
		return "rising"
	case hist < 0:
		return "falling"
	default:
		return "flat"
	}
}
