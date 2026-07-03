package indicatorcalc

func percentDistance(value float64, base float64) float64 {
	if base == 0 {
		return 0
	}
	return (value - base) / base * 100
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
