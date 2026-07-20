package main

import (
	"flag"
	"time"

	"alphaflow/go-service/pkg/marketregime"
	"alphaflow/go-service/pkg/signalresearch"
)

type commandOptions struct {
	configPath, fixedStops, atrStops, takeProfits                                   string
	horizon                                                                         time.Duration
	platform                                                                        signalresearch.PlatformConfig
	impulse                                                                         signalresearch.ImpulseConfig
	pullback                                                                        signalresearch.PullbackConfig
	eventCooldownBars                                                               int
	counterTrend                                                                    signalresearch.CounterTrendConfig
	counterTrendEnabled                                                             bool
	validationBars                                                                  string
	chop                                                                            signalresearch.ChopConfig
	regimeAnalyzer                                                                  marketregime.Analyzer
	appendVersionRunID                                                              bool
	runIDTag                                                                        string
	skipPersist, scanSinglePosition, compareSupertrendVersions, logTradeDiagnostics bool
	swingReviewPath                                                                 string
	swingMinimumPoints, swingReversalPoints                                         float64
	stopReviewPath                                                                  string
}

func parseCommandOptions() (commandOptions, error) {
	configPath := flag.String("config", "configs/supertrend-signal-research.ethusdt-20250801-20251201.toml", "research config path")
	fixedStops := flag.String("fixed-stops", "50,70,100,150", "fixed stop margin percentages")
	atrStops := flag.String("atr-stops", "1,1.5,2,2.5", "ATR stop multipliers")
	takeProfits := flag.String("take-profits", "30,50,75,100,150,200,300,500", "take profit margin percentages")
	horizon := flag.Duration("horizon", 12*time.Hour, "maximum observation horizon")
	platformWindowBars := flag.Int("platform-window-bars", 12, "closed entry bars in a trend platform")
	platformMaxRangePct := flag.Float64("platform-max-range-pct", 0.6, "maximum platform high-low range percentage")
	platformMinVolumeRatio := flag.Float64("platform-min-volume-ratio", 1.5, "minimum breakout volume versus platform average")
	platformCooldownBars := flag.Int("platform-cooldown-bars", 20, "same-side breakout cooldown in entry bars")
	impulseLookbackBars := flag.Int("impulse-lookback-bars", 3, "entry bars used to measure an impulse")
	impulseBreakoutBars := flag.Int("impulse-breakout-bars", 10, "prior entry bars used as impulse structure")
	impulseMinMoveATR := flag.Float64("impulse-min-move-atr", 1.5, "minimum short-window move in ATR")
	impulseMinVolumeRatio := flag.Float64("impulse-min-volume-ratio", 1.5, "minimum impulse volume_ratio20")
	impulseCooldownBars := flag.Int("impulse-cooldown-bars", 20, "same-side impulse cooldown in entry bars")
	pullbackTouchDistancePct := flag.Float64("pullback-touch-distance-pct", 0.15, "maximum EMA25 touch distance percentage")
	pullbackResumeBars := flag.Int("pullback-resume-bars", 3, "prior bars crossed to confirm pullback resume")
	pullbackMaxArmedBars := flag.Int("pullback-max-armed-bars", 10, "maximum bars after a pullback touch")
	pullbackMinVolumeRatio := flag.Float64("pullback-min-volume-ratio", 1.0, "minimum resume volume_ratio5")
	pullbackCooldownBars := flag.Int("pullback-cooldown-bars", 20, "same-side pullback resume cooldown in entry bars")
	eventCooldownBars := flag.Int("event-cooldown-bars", 20, "cross-source same-side research event cooldown in entry bars")
	counterTrendWaitBars := flag.Int("counter-trend-wait-bars", 3, "entry bars required after a counter-trend flip")
	counterTrendStructureBars := flag.Int("counter-trend-structure-bars", 3, "prior entry bars crossed by a counter-trend signal")
	counterTrendMinVolumeRatio := flag.Float64("counter-trend-min-volume-ratio", 1.5, "minimum counter-trend volume_ratio20")
	counterTrendSizeFactor := flag.Float64("counter-trend-size-factor", 0.25, "research sizing marker for counter-trend signals")
	counterTrendEnabled := flag.Bool("counter-trend-enabled", false, "enable the experimental counter-trend entry gate")
	validationObservationBars := flag.String("validation-observation-bars", "3,4,5", "entry bars used for early follow-through observations")
	chopWindowBars := flag.Int("chop-window-bars", 60, "entry bars used for chop observations")
	chopMaxEfficiency := flag.Float64("chop-max-efficiency", 0.25, "maximum price efficiency ratio for chop evidence")
	chopMaxADX := flag.Float64("chop-max-adx", 20, "maximum 10m ADX for chop evidence")
	chopMinFlips := flag.Int("chop-min-flips", 3, "minimum 10m Supertrend flips for chop evidence")
	chopMaxSlope := flag.Float64("chop-max-slope", 0.08, "maximum absolute ATR-normalized 10m Supertrend slope")
	chopMaxRangeATR := flag.Float64("chop-max-range-atr", 4, "maximum platform range in 10m ATR")
	chopMinVotes := flag.Int("chop-min-votes", 3, "minimum chop evidence votes")
	chopConfirmBars := flag.Int("chop-confirm-bars", 10, "entry bars required to confirm chop")
	chopExitBars := flag.Int("chop-exit-bars", 5, "entry bars with weak chop evidence required to exit")
	chopBreakoutVolumeRatio := flag.Float64("chop-breakout-volume-ratio", 1.5, "minimum volume_ratio20 for a breakout attempt")
	regimeWindowBars := flag.Int("regime-window-bars", 60, "entry bars used for market regime observations")
	regimeMinVotes := flag.Int("regime-min-dormant-votes", 3, "minimum dormant votes per higher interval")
	regimeMaxMASpreadATR := flag.Float64("regime-max-ma-spread-atr", 0.8, "maximum EMA25/EMA99 spread in ATR")
	regimeMaxMACDAxisATR := flag.Float64("regime-max-macd-axis-atr", 0.2, "maximum MACD axis distance in ATR")
	regimeMaxMACDHistATR := flag.Float64("regime-max-macd-hist-atr", 0.08, "maximum MACD histogram magnitude in ATR")
	regimeMaxMASlopePct := flag.Float64("regime-max-ma-slope-pct", 0.08, "maximum absolute EMA25 slope percentage")
	regimeMaxEfficiency := flag.Float64("regime-max-efficiency", 0.25, "maximum price efficiency for chop lock")
	regimeMaxRangeATR := flag.Float64("regime-max-range-atr", 4, "maximum platform range in entry ATR")
	regimeStallMaxEfficiency := flag.Float64("regime-stall-max-efficiency", 0.10, "maximum local price efficiency for a trend stall")
	regimeStallMaxRangeATR := flag.Float64("regime-stall-max-range-atr", 3.5, "maximum local stall range in higher-timeframe ATR")
	regimeLockBars := flag.Int("regime-lock-bars", 10, "dormant entry bars required to lock new positions")
	regimeUnlockBars := flag.Int("regime-unlock-bars", 5, "non-dormant bars required to leave chop lock")
	regimeBreakoutBars := flag.Int("regime-breakout-bars", 2, "platform-external closes required to arm trend")
	regimeBreakoutVolume := flag.Float64("regime-breakout-volume-ratio", 1.5, "minimum volume_ratio20 for breakout pending")
	regimeMinBreakoutMACDAxis := flag.Float64("regime-min-breakout-macd-axis-atr", 0.3, "minimum MACD axis distance in ATR to arm a breakout")
	regimeVersion := flag.String("regime-version", "v1", "market regime analyzer version: v1, v2, v3, v4, v5, or v6")
	regimeV3NoiseMultiplier := flag.Float64("regime-v3-noise-multiplier", 1.5, "v3 ATR noise threshold multiplier")
	regimeV3CatchUpSpeed := flag.Float64("regime-v3-catch-up-speed", 0.35, "v3 volatility filter catch-up speed")
	regimeV3ConfirmBars := flag.Int("regime-v3-confirm-bars", 2, "v3 state confirmation bars")
	regimeAppendVersionRunID := flag.Bool("regime-append-version-run-id", false, "append the regime version to run_id for side-by-side research")
	regimeRunIDTag := flag.String("regime-run-id-tag", "", "optional research-only suffix appended to run_id")
	researchSkipPersist := flag.Bool("research-skip-persist", false, "run research in memory without persisting detailed rows")
	singlePositionScan := flag.Bool("single-position-scan", false, "compare staged single-position protection parameters in one replay")
	supertrendVersionCompare := flag.Bool("supertrend-version-compare", false, "compare standard, SMA-ATR, adaptive, and AI Supertrend flips with identical single-position rules")
	supertrendTradeDiagnostics := flag.Bool("supertrend-trade-diagnostics", false, "log standard Supertrend v4 flip decisions and completed trades")
	swingReviewPath := flag.String("swing-review-json", "", "write ETH swing opportunity and AI Supertrend coverage review JSON")
	swingMinimumPoints := flag.Float64("swing-minimum-points", 30, "minimum absolute ETH price move included in the swing review")
	swingReversalPoints := flag.Float64("swing-reversal-points", 10, "absolute ETH price reversal used to confirm a swing pivot")
	stopReviewPath := flag.String("stop-review-json", "", "write initial-stop entry diagnostics and post-stop path review JSON")
	flag.Parse()
	regimeConfig := marketregime.DefaultConfig()
	regimeConfig.WindowBars, regimeConfig.MinDormantVotes = *regimeWindowBars, *regimeMinVotes
	regimeConfig.MaxMASpreadATR, regimeConfig.MaxMACDAxisATR = *regimeMaxMASpreadATR, *regimeMaxMACDAxisATR
	regimeConfig.MaxMACDHistogramATR, regimeConfig.MaxMASlopePct = *regimeMaxMACDHistATR, *regimeMaxMASlopePct
	regimeConfig.MaxEfficiencyRatio, regimeConfig.MaxPlatformRangeATR = *regimeMaxEfficiency, *regimeMaxRangeATR
	regimeConfig.StallMaxEfficiency, regimeConfig.StallMaxRangeATR = *regimeStallMaxEfficiency, *regimeStallMaxRangeATR
	regimeConfig.LockConfirmBars, regimeConfig.UnlockEvidenceBars = *regimeLockBars, *regimeUnlockBars
	regimeConfig.BreakoutConfirmBars, regimeConfig.BreakoutVolumeRatio = *regimeBreakoutBars, *regimeBreakoutVolume
	regimeConfig.MinBreakoutMACDAxisATR = *regimeMinBreakoutMACDAxis
	regimeAnalyzer, err := buildRegimeAnalyzer(*regimeVersion, regimeConfig, *regimeV3NoiseMultiplier, *regimeV3CatchUpSpeed, *regimeV3ConfirmBars)
	if err != nil {
		return commandOptions{}, err
	}
	return commandOptions{
		configPath: *configPath, fixedStops: *fixedStops, atrStops: *atrStops, takeProfits: *takeProfits, horizon: *horizon,
		platform:          signalresearch.PlatformConfig{WindowBars: *platformWindowBars, MaxRangePct: *platformMaxRangePct, MinVolumeRatio: *platformMinVolumeRatio, CooldownBars: *platformCooldownBars},
		impulse:           signalresearch.ImpulseConfig{LookbackBars: *impulseLookbackBars, BreakoutBars: *impulseBreakoutBars, MinMoveATR: *impulseMinMoveATR, MinVolumeRatio: *impulseMinVolumeRatio, CooldownBars: *impulseCooldownBars},
		pullback:          signalresearch.PullbackConfig{TouchDistancePct: *pullbackTouchDistancePct, ResumeBars: *pullbackResumeBars, MaxArmedBars: *pullbackMaxArmedBars, MinVolumeRatio: *pullbackMinVolumeRatio, CooldownBars: *pullbackCooldownBars},
		eventCooldownBars: *eventCooldownBars, counterTrend: signalresearch.CounterTrendConfig{WaitBars: *counterTrendWaitBars, StructureBars: *counterTrendStructureBars, MinVolumeRatio: *counterTrendMinVolumeRatio, SizeFactor: *counterTrendSizeFactor}, counterTrendEnabled: *counterTrendEnabled,
		validationBars: *validationObservationBars, chop: signalresearch.ChopConfig{WindowBars: *chopWindowBars, MaxEfficiencyRatio: *chopMaxEfficiency, MaxADX: *chopMaxADX, MinFlips: *chopMinFlips, MaxNormalizedSlope: *chopMaxSlope, MaxRangeATR: *chopMaxRangeATR, MinVotes: *chopMinVotes, ConfirmBars: *chopConfirmBars, ExitBars: *chopExitBars, BreakoutVolumeRatio: *chopBreakoutVolumeRatio},
		regimeAnalyzer: regimeAnalyzer, appendVersionRunID: *regimeAppendVersionRunID, runIDTag: *regimeRunIDTag, skipPersist: *researchSkipPersist, scanSinglePosition: *singlePositionScan, compareSupertrendVersions: *supertrendVersionCompare, logTradeDiagnostics: *supertrendTradeDiagnostics,
		swingReviewPath: *swingReviewPath, swingMinimumPoints: *swingMinimumPoints, swingReversalPoints: *swingReversalPoints, stopReviewPath: *stopReviewPath,
	}, nil
}
