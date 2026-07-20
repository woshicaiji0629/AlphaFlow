package indicatorcalc

type streamEMAState struct {
	period       int
	sum          float64
	count        int
	value        float64
	previous     float64
	hasPrevious  bool
	history      [5]float64
	historyCount int
	ready        bool
}

type streamDEMATEMAState struct {
	second streamEMAState
	third  streamEMAState
}

func newStreamDEMATEMAState(period int) streamDEMATEMAState {
	return streamDEMATEMAState{
		second: *newStreamEMAState(period),
		third:  *newStreamEMAState(period),
	}
}

func (s *streamDEMATEMAState) append(first *streamEMAState) {
	if s == nil || first == nil || !first.ready {
		return
	}
	s.second.append(first.value)
	if s.second.ready {
		s.third.append(s.second.value)
	}
}

func (s *streamDEMATEMAState) value(first *streamEMAState) (float64, float64, bool, bool) {
	if s == nil || first == nil {
		return 0, 0, false, false
	}
	demaOK := first.ready && s.second.ready
	temaOK := demaOK && s.third.ready
	var demaValue, temaValue float64
	if demaOK {
		demaValue = 2*first.value - s.second.value
	}
	if temaOK {
		temaValue = 3*first.value - 3*s.second.value + s.third.value
	}
	return demaValue, temaValue, demaOK, temaOK
}

func newStreamEMAState(period int) *streamEMAState {
	return &streamEMAState{period: period}
}

func (s *streamEMAState) clone() *streamEMAState {
	if s == nil {
		return nil
	}
	return &streamEMAState{
		period:       s.period,
		sum:          s.sum,
		count:        s.count,
		value:        s.value,
		previous:     s.previous,
		hasPrevious:  s.hasPrevious,
		history:      s.history,
		historyCount: s.historyCount,
		ready:        s.ready,
	}
}

func (s *streamEMAState) append(value float64) {
	if s == nil || s.period <= 0 {
		return
	}
	if !s.ready {
		s.sum += value
		s.count++
		if s.count == s.period {
			s.value = s.sum / float64(s.period)
			s.ready = true
		}
		return
	}
	multiplier := 2 / float64(s.period+1)
	if s.historyCount < len(s.history) {
		s.history[s.historyCount] = s.value
		s.historyCount++
	} else {
		copy(s.history[:len(s.history)-1], s.history[1:])
		s.history[len(s.history)-1] = s.value
	}
	s.previous = s.value
	s.hasPrevious = true
	s.value = (value-s.value)*multiplier + s.value
}

func (s *streamEMAState) historicalValue(offset int) (float64, bool) {
	if s == nil || offset <= 0 || offset > s.historyCount {
		return 0, false
	}
	return s.history[s.historyCount-offset], true
}
