package indicatorcalc

func directionFlipSignal(previous string, current string) string {
	switch {
	case previous != current && current == "up":
		return "buy"
	case previous != current && current == "down":
		return "sell"
	default:
		return "none"
	}
}

func directionFlipCross(previous string, current string) string {
	switch {
	case previous != current && current == "bull":
		return "golden"
	case previous != current && current == "bear":
		return "dead"
	default:
		return "none"
	}
}
