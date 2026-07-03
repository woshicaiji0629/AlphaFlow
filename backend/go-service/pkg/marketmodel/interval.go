package marketmodel

import "fmt"

func IntervalMillis(interval string) (int64, error) {
	switch interval {
	case "1m":
		return 60 * 1000, nil
	case "3m":
		return 3 * 60 * 1000, nil
	case "5m":
		return 5 * 60 * 1000, nil
	case "10m":
		return 10 * 60 * 1000, nil
	case "15m":
		return 15 * 60 * 1000, nil
	case "30m":
		return 30 * 60 * 1000, nil
	case "1h":
		return 60 * 60 * 1000, nil
	case "2h":
		return 2 * 60 * 60 * 1000, nil
	case "4h":
		return 4 * 60 * 60 * 1000, nil
	default:
		return 0, fmt.Errorf("unsupported interval %q", interval)
	}
}
