package indicatorcalc

type streamRSIState struct {
	period  int
	count   int
	avgGain float64
	avgLoss float64
	ready   bool
	series  []float64
}

func newStreamRSIState(period int) streamRSIState {
	return streamRSIState{period: period}
}

func (s streamRSIState) clone() streamRSIState {
	return s.cloneWithExtraCapacity(0)
}

func (s streamRSIState) cloneWithExtraCapacity(extra int) streamRSIState {
	s.series = cloneSliceWithExtra(s.series, extra)
	return s
}

func (s *streamRSIState) append(previous float64, current float64) {
	if s == nil || s.period <= 0 {
		return
	}
	delta := current - previous
	gain := 0.0
	loss := 0.0
	if delta >= 0 {
		gain = delta
	} else {
		loss = -delta
	}
	if !s.ready {
		s.avgGain += gain
		s.avgLoss += loss
		s.count++
		if s.count == s.period {
			s.avgGain /= float64(s.period)
			s.avgLoss /= float64(s.period)
			s.ready = true
			s.series = append(s.series, rsiFromAverages(s.avgGain, s.avgLoss))
		}
		return
	}
	s.avgGain = (s.avgGain*float64(s.period-1) + gain) / float64(s.period)
	s.avgLoss = (s.avgLoss*float64(s.period-1) + loss) / float64(s.period)
	s.series = append(s.series, rsiFromAverages(s.avgGain, s.avgLoss))
}
