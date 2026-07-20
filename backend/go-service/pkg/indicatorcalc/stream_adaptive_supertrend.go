package indicatorcalc

type streamAdaptiveSupertrendState struct {
	atr            streamATRState
	trainingPeriod int
	multiplier     float64
	finalUpper     float64
	finalLower     float64
	direction      string
	previousPoint  trendPoint
	currentPoint   trendPoint
	cluster        volatilityCluster
	pointCount     int
}

func newStreamAdaptiveSupertrendState(period int, multiplier float64, trainingPeriod int) streamAdaptiveSupertrendState {
	return streamAdaptiveSupertrendState{atr: newStreamATRState(period), multiplier: multiplier, trainingPeriod: trainingPeriod}
}

func (s streamAdaptiveSupertrendState) cloneWithExtraCapacity(extra int) streamAdaptiveSupertrendState {
	s.atr = s.atr.cloneWithExtraCapacity(extra)
	return s
}

func (s *streamAdaptiveSupertrendState) append(high float64, low float64, previousClose float64, closeValue float64) {
	if s == nil {
		return
	}
	s.atr.append(high, low, previousClose)
	if len(s.atr.series) < s.trainingPeriod {
		return
	}
	values := s.atr.series[len(s.atr.series)-s.trainingPeriod:]
	cluster, ok := adaptiveVolatilityCluster(values, s.atr.value)
	if !ok {
		return
	}
	mid := (high + low) / 2
	basicUpper := mid + s.multiplier*cluster.assignedATR
	basicLower := mid - s.multiplier*cluster.assignedATR
	if s.pointCount == 0 {
		s.finalUpper = basicUpper
		s.finalLower = basicLower
		s.direction = "down"
		if closeValue >= mid {
			s.direction = "up"
		}
	} else {
		if basicUpper < s.finalUpper || previousClose > s.finalUpper {
			s.finalUpper = basicUpper
		}
		if basicLower > s.finalLower || previousClose < s.finalLower {
			s.finalLower = basicLower
		}
		if s.direction == "down" && closeValue > s.finalUpper {
			s.direction = "up"
		} else if s.direction == "up" && closeValue < s.finalLower {
			s.direction = "down"
		}
	}
	s.previousPoint = s.currentPoint
	s.currentPoint = supertrendPoint(s.finalUpper, s.finalLower, s.direction)
	s.cluster = cluster
	s.pointCount++
}
