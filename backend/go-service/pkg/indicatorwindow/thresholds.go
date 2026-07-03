package indicatorwindow

const (
	slopeSteepPct = 0.2
	slopeWeakPct  = 0.05

	maTangleSpreadPct = 0.15

	volumeClimaxRatio    = 2.5
	volumeExpansionRatio = 1.5
	volumeDryRatio       = 0.7
	volumeClimaxZScore   = 2.0

	cmfBullThreshold = 0.05
	cmfBearThreshold = -0.05

	volumeProfileNearPOCPct       = 0.2
	volumeProfileNearValueEdgePct = 0.3

	structureNearLevelPct = 0.5

	rsiOverbought = 70.0
	rsiOversold   = 30.0
	rsiBullLevel  = 55.0
	rsiBearLevel  = 45.0

	candleStrongBodyRatio = 0.7
	candleWeakBodyRatio   = 0.25
	candleLongShadowRatio = 0.6

	recentEventBars      = 3
	recentTrendFlipBars  = 2
	earlyCrossBars       = 2
	minContinuationBars  = 2
	choppyCrossFlipCount = 3
)
