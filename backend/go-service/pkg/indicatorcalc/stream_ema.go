package indicatorcalc

type streamEMAState struct {
	period      int
	sum         float64
	count       int
	value       float64
	previous    float64
	hasPrevious bool
	ready       bool
}

func newStreamEMAState(period int) *streamEMAState {
	return &streamEMAState{period: period}
}

func (s *streamEMAState) clone() *streamEMAState {
	if s == nil {
		return nil
	}
	return &streamEMAState{
		period:      s.period,
		sum:         s.sum,
		count:       s.count,
		value:       s.value,
		previous:    s.previous,
		hasPrevious: s.hasPrevious,
		ready:       s.ready,
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
	s.previous = s.value
	s.hasPrevious = true
	s.value = (value-s.value)*multiplier + s.value
}
