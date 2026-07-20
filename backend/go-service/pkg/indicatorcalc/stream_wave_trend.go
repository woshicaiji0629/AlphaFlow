package indicatorcalc

type streamWaveTrendState struct {
	channelEMA   streamEMAState
	deviationEMA streamEMAState
	ciEMA        streamEMAState
	values       [5]float64
	count        int
}

func newStreamWaveTrendState(channelLength int, averageLength int) streamWaveTrendState {
	return streamWaveTrendState{
		channelEMA:   *newStreamEMAState(channelLength),
		deviationEMA: *newStreamEMAState(channelLength),
		ciEMA:        *newStreamEMAState(averageLength),
	}
}

func (s streamWaveTrendState) clone() streamWaveTrendState {
	return s
}

func (s *streamWaveTrendState) append(typical float64) {
	if s == nil {
		return
	}
	s.channelEMA.append(typical)
	if !s.channelEMA.ready {
		return
	}
	deviation := absFloat(typical - s.channelEMA.value)
	s.deviationEMA.append(deviation)
	if !s.deviationEMA.ready {
		return
	}
	ci := 0.0
	if s.deviationEMA.value != 0 {
		ci = (typical - s.channelEMA.value) / (0.015 * s.deviationEMA.value)
	}
	s.ciEMA.append(ci)
	if !s.ciEMA.ready {
		return
	}
	if s.count < len(s.values) {
		s.values[s.count] = s.ciEMA.value
		s.count++
		return
	}
	copy(s.values[:], s.values[1:])
	s.values[len(s.values)-1] = s.ciEMA.value
}

func (s *streamWaveTrendState) value() (float64, float64, float64, float64, float64, bool) {
	if s == nil || s.count < len(s.values) {
		return 0, 0, 0, 0, 0, false
	}
	wt1 := s.values[4]
	previousWT1 := s.values[3]
	wt2 := (s.values[1] + s.values[2] + s.values[3] + s.values[4]) / 4
	previousWT2 := (s.values[0] + s.values[1] + s.values[2] + s.values[3]) / 4
	return wt1, wt2, previousWT1, previousWT2, previousWT1 - previousWT2, true
}
