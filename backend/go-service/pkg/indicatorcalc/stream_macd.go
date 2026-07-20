package indicatorcalc

type macdConfig struct {
	fast   int
	slow   int
	signal int
}

type streamMACDState struct {
	fastEMA     streamEMAState
	slowEMA     streamEMAState
	differences []float64
	signalEMA   streamEMAState
	series      []macdPoint
}

func newStreamMACDState(config macdConfig) *streamMACDState {
	return &streamMACDState{
		fastEMA:   *newStreamEMAState(config.fast),
		slowEMA:   *newStreamEMAState(config.slow),
		signalEMA: *newStreamEMAState(config.signal),
	}
}

func (s *streamMACDState) clone() *streamMACDState {
	return s.cloneWithExtraCapacity(0)
}

func (s *streamMACDState) cloneWithExtraCapacity(extra int) *streamMACDState {
	if s == nil {
		return nil
	}
	return &streamMACDState{
		fastEMA:     *s.fastEMA.clone(),
		slowEMA:     *s.slowEMA.clone(),
		differences: cloneSliceWithExtra(s.differences, extra),
		signalEMA:   *s.signalEMA.clone(),
		series:      cloneSliceWithExtra(s.series, extra),
	}
}

func (s *streamMACDState) append(value float64) {
	if s == nil {
		return
	}
	s.fastEMA.append(value)
	s.slowEMA.append(value)
	if !s.fastEMA.ready || !s.slowEMA.ready {
		return
	}
	difference := s.fastEMA.value - s.slowEMA.value
	s.differences = append(s.differences, difference)
	s.signalEMA.append(difference)
	if !s.signalEMA.ready {
		return
	}
	s.series = append(s.series, macdPoint{
		value:  difference,
		signal: s.signalEMA.value,
		hist:   difference - s.signalEMA.value,
	})
}
