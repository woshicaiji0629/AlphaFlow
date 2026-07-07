package indicatorcalc

type floatWindowValue struct {
	index int
	value float64
}

type floatMonotonicWindow struct {
	values     [256]floatWindowValue
	head       int
	tail       int
	decreasing bool
}

func newFloatMonotonicWindow(decreasing bool) floatMonotonicWindow {
	return floatMonotonicWindow{decreasing: decreasing}
}

func (w *floatMonotonicWindow) canHold(length int) bool {
	return w != nil && length > 0 && length <= len(w.values)
}

func (w *floatMonotonicWindow) push(index int, value float64) {
	if w == nil {
		return
	}
	for w.tail > w.head && w.shouldDrop(w.values[w.tail-1].value, value) {
		w.tail--
	}
	if w.tail >= len(w.values) {
		if w.head == 0 {
			return
		}
		copy(w.values[:], w.values[w.head:w.tail])
		w.tail -= w.head
		w.head = 0
	}
	w.values[w.tail] = floatWindowValue{index: index, value: value}
	w.tail++
}

func (w *floatMonotonicWindow) shouldDrop(current float64, next float64) bool {
	if w.decreasing {
		return current <= next
	}
	return current >= next
}

func (w *floatMonotonicWindow) expireBefore(index int) {
	if w == nil {
		return
	}
	for w.head < w.tail && w.values[w.head].index < index {
		w.head++
	}
	if w.head == w.tail {
		w.head = 0
		w.tail = 0
	}
}

func (w *floatMonotonicWindow) value() (float64, bool) {
	if w == nil || w.head >= w.tail {
		return 0, false
	}
	return w.values[w.head].value, true
}
