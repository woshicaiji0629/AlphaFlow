package indicatorcalc

import "math"

type rollingWindow struct {
	period int
	index  int
	count  int
	next   int
	sum    float64
	sumSq  float64
	values []float64
}

func newRollingWindow(period int) *rollingWindow {
	return &rollingWindow{
		period: period,
		values: make([]float64, 0, period),
	}
}

func (r *rollingWindow) clone() *rollingWindow {
	if r == nil {
		return nil
	}
	cloned := *r
	cloned.values = append([]float64(nil), r.values...)
	return &cloned
}

func (r *rollingWindow) append(value float64) {
	if r == nil || r.period <= 0 {
		return
	}
	r.index++
	if r.count == r.period {
		oldest := r.values[r.next]
		r.sum -= oldest
		r.sumSq -= oldest * oldest
		r.values[r.next] = value
	} else {
		r.count++
		r.values = append(r.values, value)
	}
	r.next = (r.next + 1) % r.period
	r.sum += value
	r.sumSq += value * value
	if r.index%4096 == 0 && r.count == r.period {
		r.sum, r.sumSq = 0, 0
		for _, item := range r.values {
			r.sum += item
			r.sumSq += item * item
		}
	}

}

func (r *rollingWindow) ready() bool { return r != nil && r.count == r.period }

func (r *rollingWindow) meanVariance() (float64, float64, bool) {
	if !r.ready() {
		return 0, 0, false
	}
	mean := r.sum / float64(r.period)
	variance := r.sumSq/float64(r.period) - mean*mean
	if variance < 0 && variance > -1e-12*math.Max(1, mean*mean) {
		variance = 0
	}
	return mean, variance, variance >= 0
}

func (r *rollingWindow) rangeValues() (float64, float64, bool) {
	if !r.ready() {
		return 0, 0, false
	}
	maximum, minimum := r.values[0], r.values[0]
	for _, value := range r.values[1:] {
		if value > maximum {
			maximum = value
		}
		if value < minimum {
			minimum = value
		}
	}
	return maximum, minimum, true
}
