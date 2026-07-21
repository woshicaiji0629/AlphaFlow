package supertrend

import (
	"fmt"
	"strconv"
	"strings"

	"alphaflow/go-service/pkg/marketregime"
	"alphaflow/go-service/pkg/strategy"
)

type pendingFlip struct {
	side        strategy.SignalSide
	signalPrice float64
	atr         float64
	age         int
}

type confirmationPending struct {
	side     strategy.SignalSide
	close    float64
	high     float64
	low      float64
	midpoint float64
	age      int
	retested bool
}

func newConfirmationPending(snapshot strategy.Snapshot, side strategy.SignalSide) (confirmationPending, error) {
	openPrice, err := strconv.ParseFloat(snapshot.Current.Open, 64)
	if err != nil {
		return confirmationPending{}, fmt.Errorf("parse confirmation open %q", snapshot.Current.Open)
	}
	high, err := strconv.ParseFloat(snapshot.Current.High, 64)
	if err != nil {
		return confirmationPending{}, fmt.Errorf("parse confirmation high %q", snapshot.Current.High)
	}
	low, err := strconv.ParseFloat(snapshot.Current.Low, 64)
	if err != nil {
		return confirmationPending{}, fmt.Errorf("parse confirmation low %q", snapshot.Current.Low)
	}
	closePrice, err := strconv.ParseFloat(snapshot.Current.Close, 64)
	if err != nil {
		return confirmationPending{}, fmt.Errorf("parse confirmation close %q", snapshot.Current.Close)
	}
	return confirmationPending{
		side: side, close: closePrice, high: high, low: low, midpoint: (openPrice + closePrice) / 2,
	}, nil
}

func (p *confirmationPending) waitOneAllows(snapshot strategy.Snapshot) (bool, bool, error) {
	closePrice, err := strconv.ParseFloat(snapshot.Current.Close, 64)
	if err != nil {
		return false, true, fmt.Errorf("parse wait confirmation close %q", snapshot.Current.Close)
	}
	directionalPrice := p.side == strategy.SignalSideBuy && closePrice > p.midpoint ||
		p.side == strategy.SignalSideSell && closePrice < p.midpoint
	macdHistogram, histogramOK := snapshot.Indicator.Float("macd_hist")
	macdDelta, deltaOK := snapshot.Indicator.Float("macd_hist_delta")
	squeeze, squeezeOK := snapshot.Indicator.Float("squeeze_momentum")
	squeezeDelta, squeezeDeltaOK := snapshot.Indicator.Float("squeeze_momentum_delta")
	direction := 1.0
	if p.side == strategy.SignalSideSell {
		direction = -1
	}
	macdOK := histogramOK && direction*macdHistogram >= 0 || deltaOK && direction*macdDelta >= 0
	squeezeOK = squeezeOK && direction*squeeze > 0 || squeezeDeltaOK && direction*squeezeDelta > 0
	return p.age == 1 && directionalPrice && macdOK && squeezeOK, p.age >= 1, nil
}

func (p *confirmationPending) retestAllows(snapshot strategy.Snapshot) (bool, bool, error) {
	high, err := strconv.ParseFloat(snapshot.Current.High, 64)
	if err != nil {
		return false, true, fmt.Errorf("parse retest high %q", snapshot.Current.High)
	}
	low, err := strconv.ParseFloat(snapshot.Current.Low, 64)
	if err != nil {
		return false, true, fmt.Errorf("parse retest low %q", snapshot.Current.Low)
	}
	closePrice, err := strconv.ParseFloat(snapshot.Current.Close, 64)
	if err != nil {
		return false, true, fmt.Errorf("parse retest close %q", snapshot.Current.Close)
	}
	invalid := p.side == strategy.SignalSideBuy && low < p.low || p.side == strategy.SignalSideSell && high > p.high
	if invalid {
		return false, true, nil
	}
	if p.side == strategy.SignalSideBuy && low <= p.midpoint || p.side == strategy.SignalSideSell && high >= p.midpoint {
		p.retested = true
	}
	recovered := p.retested && (p.side == strategy.SignalSideBuy && closePrice > p.close || p.side == strategy.SignalSideSell && closePrice < p.close)
	return recovered, p.age >= 3, nil
}

func (p *confirmationPending) exhaustReaccelerationAllows(snapshot strategy.Snapshot) (bool, bool, error) {
	closePrice, err := strconv.ParseFloat(snapshot.Current.Close, 64)
	if err != nil {
		return false, true, fmt.Errorf("parse exhaustion confirmation close %q", snapshot.Current.Close)
	}
	structureEvent := strings.ToLower(strings.TrimSpace(snapshot.Indicator.Signals["structure_event"]))
	if structureEvent == "" {
		if series, ok := snapshot.Window.Signal("structure_event"); ok {
			structureEvent = strings.ToLower(strings.TrimSpace(series.Latest))
		}
	}
	invalid := p.side == strategy.SignalSideBuy && (closePrice < p.low || structureEvent == "bos_down" || structureEvent == "choch_down") ||
		p.side == strategy.SignalSideSell && (closePrice > p.high || structureEvent == "bos_up" || structureEvent == "choch_up")
	if invalid {
		return false, true, nil
	}
	direction := 1.0
	if p.side == strategy.SignalSideSell {
		direction = -1
	}
	macdReaccelerated := false
	if timeframe, ok := snapshot.Timeframes["15m"]; ok {
		if delta, deltaOK := timeframe.Indicator.Float("macd_hist_delta"); deltaOK {
			macdReaccelerated = direction*delta > 0
		}
	}
	breakout := p.side == strategy.SignalSideBuy && closePrice > p.high || p.side == strategy.SignalSideSell && closePrice < p.low
	squeeze, squeezeOK := snapshot.Indicator.Float("squeeze_momentum")
	squeezeDelta, squeezeDeltaOK := snapshot.Indicator.Float("squeeze_momentum_delta")
	squeezeAligned := squeezeOK && squeezeDeltaOK && direction*squeeze > 0 && direction*squeezeDelta > 0
	return macdReaccelerated || breakout && squeezeAligned, p.age >= 10, nil
}

func exhaustionBlocked(snapshot strategy.Snapshot, side strategy.SignalSide, minADX float64, minDIDifference float64) bool {
	timeframe, ok := snapshot.Timeframes["15m"]
	if !ok {
		return false
	}
	adx, adxOK := timeframe.Indicator.Float("adx14")
	diPlus, plusOK := timeframe.Indicator.Float("di_plus14")
	diMinus, minusOK := timeframe.Indicator.Float("di_minus14")
	delta, deltaOK := timeframe.Indicator.Float("macd_hist_delta")
	if !adxOK || !plusOK || !minusOK || !deltaOK {
		return false
	}
	direction := 1.0
	if side == strategy.SignalSideSell {
		direction = -1
	}
	return adx >= minADX && direction*(diPlus-diMinus) >= minDIDifference && direction*delta <= 0
}

func macroMomentumBlocked(snapshot strategy.Snapshot, side strategy.SignalSide) bool {
	_, tenOK := snapshot.Timeframes["10m"]
	fifteenMinute, fifteenOK := snapshot.Timeframes["15m"]
	if !tenOK || !fifteenOK {
		return false
	}
	direction := timeframeSignal(snapshot, "10m", "supertrend_direction")
	opposite := side == strategy.SignalSideBuy && direction == "down" || side == strategy.SignalSideSell && direction == "up"
	delta, ok := fifteenMinute.Indicator.Float("macd_hist_delta")
	if !ok {
		return false
	}
	if side == strategy.SignalSideSell {
		delta = -delta
	}
	return opposite && delta <= 0
}

func newPendingFlip(snapshot strategy.Snapshot, side strategy.SignalSide) (pendingFlip, error) {
	price, err := strconv.ParseFloat(snapshot.Current.Close, 64)
	if err != nil || price <= 0 {
		return pendingFlip{}, fmt.Errorf("parse pending flip price %q", snapshot.Current.Close)
	}
	atr, ok := snapshot.Indicator.Float("atr14")
	if !ok || atr <= 0 {
		return pendingFlip{}, fmt.Errorf("pending flip requires positive atr14")
	}
	return pendingFlip{side: side, signalPrice: price, atr: atr}, nil
}

func regimeAllowsSide(regime *marketregime.Result, side strategy.SignalSide) bool {
	return regime != nil && (side == strategy.SignalSideBuy && regime.AllowLong || side == strategy.SignalSideSell && regime.AllowShort)
}

func compressionBlocked(regime *marketregime.Result) bool {
	if regime == nil {
		return false
	}
	if regime.State == marketregime.StateChopLock {
		return true
	}
	for _, reason := range regime.Reasons {
		if strings.Contains(reason, "compression") || strings.Contains(reason, "breakout_width") {
			return true
		}
	}
	return false
}

func timeframeSignal(snapshot strategy.Snapshot, interval string, key string) string {
	timeframe, ok := snapshot.Timeframes[interval]
	if !ok {
		return ""
	}
	if value := strings.TrimSpace(timeframe.Indicator.Signals[key]); value != "" {
		return strings.ToLower(value)
	}
	if series, ok := timeframe.Window.Signal(key); ok {
		return strings.ToLower(strings.TrimSpace(series.Latest))
	}
	return ""
}
