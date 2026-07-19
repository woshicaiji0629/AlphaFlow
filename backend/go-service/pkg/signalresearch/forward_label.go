package signalresearch

import (
	"fmt"
	"math"
	"strconv"

	"alphaflow/go-service/pkg/marketmodel"
)

const ForwardLabelVersion = "forward-market-label.v1"

// ForwardLabel is an offline-only research target. It must never be added to
// an online strategy snapshot or used as a real-time strategy input.
type ForwardLabel string

const (
	ForwardLabelTrendUp             ForwardLabel = "trend_up"
	ForwardLabelTrendDown           ForwardLabel = "trend_down"
	ForwardLabelHighVolatilityRange ForwardLabel = "high_volatility_range"
	ForwardLabelLowVolatilityRange  ForwardLabel = "low_volatility_consolidation"
	ForwardLabelTrendStart          ForwardLabel = "trend_start"
	ForwardLabelTrendExhaustion     ForwardLabel = "trend_exhaustion"
)

var DefaultForwardHorizons = [...]int{20, 40, 80}

// ForwardMetrics contains result-side measurements calculated from future
// bars. The continuous measurements are persisted independently from a label
// so label thresholds can be frozen, versioned, and validated out of sample.
type ForwardMetrics struct {
	Version                   string
	HorizonBars               int
	ObservedBars              int
	DirectionReturnBps        float64
	MidpointReturnBps         float64
	MaxUpsideBps              float64
	MaxDownsideBps            float64
	PathEfficiency            float64
	RealizedVolatilityBps     float64
	MaxCloseDrawdownBps       float64
	MaxCloseRecoveryBps       float64
	DominantExcursionBps      float64
	DominantGivebackBps       float64
	DominantGivebackRatio     float64
	DominantExcursionBar      int
	DominantExcursionPosition float64
	DominantExcursionIsUpward bool
	DirectionalAdvantage      float64
	DominantRetention         float64
	LateReturnBps             float64
	PhaseExpansion            float64
}

// CalculateForwardMetrics measures the first horizon closed bars following an
// observation point. entryPrice is normally the signal bar close. Intrabar
// upside/downside use high/low; path, volatility, drawdown, and recovery use
// closes to avoid assuming whether a bar's high or low occurred first.
func CalculateForwardMetrics(entryPrice float64, bars []marketmodel.Kline, horizon int) (ForwardMetrics, error) {
	if entryPrice <= 0 {
		return ForwardMetrics{}, fmt.Errorf("entry price must be positive")
	}
	if horizon <= 0 {
		return ForwardMetrics{}, fmt.Errorf("forward horizon must be positive")
	}
	if len(bars) < horizon {
		return ForwardMetrics{}, fmt.Errorf("forward horizon requires %d bars, got %d", horizon, len(bars))
	}

	result := ForwardMetrics{Version: ForwardLabelVersion, HorizonBars: horizon, ObservedBars: horizon}
	previousClose := entryPrice
	peakClose := entryPrice
	troughClose := entryPrice
	pathLength := 0.0
	logReturnSquares := 0.0
	closes := make([]float64, horizon)
	closeTimeStep := int64(0)

	for index, bar := range bars[:horizon] {
		if !bar.IsClosed {
			return ForwardMetrics{}, fmt.Errorf("forward bar %d is not closed", index)
		}
		if index > 0 {
			step := bar.CloseTime - bars[index-1].CloseTime
			if step <= 0 {
				return ForwardMetrics{}, fmt.Errorf("forward bar %d close time is not increasing", index)
			}
			if closeTimeStep == 0 {
				closeTimeStep = step
			} else if step != closeTimeStep {
				return ForwardMetrics{}, fmt.Errorf("forward bar %d breaks close time cadence", index)
			}
		}
		high, err := positivePrice(bar.High, "high", index)
		if err != nil {
			return ForwardMetrics{}, err
		}
		low, err := positivePrice(bar.Low, "low", index)
		if err != nil {
			return ForwardMetrics{}, err
		}
		closePrice, err := positivePrice(bar.Close, "close", index)
		if err != nil {
			return ForwardMetrics{}, err
		}
		if low > high || closePrice < low || closePrice > high {
			return ForwardMetrics{}, fmt.Errorf("forward bar %d has inconsistent prices", index)
		}

		closes[index] = closePrice
		upside := priceReturnBps(entryPrice, high)
		downside := -priceReturnBps(entryPrice, low)
		if upside > result.MaxUpsideBps {
			result.MaxUpsideBps = upside
		}
		if downside > result.MaxDownsideBps {
			result.MaxDownsideBps = downside
		}

		pathLength += math.Abs(closePrice - previousClose)
		logReturn := math.Log(closePrice / previousClose)
		logReturnSquares += logReturn * logReturn
		previousClose = closePrice

		if closePrice > peakClose {
			peakClose = closePrice
		}
		if closePrice < troughClose {
			troughClose = closePrice
		}
		result.MaxCloseDrawdownBps = math.Max(result.MaxCloseDrawdownBps, -priceReturnBps(peakClose, closePrice))
		result.MaxCloseRecoveryBps = math.Max(result.MaxCloseRecoveryBps, priceReturnBps(troughClose, closePrice))
	}

	lastClose := closes[horizon-1]
	result.DirectionReturnBps = priceReturnBps(entryPrice, lastClose)
	result.MidpointReturnBps = priceReturnBps(entryPrice, closes[(horizon-1)/2])
	result.LateReturnBps = result.DirectionReturnBps - result.MidpointReturnBps
	result.PhaseExpansion = math.Abs(result.LateReturnBps) / math.Max(math.Abs(result.MidpointReturnBps), 1e-9)
	if pathLength > 0 {
		result.PathEfficiency = math.Abs(lastClose-entryPrice) / pathLength
	}
	result.RealizedVolatilityBps = math.Sqrt(logReturnSquares/float64(horizon)) * 10000
	setDominantExcursion(&result, entryPrice, bars[:horizon])
	if totalExcursion := result.MaxUpsideBps + result.MaxDownsideBps; totalExcursion > 0 {
		result.DirectionalAdvantage = (result.MaxUpsideBps - result.MaxDownsideBps) / totalExcursion
	}
	return result, nil
}

func setDominantExcursion(result *ForwardMetrics, entryPrice float64, bars []marketmodel.Kline) {
	result.DominantExcursionIsUpward = result.MaxUpsideBps >= result.MaxDownsideBps
	result.DominantExcursionBps = result.MaxDownsideBps
	if result.DominantExcursionIsUpward {
		result.DominantExcursionBps = result.MaxUpsideBps
	}

	for index, bar := range bars {
		priceText := bar.Low
		if result.DominantExcursionIsUpward {
			priceText = bar.High
		}
		price, _ := strconv.ParseFloat(priceText, 64)
		excursion := -priceReturnBps(entryPrice, price)
		if result.DominantExcursionIsUpward {
			excursion = priceReturnBps(entryPrice, price)
		}
		if excursion >= result.DominantExcursionBps {
			result.DominantExcursionBar = index + 1
			result.DominantExcursionPosition = float64(index+1) / float64(len(bars))
			break
		}
	}

	lastClose, _ := strconv.ParseFloat(bars[len(bars)-1].Close, 64)
	finalInDominantDirection := -priceReturnBps(entryPrice, lastClose)
	if result.DominantExcursionIsUpward {
		finalInDominantDirection = priceReturnBps(entryPrice, lastClose)
	}
	result.DominantGivebackBps = math.Max(0, result.DominantExcursionBps-finalInDominantDirection)
	if result.DominantExcursionBps > 0 {
		result.DominantGivebackRatio = result.DominantGivebackBps / result.DominantExcursionBps
		result.DominantRetention = finalInDominantDirection / result.DominantExcursionBps
	}
}

func positivePrice(value string, field string, index int) (float64, error) {
	price, err := strconv.ParseFloat(value, 64)
	if err != nil || price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
		return 0, fmt.Errorf("parse forward bar %d %s price %q", index, field, value)
	}
	return price, nil
}

func priceReturnBps(from float64, to float64) float64 {
	return (to - from) / from * 10000
}
