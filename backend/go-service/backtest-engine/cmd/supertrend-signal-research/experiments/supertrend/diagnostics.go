package supertrend

import (
	"math"
	"strconv"
	"strings"

	"alphaflow/go-service/pkg/marketregime"
	"alphaflow/go-service/pkg/signalresearch"
	"alphaflow/go-service/pkg/strategy"
)

type flipDiagnostic struct {
	SignalTimeMS          int64                                `json:"signal_time_ms"`
	Side                  strategy.SignalSide                  `json:"side"`
	Close                 string                               `json:"close"`
	RegimeState           marketregime.State                   `json:"regime_state,omitempty"`
	Direction             marketregime.Direction               `json:"direction,omitempty"`
	Allowed               bool                                 `json:"allowed"`
	Reason                string                               `json:"reason"`
	VolumeRatio20         float64                              `json:"volume_ratio20"`
	VolumeRatioReady      bool                                 `json:"volume_ratio20_ready"`
	PriceVolume           string                               `json:"price_volume_confirmation,omitempty"`
	VolumeLooseAllowed    bool                                 `json:"volume_loose_allowed"`
	VolumeStrongAllowed   bool                                 `json:"volume_strong_allowed"`
	ATR14                 float64                              `json:"atr14"`
	BodyATR               float64                              `json:"body_atr"`
	UpperWickATR          float64                              `json:"upper_wick_atr"`
	LowerWickATR          float64                              `json:"lower_wick_atr"`
	EMA25DistancePct      float64                              `json:"ema25_distance_pct"`
	EMA99DistancePct      float64                              `json:"ema99_distance_pct"`
	SupertrendDistancePct float64                              `json:"supertrend_distance_pct"`
	MACDHistogram         float64                              `json:"macd_hist"`
	MACDHistogramDelta    float64                              `json:"macd_hist_delta"`
	MACDMomentum          string                               `json:"macd_momentum,omitempty"`
	MACDDivergence        string                               `json:"macd_divergence,omitempty"`
	StructureEvent        string                               `json:"structure_event,omitempty"`
	StructureBias         string                               `json:"structure_bias,omitempty"`
	Direction5M           string                               `json:"direction_5m,omitempty"`
	Direction10M          string                               `json:"direction_10m,omitempty"`
	Direction15M          string                               `json:"direction_15m,omitempty"`
	HigherTimeframes      map[string]higherTimeframeDiagnostic `json:"higher_timeframes,omitempty"`
}

type entryDiagnostic struct {
	Mode                  string              `json:"mode"`
	TimeMS                int64               `json:"time_ms"`
	Side                  strategy.SignalSide `json:"side"`
	Price                 float64             `json:"price"`
	RegimeState           marketregime.State  `json:"regime_state,omitempty"`
	RegimeReason          string              `json:"regime_reason,omitempty"`
	ATR14                 float64             `json:"atr14"`
	EMA25DistancePct      float64             `json:"ema25_distance_pct"`
	SupertrendDistancePct float64             `json:"supertrend_distance_pct"`
	VolumeRatio20         float64             `json:"volume_ratio20"`
	MACDHistogram         float64             `json:"macd_hist"`
	MACDHistogramDelta    float64             `json:"macd_hist_delta"`
	MACDMomentum          string              `json:"macd_momentum,omitempty"`
	StructureEvent        string              `json:"structure_event,omitempty"`
	StructureBias         string              `json:"structure_bias,omitempty"`
	Direction5M           string              `json:"direction_5m,omitempty"`
	Direction10M          string              `json:"direction_10m,omitempty"`
	Direction15M          string              `json:"direction_15m,omitempty"`
	Direction30M          string              `json:"direction_30m,omitempty"`
}

type higherTimeframeDiagnostic struct {
	Available             bool     `json:"available"`
	Supertrend            *float64 `json:"supertrend,omitempty"`
	SupertrendDistancePct *float64 `json:"supertrend_distance_pct,omitempty"`
	SupertrendDirection   string   `json:"supertrend_direction,omitempty"`
	SupertrendFlip        string   `json:"supertrend_flip,omitempty"`
	EMA7                  *float64 `json:"ema7,omitempty"`
	EMA25                 *float64 `json:"ema25,omitempty"`
	EMA99                 *float64 `json:"ema99,omitempty"`
	EMA25DistancePct      *float64 `json:"ema25_distance_pct,omitempty"`
	EMA99DistancePct      *float64 `json:"ema99_distance_pct,omitempty"`
	EMA25SlopePct         *float64 `json:"ema25_slope_pct,omitempty"`
	MAGroupSpreadPct      *float64 `json:"ma_group_spread_pct,omitempty"`
	MAArrangement         string   `json:"ma_arrangement,omitempty"`
	MAState               string   `json:"ma_state,omitempty"`
	MACD                  *float64 `json:"macd,omitempty"`
	MACDSignal            *float64 `json:"macd_signal,omitempty"`
	MACDHistogram         *float64 `json:"macd_hist,omitempty"`
	MACDHistogramDelta    *float64 `json:"macd_hist_delta,omitempty"`
	MACDMomentum          string   `json:"macd_momentum,omitempty"`
	MACDHistogramPhase    string   `json:"macd_hist_phase,omitempty"`
	MACDDivergence        string   `json:"macd_divergence,omitempty"`
	ADX                   *float64 `json:"adx14,omitempty"`
	DIPlus                *float64 `json:"di_plus14,omitempty"`
	DIMinus               *float64 `json:"di_minus14,omitempty"`
	KAMA                  *float64 `json:"kama10,omitempty"`
	KAMASlopeATR          *float64 `json:"kama_slope_atr,omitempty"`
	MoneyFlow             string   `json:"money_flow,omitempty"`
	RSI                   *float64 `json:"rsi14,omitempty"`
	BollingerWidthPct     *float64 `json:"bb_width_pct,omitempty"`
	BollingerWidthDelta   *float64 `json:"bb_width_delta,omitempty"`
	BollingerWidthState   string   `json:"bb_width_state,omitempty"`
	BollingerPosition     string   `json:"bb_position,omitempty"`
	SqueezeState          string   `json:"squeeze_state,omitempty"`
	SqueezeMomentum       *float64 `json:"squeeze_momentum,omitempty"`
	SqueezeMomentumDelta  *float64 `json:"squeeze_momentum_delta,omitempty"`
	MomentumState         string   `json:"momentum_state,omitempty"`
	VolumeRatio20         *float64 `json:"volume_ratio20,omitempty"`
	PriceVolume           string   `json:"price_volume_confirmation,omitempty"`
	StructureEvent        string   `json:"structure_event,omitempty"`
	StructureBias         string   `json:"structure_bias,omitempty"`
}

type diagnosticModeTrades struct {
	EntryMode string                               `json:"entry_mode"`
	Trades    []signalresearch.SinglePositionTrade `json:"trades"`
}

type versionDiagnostics struct {
	Version       string                                 `json:"version"`
	Flips         []flipDiagnostic                       `json:"flips,omitempty"`
	Entries       []entryDiagnostic                      `json:"entries,omitempty"`
	Trades        []diagnosticModeTrades                 `json:"trades,omitempty"`
	Followthrough []signalresearch.ValidationObservation `json:"followthrough,omitempty"`
}

type diagnosticsArtifact struct {
	Versions []versionDiagnostics `json:"versions"`
}

func enableDiagnostics(versions []*versionReplay) {
	for _, version := range versions {
		version.flipDiagnostics = make([]flipDiagnostic, 0, 128)
		version.entryDiagnostics = make([]entryDiagnostic, 0, 128)
	}
}

func buildDiagnosticsArtifact(versions []*versionReplay) diagnosticsArtifact {
	result := diagnosticsArtifact{Versions: make([]versionDiagnostics, 0, len(versions))}
	for _, version := range versions {
		item := versionDiagnostics{
			Version: version.spec.name, Flips: version.flipDiagnostics, Entries: version.entryDiagnostics,
			Trades:        make([]diagnosticModeTrades, 0, len(version.modes)),
			Followthrough: version.followthrough.Results(),
		}
		for _, mode := range version.modes {
			item.Trades = append(item.Trades, diagnosticModeTrades{EntryMode: mode.name, Trades: mode.replay.Trades()})
		}
		result.Versions = append(result.Versions, item)
	}
	return result
}

func buildFlipDiagnostic(snapshot strategy.Snapshot, side strategy.SignalSide, regime *marketregime.Result) flipDiagnostic {
	diagnostic := flipDiagnostic{
		SignalTimeMS: snapshot.Current.CloseTime, Side: side, Close: snapshot.Current.Close,
		Reason: "no_regime", PriceVolume: strings.ToLower(strings.TrimSpace(snapshot.Indicator.Signals["price_volume_confirmation"])),
	}
	diagnostic.VolumeRatio20, diagnostic.VolumeRatioReady = snapshot.Indicator.Float("volume_ratio20")
	diagnostic.VolumeLooseAllowed = volumeAllowsFlip(snapshot, side, 1.0, true)
	diagnostic.VolumeStrongAllowed = volumeAllowsFlip(snapshot, side, 1.2, false)
	diagnostic.ATR14, _ = snapshot.Indicator.Float("atr14")
	diagnostic.EMA25DistancePct, _ = snapshot.Indicator.Float("price_ema25_distance_pct")
	diagnostic.EMA99DistancePct, _ = snapshot.Indicator.Float("price_ema99_distance_pct")
	diagnostic.SupertrendDistancePct, _ = snapshot.Indicator.Float("supertrend_distance_pct")
	diagnostic.MACDHistogram, _ = snapshot.Indicator.Float("macd_hist")
	diagnostic.MACDHistogramDelta, _ = snapshot.Indicator.Float("macd_hist_delta")
	diagnostic.MACDMomentum = snapshot.Indicator.Signals["macd_momentum"]
	diagnostic.MACDDivergence = snapshot.Indicator.Signals["macd_divergence"]
	diagnostic.StructureEvent = snapshot.Indicator.Signals["structure_event"]
	diagnostic.StructureBias = snapshot.Indicator.Signals["structure_bias"]
	diagnostic.Direction5M = timeframeSignal(snapshot, "5m", "supertrend_direction")
	diagnostic.Direction10M = timeframeSignal(snapshot, "10m", "supertrend_direction")
	diagnostic.Direction15M = timeframeSignal(snapshot, "15m", "supertrend_direction")
	diagnostic.HigherTimeframes = make(map[string]higherTimeframeDiagnostic, 4)
	for _, interval := range []string{"5m", "10m", "15m", "30m"} {
		diagnostic.HigherTimeframes[interval] = buildHigherTimeframeDiagnostic(snapshot, interval)
	}
	if diagnostic.ATR14 > 0 {
		openPrice, openErr := strconv.ParseFloat(snapshot.Current.Open, 64)
		highPrice, highErr := strconv.ParseFloat(snapshot.Current.High, 64)
		lowPrice, lowErr := strconv.ParseFloat(snapshot.Current.Low, 64)
		closePrice, closeErr := strconv.ParseFloat(snapshot.Current.Close, 64)
		if openErr == nil && highErr == nil && lowErr == nil && closeErr == nil {
			diagnostic.BodyATR = math.Abs(closePrice-openPrice) / diagnostic.ATR14
			diagnostic.UpperWickATR = (highPrice - math.Max(openPrice, closePrice)) / diagnostic.ATR14
			diagnostic.LowerWickATR = (math.Min(openPrice, closePrice) - lowPrice) / diagnostic.ATR14
		}
	}
	if regime != nil {
		diagnostic.RegimeState = regime.State
		diagnostic.Direction = regime.Direction
		diagnostic.Allowed = side == strategy.SignalSideBuy && regime.AllowLong || side == strategy.SignalSideSell && regime.AllowShort
		diagnostic.Reason = regimeDecisionReason(side, *regime)
	}
	return diagnostic
}

func buildEntryDiagnostic(mode string, snapshot strategy.Snapshot, side strategy.SignalSide, regime *marketregime.Result) entryDiagnostic {
	price, _ := strconv.ParseFloat(snapshot.Current.Close, 64)
	result := entryDiagnostic{Mode: mode, TimeMS: snapshot.Current.CloseTime, Side: side, Price: price}
	result.ATR14, _ = snapshot.Indicator.Float("atr14")
	result.EMA25DistancePct, _ = snapshot.Indicator.Float("price_ema25_distance_pct")
	result.SupertrendDistancePct, _ = snapshot.Indicator.Float("supertrend_distance_pct")
	result.VolumeRatio20, _ = snapshot.Indicator.Float("volume_ratio20")
	result.MACDHistogram, _ = snapshot.Indicator.Float("macd_hist")
	result.MACDHistogramDelta, _ = snapshot.Indicator.Float("macd_hist_delta")
	result.MACDMomentum = snapshot.Indicator.Signals["macd_momentum"]
	result.StructureEvent = snapshot.Indicator.Signals["structure_event"]
	result.StructureBias = snapshot.Indicator.Signals["structure_bias"]
	result.Direction5M = timeframeSignal(snapshot, "5m", "ai_supertrend_direction")
	result.Direction10M = timeframeSignal(snapshot, "10m", "ai_supertrend_direction")
	result.Direction15M = timeframeSignal(snapshot, "15m", "ai_supertrend_direction")
	result.Direction30M = timeframeSignal(snapshot, "30m", "ai_supertrend_direction")
	if regime != nil {
		result.RegimeState = regime.State
		result.RegimeReason = regimeDecisionReason(side, *regime)
	}
	return result
}

func buildHigherTimeframeDiagnostic(snapshot strategy.Snapshot, interval string) higherTimeframeDiagnostic {
	timeframe, ok := snapshot.Timeframes[interval]
	if !ok {
		return higherTimeframeDiagnostic{}
	}
	view := timeframe.Indicator
	result := higherTimeframeDiagnostic{
		Available: true, Supertrend: numericDiagnostic(view, "supertrend"), SupertrendDistancePct: numericDiagnostic(view, "supertrend_distance_pct"),
		SupertrendDirection: timeframeSignal(snapshot, interval, "supertrend_direction"), SupertrendFlip: indicatorSignalDiagnostic(timeframe, "supertrend_flip"),
		EMA7: numericDiagnostic(view, "ema7"), EMA25: numericDiagnostic(view, "ema25"), EMA99: numericDiagnostic(view, "ema99"),
		EMA25DistancePct: numericDiagnostic(view, "price_ema25_distance_pct"), EMA99DistancePct: numericDiagnostic(view, "price_ema99_distance_pct"),
		EMA25SlopePct: numericDiagnostic(view, "ema25_slope5_pct"), MAGroupSpreadPct: numericDiagnostic(view, "ma_group_spread_pct"),
		MAArrangement: indicatorSignalDiagnostic(timeframe, "ma_arrangement"), MAState: indicatorSignalDiagnostic(timeframe, "ma_state"),
		MACD: numericDiagnostic(view, "macd"), MACDSignal: numericDiagnostic(view, "macd_signal"), MACDHistogram: numericDiagnostic(view, "macd_hist"),
		MACDHistogramDelta: numericDiagnostic(view, "macd_hist_delta"), MACDMomentum: indicatorSignalDiagnostic(timeframe, "macd_momentum"),
		MACDHistogramPhase: indicatorSignalDiagnostic(timeframe, "macd_hist_phase"), MACDDivergence: indicatorSignalDiagnostic(timeframe, "macd_divergence"),
		ADX: numericDiagnostic(view, "adx14"), DIPlus: numericDiagnostic(view, "di_plus14"), DIMinus: numericDiagnostic(view, "di_minus14"),
		KAMA: numericDiagnostic(view, "kama10"), MoneyFlow: indicatorSignalDiagnostic(timeframe, "money_flow_window_bias"), RSI: numericDiagnostic(view, "rsi14"),
		BollingerWidthPct: numericDiagnostic(view, "bb_width_pct"), BollingerWidthDelta: numericDiagnostic(view, "bb_width_delta"),
		BollingerWidthState: indicatorSignalDiagnostic(timeframe, "bb_width_state"), BollingerPosition: indicatorSignalDiagnostic(timeframe, "bb_position"),
		SqueezeState: indicatorSignalDiagnostic(timeframe, "squeeze_state"), SqueezeMomentum: numericDiagnostic(view, "squeeze_momentum"),
		SqueezeMomentumDelta: numericDiagnostic(view, "squeeze_momentum_delta"), MomentumState: indicatorSignalDiagnostic(timeframe, "momentum_state"),
		VolumeRatio20: numericDiagnostic(view, "volume_ratio20"), PriceVolume: indicatorSignalDiagnostic(timeframe, "price_volume_confirmation"),
		StructureEvent: indicatorSignalDiagnostic(timeframe, "structure_event"), StructureBias: indicatorSignalDiagnostic(timeframe, "structure_bias"),
	}
	atr, atrOK := view.Float("atr14")
	if kamaSeries, seriesOK := timeframe.Window.Numeric("kama10"); atrOK && atr > 0 && seriesOK {
		value := (kamaSeries.Latest - kamaSeries.Previous) / atr
		result.KAMASlopeATR = &value
	}
	return result
}

func numericDiagnostic(view strategy.IndicatorView, key string) *float64 {
	value, ok := view.Float(key)
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	return &value
}

func indicatorSignalDiagnostic(timeframe strategy.TimeframeSnapshot, key string) string {
	if value := strings.TrimSpace(timeframe.Indicator.Signals[key]); value != "" {
		return value
	}
	if series, ok := timeframe.Window.Signal(key); ok {
		return strings.TrimSpace(series.Latest)
	}
	return ""
}

func regimeDecisionReason(side strategy.SignalSide, regime marketregime.Result) string {
	if side == strategy.SignalSideBuy && regime.AllowLong || side == strategy.SignalSideSell && regime.AllowShort {
		return "permitted"
	}
	if side == strategy.SignalSideBuy && regime.AllowShort || side == strategy.SignalSideSell && regime.AllowLong {
		return "v4_countertrend_signal"
	}
	for index := len(regime.Reasons) - 1; index >= 0; index-- {
		reason := regime.Reasons[index]
		if (strings.HasPrefix(reason, "v4_") || strings.HasPrefix(reason, "v5_") || strings.HasPrefix(reason, "v6_")) &&
			reason != "v4_permitted" && reason != "v4_release_confirmed" &&
			reason != "v5_permitted" && reason != "v5_fast_release_confirmed" &&
			reason != "v6_permitted" && reason != "v6_fast_release_confirmed" {
			return reason
		}
	}
	return "regime_blocked"
}
