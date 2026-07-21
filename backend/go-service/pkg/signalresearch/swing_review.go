package signalresearch

import (
	"crypto/sha256"
	"fmt"
	"math"
	"strconv"

	"alphaflow/go-service/pkg/marketmodel"
	"alphaflow/go-service/pkg/strategy"
)

type SwingReviewConfig struct {
	MinimumMovePoints float64
	ReversalPoints    float64
	Mode              SwingThresholdMode
	ATRPeriod         int
	MinimumMoveATR    float64
	ReversalATR       float64
	LeadWindowMS      int64
}

type SwingThresholdMode string

const (
	SwingThresholdPoints SwingThresholdMode = "points"
	SwingThresholdATR    SwingThresholdMode = "atr"
)

type SwingSignal struct {
	TimeMS  int64               `json:"time_ms"`
	Side    strategy.SignalSide `json:"side"`
	Allowed bool                `json:"allowed"`
	Reason  string              `json:"reason"`
}

type SwingEvidence struct {
	TimeMS int64               `json:"time_ms"`
	Side   strategy.SignalSide `json:"side"`
	Source string              `json:"source"`
}

type SwingOpportunity struct {
	StartTimeMS       int64                `json:"start_time_ms"`
	EndTimeMS         int64                `json:"end_time_ms"`
	Side              strategy.SignalSide  `json:"side"`
	StartPrice        float64              `json:"start_price"`
	EndPrice          float64              `json:"end_price"`
	MovePoints        float64              `json:"move_points"`
	MoveBucket        string               `json:"move_bucket"`
	MoveATR           float64              `json:"move_atr,omitempty"`
	MovePct           float64              `json:"move_pct"`
	DurationMinutes   float64              `json:"duration_minutes"`
	HitStage          string               `json:"hit_stage"`
	FirstSignal       *SwingSignal         `json:"first_signal,omitempty"`
	SignalDelayMin    float64              `json:"signal_delay_minutes,omitempty"`
	MoveBeforeSignal  float64              `json:"move_before_signal_ratio,omitempty"`
	Trade             *SinglePositionTrade `json:"trade,omitempty"`
	CapturedMoveRatio float64              `json:"captured_move_ratio,omitempty"`
	MissReason        string               `json:"miss_reason,omitempty"`
	OpportunityType   string               `json:"opportunity_type,omitempty"`
	Evidence          *SwingEvidence       `json:"evidence,omitempty"`
}

const MarketSwingDefinitionVersion = "absolute-points-v1"

// MarketSwing is a strategy- and run-independent market fact.
type MarketSwing struct {
	SwingID           string
	Exchange          string
	Market            string
	Symbol            string
	Interval          string
	DefinitionVersion string
	MinimumMovePoints float64
	ReversalPoints    float64
	StartTimeMS       int64
	EndTimeMS         int64
	Side              strategy.SignalSide
	StartPrice        float64
	EndPrice          float64
	MovePoints        float64
	MoveBucket        string
	MovePct           float64
	DurationMinutes   float64
}

type SwingReviewReport struct {
	ThresholdMode     SwingThresholdMode `json:"threshold_mode"`
	MinimumMovePoints float64            `json:"minimum_move_points"`
	ReversalPoints    float64            `json:"reversal_points"`
	ATRPeriod         int                `json:"atr_period,omitempty"`
	MinimumMoveATR    float64            `json:"minimum_move_atr,omitempty"`
	ReversalATR       float64            `json:"reversal_atr,omitempty"`
	Opportunities     []SwingOpportunity `json:"opportunities"`
	UpSwings          int                `json:"up_swings"`
	DownSwings        int                `json:"down_swings"`
	EarlyHits         int                `json:"early_hits"`
	MiddleHits        int                `json:"middle_hits"`
	LateHits          int                `json:"late_hits"`
	Missed            int                `json:"missed"`
	Traded            int                `json:"traded"`
	WinningTrades     int                `json:"winning_trades"`
	NetPnL            float64            `json:"net_pnl"`
}

type swingPoint struct {
	timeMS int64
	price  float64
	atr    float64
}

func ReviewSwings(bars []marketmodel.Kline, signals []SwingSignal, evidence []SwingEvidence, trades []SinglePositionTrade, config SwingReviewConfig) (SwingReviewReport, error) {
	if config.Mode == "" {
		config.Mode = SwingThresholdPoints
	}
	if err := validateSwingReviewConfig(config); err != nil {
		return SwingReviewReport{}, err
	}
	points, err := detectSwingPoints(bars, config)
	if err != nil {
		return SwingReviewReport{}, err
	}
	report := SwingReviewReport{
		ThresholdMode: config.Mode, MinimumMovePoints: config.MinimumMovePoints, ReversalPoints: config.ReversalPoints,
		ATRPeriod: config.ATRPeriod, MinimumMoveATR: config.MinimumMoveATR, ReversalATR: config.ReversalATR,
	}
	for i := 1; i < len(points); i++ {
		start, end := points[i-1], points[i]
		move := math.Abs(end.price - start.price)
		moveATR := 0.0
		if start.atr > 0 {
			moveATR = move / start.atr
		}
		if !swingMoveEligible(config, move, moveATR) {
			continue
		}
		side := strategy.SignalSideBuy
		if end.price < start.price {
			side = strategy.SignalSideSell
		}
		op := SwingOpportunity{
			StartTimeMS: start.timeMS, EndTimeMS: end.timeMS, Side: side,
			StartPrice: start.price, EndPrice: end.price, MovePoints: move,
			MoveBucket: swingMoveBucket(config.Mode, move, moveATR), MoveATR: moveATR, MovePct: move / start.price * 100,
			DurationMinutes: float64(end.timeMS-start.timeMS) / 60000, HitStage: "missed",
		}
		if side == strategy.SignalSideBuy {
			report.UpSwings++
		} else {
			report.DownSwings++
		}
		for index := range signals {
			signal := signals[index]
			if signal.Side != side || signal.TimeMS < start.timeMS-config.LeadWindowMS || signal.TimeMS > end.timeMS {
				continue
			}
			op.FirstSignal = &signal
			op.SignalDelayMin = float64(signal.TimeMS-start.timeMS) / 60000
			if end.timeMS > start.timeMS {
				op.MoveBeforeSignal = float64(signal.TimeMS-start.timeMS) / float64(end.timeMS-start.timeMS)
			}
			switch {
			case signal.TimeMS < start.timeMS:
				op.HitStage = "prepositioned"
				report.EarlyHits++
			case op.MoveBeforeSignal <= .2:
				op.HitStage = "early"
				report.EarlyHits++
			case op.MoveBeforeSignal <= .6:
				op.HitStage = "middle"
				report.MiddleHits++
			default:
				op.HitStage = "late"
				report.LateHits++
			}
			if !signal.Allowed {
				op.MissReason = "regime_blocked:" + signal.Reason
			}
			break
		}
		for index := range trades {
			trade := trades[index]
			if trade.Side != side || trade.EntryTimeMS > end.timeMS || trade.ExitTimeMS < start.timeMS {
				continue
			}
			op.Trade = &trade
			report.Traded++
			report.NetPnL += trade.NetPnL
			if trade.NetPnL > 0 {
				report.WinningTrades++
			}
			op.CapturedMoveRatio = math.Max(0, trade.GrossBps*trade.EntryPrice/10000/move)
			break
		}
		if op.FirstSignal == nil {
			op.MissReason = "no_ai_flip"
			op.OpportunityType, op.Evidence = classifyMissedSwing(start, end, side, evidence)
			report.Missed++
		} else if op.Trade == nil && op.MissReason == "" {
			op.MissReason = "not_traded_position_or_cooldown"
		}
		report.Opportunities = append(report.Opportunities, op)
	}
	return report, nil
}

func BuildMarketSwings(exchange, market, symbol, interval string, report SwingReviewReport) []MarketSwing {
	if report.ThresholdMode != "" && report.ThresholdMode != SwingThresholdPoints {
		return nil
	}
	items := make([]MarketSwing, 0, len(report.Opportunities))
	for _, opportunity := range report.Opportunities {
		identity := fmt.Sprintf("%s|%s|%s|%s|%s|%g|%g|%d|%d|%s",
			exchange, market, symbol, interval, MarketSwingDefinitionVersion,
			report.MinimumMovePoints, report.ReversalPoints, opportunity.StartTimeMS,
			opportunity.EndTimeMS, opportunity.Side)
		digest := sha256.Sum256([]byte(identity))
		items = append(items, MarketSwing{
			SwingID: fmt.Sprintf("%x", digest[:16]), Exchange: exchange, Market: market,
			Symbol: symbol, Interval: interval, DefinitionVersion: MarketSwingDefinitionVersion,
			MinimumMovePoints: report.MinimumMovePoints, ReversalPoints: report.ReversalPoints,
			StartTimeMS: opportunity.StartTimeMS, EndTimeMS: opportunity.EndTimeMS, Side: opportunity.Side,
			StartPrice: opportunity.StartPrice, EndPrice: opportunity.EndPrice,
			MovePoints: opportunity.MovePoints, MoveBucket: opportunity.MoveBucket, MovePct: opportunity.MovePct,
			DurationMinutes: opportunity.DurationMinutes,
		})
	}
	return items
}

// SwingMoveBucket assigns every eligible swing to exactly one reporting bucket.
func SwingMoveBucket(movePoints float64) string {
	switch {
	case movePoints >= 150:
		return "150_plus"
	case movePoints >= 100:
		return "100_150"
	case movePoints >= 60:
		return "60_100"
	case movePoints >= 30:
		return "30_60"
	default:
		return "below_30"
	}
}

func SwingMoveATRBucket(moveATR float64) string {
	switch {
	case moveATR >= 8:
		return "8_plus_atr"
	case moveATR >= 5:
		return "5_8_atr"
	case moveATR >= 3:
		return "3_5_atr"
	case moveATR >= 1.5:
		return "1_5_3_atr"
	default:
		return "below_1_5_atr"
	}
}

func swingMoveBucket(mode SwingThresholdMode, movePoints, moveATR float64) string {
	if mode == SwingThresholdATR {
		return SwingMoveATRBucket(moveATR)
	}
	return SwingMoveBucket(movePoints)
}

func swingMoveEligible(config SwingReviewConfig, movePoints, moveATR float64) bool {
	if config.Mode == SwingThresholdATR {
		return moveATR >= config.MinimumMoveATR
	}
	return movePoints >= config.MinimumMovePoints
}

func validateSwingReviewConfig(config SwingReviewConfig) error {
	switch config.Mode {
	case SwingThresholdPoints:
		if config.MinimumMovePoints <= 0 || config.ReversalPoints <= 0 || config.ReversalPoints >= config.MinimumMovePoints {
			return fmt.Errorf("invalid point swing review thresholds")
		}
	case SwingThresholdATR:
		if config.ATRPeriod <= 0 || config.MinimumMoveATR <= 0 || config.ReversalATR <= 0 || config.ReversalATR >= config.MinimumMoveATR {
			return fmt.Errorf("invalid ATR swing review thresholds")
		}
	default:
		return fmt.Errorf("unsupported swing threshold mode %q", config.Mode)
	}
	return nil
}

func classifyMissedSwing(start swingPoint, end swingPoint, side strategy.SignalSide, evidence []SwingEvidence) (string, *SwingEvidence) {
	cutoff := start.timeMS + (end.timeMS-start.timeMS)/5
	priority := []struct{ source, classification string }{
		{"trend_pullback_resume", "pullback_resume_missing"},
		{"trend_platform_breakout", "breakout_missing"},
		{"compression_breakout", "breakout_missing"},
		{"volatility_impulse", "impulse_missing"},
		{"ai_trend", "trend_continuation_missing"},
	}
	for _, wanted := range priority {
		for index := range evidence {
			item := evidence[index]
			if item.Side == side && item.Source == wanted.source && item.TimeMS >= start.timeMS-15*60*1000 && item.TimeMS <= cutoff {
				copy := item
				return wanted.classification, &copy
			}
		}
	}
	return "flip_missing", nil
}

func detectSwingPoints(bars []marketmodel.Kline, config SwingReviewConfig) ([]swingPoint, error) {
	if len(bars) == 0 {
		return nil, nil
	}
	startIndex := 0
	atrValues := make([]float64, len(bars))
	if config.Mode == SwingThresholdATR {
		var err error
		atrValues, startIndex, err = causalATR(bars, config.ATRPeriod)
		if err != nil {
			return nil, err
		}
		if startIndex < 0 {
			return nil, nil
		}
	}
	first, err := strconv.ParseFloat(bars[startIndex].Close, 64)
	if err != nil {
		return nil, err
	}
	pivot := swingPoint{timeMS: bars[startIndex].CloseTime, price: first, atr: atrValues[startIndex]}
	extreme := pivot
	direction := 0
	points := []swingPoint{pivot}
	for index := startIndex; index < len(bars); index++ {
		bar := bars[index]
		high, e := strconv.ParseFloat(bar.High, 64)
		if e != nil {
			return nil, e
		}
		low, e := strconv.ParseFloat(bar.Low, 64)
		if e != nil {
			return nil, e
		}
		minimumMove, reversal := swingThresholds(config, atrValues[index])
		if direction == 0 {
			if high-pivot.price >= minimumMove {
				direction = 1
				extreme = swingPoint{timeMS: bar.CloseTime, price: high, atr: atrValues[index]}
			} else if pivot.price-low >= minimumMove {
				direction = -1
				extreme = swingPoint{timeMS: bar.CloseTime, price: low, atr: atrValues[index]}
			}
			continue
		}
		if direction > 0 {
			if high > extreme.price {
				extreme = swingPoint{timeMS: bar.CloseTime, price: high, atr: atrValues[index]}
			} else if extreme.price-low >= reversal {
				points = append(points, extreme)
				pivot = extreme
				extreme = swingPoint{timeMS: bar.CloseTime, price: low, atr: atrValues[index]}
				direction = -1
			}
		} else {
			if low < extreme.price {
				extreme = swingPoint{timeMS: bar.CloseTime, price: low, atr: atrValues[index]}
			} else if high-extreme.price >= reversal {
				points = append(points, extreme)
				pivot = extreme
				extreme = swingPoint{timeMS: bar.CloseTime, price: high, atr: atrValues[index]}
				direction = 1
			}
		}
	}
	minimumMove, _ := swingThresholds(config, extreme.atr)
	if math.Abs(extreme.price-points[len(points)-1].price) >= minimumMove {
		points = append(points, extreme)
	}
	return points, nil
}

func swingThresholds(config SwingReviewConfig, atrValue float64) (float64, float64) {
	if config.Mode == SwingThresholdATR {
		return config.MinimumMoveATR * atrValue, config.ReversalATR * atrValue
	}
	return config.MinimumMovePoints, config.ReversalPoints
}

func causalATR(bars []marketmodel.Kline, period int) ([]float64, int, error) {
	values := make([]float64, len(bars))
	if len(bars) <= period {
		return values, -1, nil
	}
	previousClose, err := strconv.ParseFloat(bars[0].Close, 64)
	if err != nil {
		return nil, -1, err
	}
	current := 0.0
	for index := 1; index < len(bars); index++ {
		high, highErr := strconv.ParseFloat(bars[index].High, 64)
		low, lowErr := strconv.ParseFloat(bars[index].Low, 64)
		closeValue, closeErr := strconv.ParseFloat(bars[index].Close, 64)
		if highErr != nil {
			return nil, -1, highErr
		}
		if lowErr != nil {
			return nil, -1, lowErr
		}
		if closeErr != nil {
			return nil, -1, closeErr
		}
		trueRange := math.Max(high-low, math.Max(math.Abs(high-previousClose), math.Abs(low-previousClose)))
		if index <= period {
			current += trueRange
			if index == period {
				current /= float64(period)
				values[index] = current
			}
		} else {
			current = (current*float64(period-1) + trueRange) / float64(period)
			values[index] = current
		}
		previousClose = closeValue
	}
	return values, period, nil
}
