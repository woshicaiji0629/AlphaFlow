package supertrend

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
	ribbonTrendV1                                                                   bool
	swingReviewPath                                                                 string
	swingMinimumPoints, swingReversalPoints                                         float64
	stopReviewPath                                                                  string
}

func parseCommandOptions(args []string) (commandOptions, error) {
	commandLine := flag.NewFlagSet("market-research supertrend-signal", flag.ContinueOnError)
	configPath := commandLine.String("config", "configs/supertrend-signal-research.ethusdt-20250801-20251201.toml", "research config path")
	fixedStops := commandLine.String("fixed-stops", "50,70,100,150", "fixed stop margin percentages")
	atrStops := commandLine.String("atr-stops", "1,1.5,2,2.5", "ATR stop multipliers")
	takeProfits := commandLine.String("take-profits", "30,50,75,100,150,200,300,500", "take profit margin percentages")
	horizon := commandLine.Duration("horizon", 12*time.Hour, "maximum observation horizon")
	platformWindowBars := commandLine.Int("platform-window-bars", 12, "closed entry bars in a trend platform")
	platformMaxRangePct := commandLine.Float64("platform-max-range-pct", 0.6, "maximum platform high-low range percentage")
	platformMinVolumeRatio := commandLine.Float64("platform-min-volume-ratio", 1.5, "minimum breakout volume versus platform average")
	platformCooldownBars := commandLine.Int("platform-cooldown-bars", 20, "same-side breakout cooldown in entry bars")
	impulseLookbackBars := commandLine.Int("impulse-lookback-bars", 3, "entry bars used to measure an impulse")
	impulseBreakoutBars := commandLine.Int("impulse-breakout-bars", 10, "prior entry bars used as impulse structure")
	impulseMinMoveATR := commandLine.Float64("impulse-min-move-atr", 1.5, "minimum short-window move in ATR")
	impulseMinVolumeRatio := commandLine.Float64("impulse-min-volume-ratio", 1.5, "minimum impulse volume_ratio20")
	impulseCooldownBars := commandLine.Int("impulse-cooldown-bars", 20, "same-side impulse cooldown in entry bars")
	pullbackTouchDistancePct := commandLine.Float64("pullback-touch-distance-pct", 0.15, "maximum EMA25 touch distance percentage")
	pullbackResumeBars := commandLine.Int("pullback-resume-bars", 3, "prior bars crossed to confirm pullback resume")
	pullbackMaxArmedBars := commandLine.Int("pullback-max-armed-bars", 10, "maximum bars after a pullback touch")
	pullbackMinVolumeRatio := commandLine.Float64("pullback-min-volume-ratio", 1.0, "minimum resume volume_ratio5")
	pullbackCooldownBars := commandLine.Int("pullback-cooldown-bars", 20, "same-side pullback resume cooldown in entry bars")
	eventCooldownBars := commandLine.Int("event-cooldown-bars", 20, "cross-source same-side research event cooldown in entry bars")
	counterTrendWaitBars := commandLine.Int("counter-trend-wait-bars", 3, "entry bars required after a counter-trend flip")
	counterTrendStructureBars := commandLine.Int("counter-trend-structure-bars", 3, "prior entry bars crossed by a counter-trend signal")
	counterTrendMinVolumeRatio := commandLine.Float64("counter-trend-min-volume-ratio", 1.5, "minimum counter-trend volume_ratio20")
	counterTrendSizeFactor := commandLine.Float64("counter-trend-size-factor", 0.25, "research sizing marker for counter-trend signals")
	counterTrendEnabled := commandLine.Bool("counter-trend-enabled", false, "enable the experimental counter-trend entry gate")
	validationObservationBars := commandLine.String("validation-observation-bars", "3,4,5", "entry bars used for early follow-through observations")
	chopWindowBars := commandLine.Int("chop-window-bars", 60, "entry bars used for chop observations")
	chopMaxEfficiency := commandLine.Float64("chop-max-efficiency", 0.25, "maximum price efficiency ratio for chop evidence")
	chopMaxADX := commandLine.Float64("chop-max-adx", 20, "maximum 10m ADX for chop evidence")
	chopMinFlips := commandLine.Int("chop-min-flips", 3, "minimum 10m Supertrend flips for chop evidence")
	chopMaxSlope := commandLine.Float64("chop-max-slope", 0.08, "maximum absolute ATR-normalized 10m Supertrend slope")
	chopMaxRangeATR := commandLine.Float64("chop-max-range-atr", 4, "maximum platform range in 10m ATR")
	chopMinVotes := commandLine.Int("chop-min-votes", 3, "minimum chop evidence votes")
	chopConfirmBars := commandLine.Int("chop-confirm-bars", 10, "entry bars required to confirm chop")
	chopExitBars := commandLine.Int("chop-exit-bars", 5, "entry bars with weak chop evidence required to exit")
	chopBreakoutVolumeRatio := commandLine.Float64("chop-breakout-volume-ratio", 1.5, "minimum volume_ratio20 for a breakout attempt")
	regimeWindowBars := commandLine.Int("regime-window-bars", 60, "entry bars used for market regime observations")
	regimeMinVotes := commandLine.Int("regime-min-dormant-votes", 3, "minimum dormant votes per higher interval")
	regimeMaxMASpreadATR := commandLine.Float64("regime-max-ma-spread-atr", 0.8, "maximum EMA25/EMA99 spread in ATR")
	regimeMaxMACDAxisATR := commandLine.Float64("regime-max-macd-axis-atr", 0.2, "maximum MACD axis distance in ATR")
	regimeMaxMACDHistATR := commandLine.Float64("regime-max-macd-hist-atr", 0.08, "maximum MACD histogram magnitude in ATR")
	regimeMaxMASlopePct := commandLine.Float64("regime-max-ma-slope-pct", 0.08, "maximum absolute EMA25 slope percentage")
	regimeMaxEfficiency := commandLine.Float64("regime-max-efficiency", 0.25, "maximum price efficiency for chop lock")
	regimeMaxRangeATR := commandLine.Float64("regime-max-range-atr", 4, "maximum platform range in entry ATR")
	regimeStallMaxEfficiency := commandLine.Float64("regime-stall-max-efficiency", 0.10, "maximum local price efficiency for a trend stall")
	regimeStallMaxRangeATR := commandLine.Float64("regime-stall-max-range-atr", 3.5, "maximum local stall range in higher-timeframe ATR")
	regimeLockBars := commandLine.Int("regime-lock-bars", 10, "dormant entry bars required to lock new positions")
	regimeUnlockBars := commandLine.Int("regime-unlock-bars", 5, "non-dormant bars required to leave chop lock")
	regimeBreakoutBars := commandLine.Int("regime-breakout-bars", 2, "platform-external closes required to arm trend")
	regimeBreakoutVolume := commandLine.Float64("regime-breakout-volume-ratio", 1.5, "minimum volume_ratio20 for breakout pending")
	regimeMinBreakoutMACDAxis := commandLine.Float64("regime-min-breakout-macd-axis-atr", 0.3, "minimum MACD axis distance in ATR to arm a breakout")
	regimeVersion := commandLine.String("regime-version", "v1", "market regime analyzer version: v1, v2, v3, v4, v5, or v6")
	regimeV3NoiseMultiplier := commandLine.Float64("regime-v3-noise-multiplier", 1.5, "v3 ATR noise threshold multiplier")
	regimeV3CatchUpSpeed := commandLine.Float64("regime-v3-catch-up-speed", 0.35, "v3 volatility filter catch-up speed")
	regimeV3ConfirmBars := commandLine.Int("regime-v3-confirm-bars", 2, "v3 state confirmation bars")
	regimeAppendVersionRunID := commandLine.Bool("regime-append-version-run-id", false, "append the regime version to run_id for side-by-side research")
	regimeRunIDTag := commandLine.String("regime-run-id-tag", "", "optional research-only suffix appended to run_id")
	researchSkipPersist := commandLine.Bool("research-skip-persist", false, "run research in memory without persisting detailed rows")
	singlePositionScan := commandLine.Bool("single-position-scan", false, "compare staged single-position protection parameters in one replay")
	supertrendVersionCompare := commandLine.Bool("supertrend-version-compare", false, "compare standard, SMA-ATR, adaptive, and AI Supertrend flips with identical single-position rules")
	supertrendTradeDiagnostics := commandLine.Bool("supertrend-trade-diagnostics", false, "log standard Supertrend v4 flip decisions and completed trades")
	ribbonTrendV1 := commandLine.Bool("ribbon-trend-v1", false, "run the causal EMA-ribbon and momentum expansion replay")
	swingReviewPath := commandLine.String("swing-review-json", "", "write ETH swing opportunity and AI Supertrend coverage review JSON")
	swingMinimumPoints := commandLine.Float64("swing-minimum-points", 30, "minimum absolute ETH price move included in the swing review")
	swingReversalPoints := commandLine.Float64("swing-reversal-points", 10, "absolute ETH price reversal used to confirm a swing pivot")
	stopReviewPath := commandLine.String("stop-review-json", "", "write initial-stop entry diagnostics and post-stop path review JSON")
	if err := commandLine.Parse(args); err != nil {
		return commandOptions{}, err
	}
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
		ribbonTrendV1:   *ribbonTrendV1,
		swingReviewPath: *swingReviewPath, swingMinimumPoints: *swingMinimumPoints, swingReversalPoints: *swingReversalPoints, stopReviewPath: *stopReviewPath,
	}, nil
}
