package indicatorcalc

const (
	aiSourceOpen = iota
	aiSourceHigh
	aiSourceLow
	aiSourceClose
)

type aiSourceConfig struct {
	memoryDepth     int
	kNeighbors      int
	horizonBars     int
	spacingBars     int
	learnATRFactor  float64
	neuralInfluence float64
	learnRate       float64
	huberDelta      float64
	fisherSpeed     float64
	fisherFloor     float64
	minRows         int
	sourceSmoothLen int
	maLength        int
	stLength        int
	stMultiplier    float64
	stAdaptivity    float64
}

type aiSourceRow struct {
	features [6]float64
	outcome  int
}

type aiSourceScore struct {
	analog float64
	agree  float64
	tight  float64
	count  int
	rank   float64
}

type aiSourceNeuralState struct {
	weights [6]float64
	bias    float64
	mom     [7]float64
	vel     [7]float64
	step    int
}

type aiSourceState struct {
	input               aiSourceInput
	featureCursors      [4]aiSourceFeatureCursor
	banks               [4][]aiSourceRow
	allBank             []aiSourceRow
	weights             [6]float64
	neural              aiSourceNeuralState
	featureRing         [][4][6]float64
	validFeatureRing    [][4]bool
	stLong              float64
	stShort             float64
	stDirection         string
	previousSTDirection string
	currentSelected     int
	previousSelected    int
	selectedCount       int
	lineCount           int
	sourceValue         float64
	maValue             float64
	stLine              float64
	drive               float64
	adaptMultiplier     float64
	scoreValues         [4]float64
	sourceEMA           *aiSourceEMAState
	maEMA               *aiSourceEMAState
}

type aiSourceInput struct {
	sources     [4][]float64
	highs       []float64
	lows        []float64
	closes      []float64
	atr14       []float64
	atr14Offset int
	stATR       []float64
	stATROffset int
	config      aiSourceConfig
}

type aiSourceStep func(*aiSourceState, aiSourceInput, int)

func (s *aiSourceState) append(input aiSourceInput, index int, step aiSourceStep) {
	if s == nil || step == nil || index < 0 || index >= len(input.closes) {
		return
	}
	step(s, input, index)
}

func newAISourceState(input aiSourceInput) *aiSourceState {
	cfg := input.config
	input = cloneAISourceInput(input)
	state := &aiSourceState{
		input:               input,
		weights:             [6]float64{1, 1, 1, 1, 1, 1},
		neural:              aiSourceNeuralState{weights: [6]float64{0.01, 0.01, 0.01, 0.01, 0.01, 0.01}},
		stDirection:         "bull",
		previousSTDirection: "bull",
		sourceEMA:           newAISourceEMAState(cfg.sourceSmoothLen),
		maEMA:               newAISourceEMAState(cfg.maLength),
	}
	for sourceID := range input.sources {
		state.featureCursors[sourceID] = newAISourceFeatureCursor(input.sources[sourceID])
		state.banks[sourceID] = make([]aiSourceRow, 0, cfg.memoryDepth)
	}
	state.allBank = make([]aiSourceRow, 0, cfg.memoryDepth*4)
	ringSize := cfg.horizonBars + 1
	if ringSize < 1 {
		ringSize = 1
	}
	state.featureRing = make([][4][6]float64, ringSize)
	state.validFeatureRing = make([][4]bool, ringSize)
	return state
}

func cloneAISourceInput(input aiSourceInput) aiSourceInput {
	return cloneAISourceInputWithExtra(input, 0)
}

func cloneAISourceInputWithExtra(input aiSourceInput, extra int) aiSourceInput {
	for index := range input.sources {
		input.sources[index] = cloneSliceWithExtra(input.sources[index], extra)
	}
	input.highs = cloneSliceWithExtra(input.highs, extra)
	input.lows = cloneSliceWithExtra(input.lows, extra)
	input.closes = cloneSliceWithExtra(input.closes, extra)
	input.atr14 = cloneSliceWithExtra(input.atr14, extra)
	input.stATR = cloneSliceWithExtra(input.stATR, extra)
	return input
}

func (s *aiSourceState) appendClosed(open float64, high float64, low float64, closeValue float64, atr14Value float64, stATRValue float64) (aiSourceResult, bool) {
	if s == nil {
		return aiSourceResult{}, false
	}
	s.input.sources[aiSourceOpen] = append(s.input.sources[aiSourceOpen], open)
	s.input.sources[aiSourceHigh] = append(s.input.sources[aiSourceHigh], high)
	s.input.sources[aiSourceLow] = append(s.input.sources[aiSourceLow], low)
	s.input.sources[aiSourceClose] = append(s.input.sources[aiSourceClose], closeValue)
	s.input.highs = append(s.input.highs, high)
	s.input.lows = append(s.input.lows, low)
	s.input.closes = append(s.input.closes, closeValue)
	s.input.atr14 = append(s.input.atr14, atr14Value)
	s.input.stATR = append(s.input.stATR, stATRValue)
	for index := range s.featureCursors {
		s.featureCursors[index].source = s.input.sources[index]
	}
	index := len(s.input.closes) - 1
	s.append(s.input, index, appendAISourceState)
	return s.result(s.input.closes)
}

func (s *aiSourceState) result(closes []float64) (aiSourceResult, bool) {
	if s == nil || s.lineCount < 2 || len(closes) == 0 {
		return aiSourceResult{}, false
	}
	flip := "none"
	if s.previousSTDirection == "bear" && s.stDirection == "bull" {
		flip = "buy"
	} else if s.previousSTDirection == "bull" && s.stDirection == "bear" {
		flip = "sell"
	}
	return aiSourceResult{ma: s.maValue, sourceValue: s.sourceValue, drive: s.drive,
		scoreOpen: s.scoreValues[0], scoreHigh: s.scoreValues[1], scoreLow: s.scoreValues[2], scoreClose: s.scoreValues[3],
		supertrend: s.stLine, supertrendDist: percentDistance(closes[len(closes)-1], s.stLine), adaptMultiplier: s.adaptMultiplier,
		selected: aiSourceName(s.currentSelected), changed: s.selectedCount >= 2 && s.currentSelected != s.previousSelected,
		direction: s.stDirection, flip: flip, ready: aiSourceBanksReady(s.banks[:], 20)}, true
}

func (s *aiSourceState) clone() *aiSourceState {
	return s.cloneWithExtraCapacity(0)
}

func (s *aiSourceState) cloneWithExtraCapacity(extra int) *aiSourceState {
	if s == nil {
		return nil
	}
	cloned := *s
	cloned.input = cloneAISourceInputWithExtra(s.input, extra)
	for index := range s.featureCursors {
		if len(cloned.input.sources[index]) > 0 {
			cloned.featureCursors[index].source = cloned.input.sources[index]
		} else {
			cloned.featureCursors[index].source = append([]float64(nil), s.featureCursors[index].source...)
		}
	}
	for index := range s.banks {
		cloned.banks[index] = append([]aiSourceRow(nil), s.banks[index]...)
	}
	cloned.allBank = append([]aiSourceRow(nil), s.allBank...)
	cloned.featureRing = append([][4][6]float64(nil), s.featureRing...)
	cloned.validFeatureRing = append([][4]bool(nil), s.validFeatureRing...)
	if s.sourceEMA != nil {
		sourceEMA := *s.sourceEMA
		cloned.sourceEMA = &sourceEMA
	}
	if s.maEMA != nil {
		maEMA := *s.maEMA
		cloned.maEMA = &maEMA
	}
	return &cloned
}

type aiSourceResult struct {
	ma              float64
	sourceValue     float64
	drive           float64
	scoreOpen       float64
	scoreHigh       float64
	scoreLow        float64
	scoreClose      float64
	supertrend      float64
	supertrendDist  float64
	adaptMultiplier float64
	selected        string
	changed         bool
	direction       string
	flip            string
	ready           bool
}

func defaultAISourceConfig() aiSourceConfig {
	return aiSourceConfig{
		memoryDepth:     40,
		kNeighbors:      9,
		horizonBars:     4,
		spacingBars:     4,
		learnATRFactor:  0.45,
		neuralInfluence: 0.35,
		learnRate:       0.01,
		huberDelta:      0.02,
		fisherSpeed:     0.20,
		fisherFloor:     0.40,
		minRows:         80,
		sourceSmoothLen: 3,
		maLength:        50,
		stLength:        10,
		stMultiplier:    1.7,
		stAdaptivity:    0.80,
	}
}

func addAISourceSwitchingFeatures(values map[string]string, signals map[string]string, opens []float64, highs []float64, lows []float64, closes []float64) {
	addAISourceSwitchingFeaturesWithContextToSet(nil, values, signals, opens, highs, lows, closes, nil)
}

func addAISourceSwitchingFeaturesWithContextToSet(target *ValueSet, values map[string]string, signals map[string]string, opens []float64, highs []float64, lows []float64, closes []float64, features *featureContext) {
	cfg := defaultAISourceConfig()
	var atr14, stATR []float64
	var ok14, okST bool
	if features != nil {
		atr14, ok14 = features.atrSeries(14)
		stATR, okST = features.atrSeries(cfg.stLength)
	}
	result, ok := aiSourceSwitchingWithATR(opens, highs, lows, closes, cfg, atr14, ok14, stATR, okST)
	if !ok {
		signals["ai_source_ready"] = "false"
		return
	}
	setValueTarget(target, values, "ai_source_ma", result.ma, true)
	setValueTarget(target, values, "ai_source_value", result.sourceValue, true)
	setValueTarget(target, values, "ai_source_drive", result.drive, true)
	setValueTarget(target, values, "ai_source_score_open", result.scoreOpen, true)
	setValueTarget(target, values, "ai_source_score_high", result.scoreHigh, true)
	setValueTarget(target, values, "ai_source_score_low", result.scoreLow, true)
	setValueTarget(target, values, "ai_source_score_close", result.scoreClose, true)
	setValueTarget(target, values, "ai_source_supertrend", result.supertrend, true)
	setValueTarget(target, values, "ai_source_supertrend_distance_pct", result.supertrendDist, true)
	setValueTarget(target, values, "ai_source_supertrend_adapt_mult", result.adaptMultiplier, true)
	signals["ai_source_selected"] = result.selected
	signals["ai_source_changed"] = aiSourceBoolSignal(result.changed)
	signals["ai_source_supertrend_direction"] = result.direction
	signals["ai_source_supertrend_flip"] = result.flip
	signals["ai_source_ready"] = aiSourceBoolSignal(result.ready)
}

func aiSourceSwitching(opens []float64, highs []float64, lows []float64, closes []float64, cfg aiSourceConfig) (aiSourceResult, bool) {
	return aiSourceSwitchingWithATR(opens, highs, lows, closes, cfg, nil, false, nil, false)
}

func aiSourceSwitchingWithATR(opens []float64, highs []float64, lows []float64, closes []float64, cfg aiSourceConfig, atr14 []float64, ok14 bool, stATR []float64, okST bool) (aiSourceResult, bool) {
	if !validAISourceInput(opens, highs, lows, closes, cfg) {
		return aiSourceResult{}, false
	}
	if !ok14 {
		atr14, ok14 = atrSeries(highs, lows, closes, 14)
	}
	if !okST {
		stATR, okST = atrSeries(highs, lows, closes, cfg.stLength)
	}
	if !ok14 || !okST {
		return aiSourceResult{}, false
	}
	atr14Offset := len(closes) - len(atr14)
	stATROffset := len(closes) - len(stATR)
	input := aiSourceInput{
		sources:     [4][]float64{opens, highs, lows, closes},
		highs:       highs,
		lows:        lows,
		closes:      closes,
		atr14:       atr14,
		atr14Offset: atr14Offset,
		stATR:       stATR,
		stATROffset: stATROffset,
		config:      cfg,
	}
	state := newAISourceState(input)
	for index := range input.closes {
		state.append(input, index, appendAISourceState)
	}
	return state.result(input.closes)
}

func appendAISourceState(state *aiSourceState, input aiSourceInput, index int) {
	cfg := input.config
	sources := input.sources
	highs := input.highs
	lows := input.lows
	closes := input.closes
	atr14 := input.atr14
	atr14Offset := input.atr14Offset
	stATR := input.stATR
	stATROffset := input.stATROffset
	features := [4][6]float64{}
	validFeatures := [4]bool{}
	atrValue := atrValueAt(index, atr14, atr14Offset)
	for sourceID := 0; sourceID < 4; sourceID++ {
		point := state.featureCursors[sourceID].next(index)
		features[sourceID], validFeatures[sourceID] = aiSourceFeaturesFromPoint(point, sources[sourceID], highs, lows, index, atrValue)
	}
	ringIndex := index % len(state.featureRing)
	state.featureRing[ringIndex] = features
	state.validFeatureRing[ringIndex] = validFeatures
	if sampleIndex := index - cfg.horizonBars; sampleIndex >= 0 {
		outcome := aiSourceOutcome(closes, sampleIndex, index, atrValueAt(sampleIndex, atr14, atr14Offset), cfg.learnATRFactor)
		sampleRingIndex := sampleIndex % len(state.featureRing)
		sampleFeatures := state.featureRing[sampleRingIndex]
		sampleValidFeatures := state.validFeatureRing[sampleRingIndex]
		for sourceID := 0; sourceID < 4; sourceID++ {
			if outcome != 0 && sampleValidFeatures[sourceID] {
				row := aiSourceRow{features: sampleFeatures[sourceID], outcome: outcome}
				state.banks[sourceID] = prependAISourceRow(state.banks[sourceID], row, cfg.memoryDepth)
				state.allBank = prependAISourceRow(state.allBank, row, cfg.memoryDepth*4)
			}
		}
		if sampleValidFeatures[aiSourceClose] && outcome != 0 {
			aiSourceTrainNeural(&state.neural, sampleFeatures[aiSourceClose], outcome, cfg)
		}
	}
	if len(state.allBank) >= cfg.minRows {
		rawWeights := aiSourceFisherWeights(state.allBank, cfg.minRows, cfg.fisherFloor)
		for weightIndex := range state.weights {
			state.weights[weightIndex] += cfg.fisherSpeed * (rawWeights[weightIndex] - state.weights[weightIndex])
		}
	}
	ready := aiSourceBanksReady(state.banks[:], 20)
	scores := [4]aiSourceScore{}
	ranks := [4]float64{0.25, 0.25, 0.25, 0.25}
	if ready {
		for sourceID := 0; sourceID < 4; sourceID++ {
			if validFeatures[sourceID] {
				scores[sourceID] = aiSourceKNNScore(features[sourceID], state.banks[sourceID], state.weights, cfg)
				ranks[sourceID] = aiSourceRank(features[sourceID], scores[sourceID], state.neural, cfg)
			}
		}
	}
	selected := aiSourceBestID(ranks)
	hardSource := sources[selected][index]
	state.sourceValue = state.sourceEMA.append(hardSource)
	state.maValue = state.maEMA.append(state.sourceValue)
	avgAnalog := (scores[0].analog + scores[1].analog + scores[2].analog + scores[3].analog) / 4
	avgAgree := (scores[0].agree + scores[1].agree + scores[2].agree + scores[3].agree) / 4
	avgTight := (scores[0].tight + scores[1].tight + scores[2].tight + scores[3].tight) / 4
	state.drive = clampFloat(absFloat(avgAnalog)*0.20+avgAgree*0.40+avgTight*0.40, 0, 1)
	state.adaptMultiplier = cfg.stMultiplier * (1 + cfg.stAdaptivity*(1-state.drive))
	stATRValue := atrValueAt(index, stATR, stATROffset)
	line := state.sourceValue
	priorSTDirection := state.stDirection
	if stATRValue > 0 {
		upBand := state.sourceValue - state.adaptMultiplier*stATRValue
		downBand := state.sourceValue + state.adaptMultiplier*stATRValue
		if state.lineCount == 0 {
			state.stLong = upBand
			state.stShort = downBand
		} else {
			if closes[index-1] > state.stLong {
				state.stLong = maxFloat(upBand, state.stLong)
			} else {
				state.stLong = upBand
			}
			if closes[index-1] < state.stShort {
				state.stShort = minFloat(downBand, state.stShort)
			} else {
				state.stShort = downBand
			}
			if state.stDirection == "bear" && closes[index] > state.stShort {
				state.stDirection = "bull"
			} else if state.stDirection == "bull" && closes[index] < state.stLong {
				state.stDirection = "bear"
			}
		}
		if state.stDirection == "bull" {
			line = state.stLong
		} else {
			line = state.stShort
		}
	}
	state.stLine = line
	state.previousSTDirection = priorSTDirection
	state.previousSelected = state.currentSelected
	state.currentSelected = selected
	state.selectedCount++
	state.scoreValues = [4]float64{ranks[0], ranks[1], ranks[2], ranks[3]}
	state.lineCount++

}

func validAISourceInput(opens []float64, highs []float64, lows []float64, closes []float64, cfg aiSourceConfig) bool {
	minLength := 140
	if required := cfg.horizonBars + cfg.maLength + cfg.memoryDepth; required > minLength {
		minLength = required
	}
	return len(closes) >= minLength && len(opens) == len(closes) && len(highs) == len(closes) && len(lows) == len(closes)
}
