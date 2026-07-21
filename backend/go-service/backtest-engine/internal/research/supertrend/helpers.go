package supertrend

import (
	"fmt"
	"strconv"
	"strings"

	"alphaflow/go-service/pkg/marketregime"
)

func buildRegimeAnalyzer(versionText string, v1Config marketregime.Config, noiseMultiplier float64, catchUpSpeed float64, confirmBars int) (marketregime.Analyzer, error) {
	version := marketregime.Version(strings.ToLower(strings.TrimSpace(versionText)))
	switch version {
	case marketregime.VersionV1:
		return marketregime.NewDetector(v1Config)
	case marketregime.VersionV2:
		return marketregime.NewV2Analyzer(marketregime.DefaultV2Config())
	case marketregime.VersionV3:
		config := marketregime.DefaultV3Config()
		config.NoiseMultiplier = noiseMultiplier
		config.CatchUpSpeed = catchUpSpeed
		config.ConfirmBars = confirmBars
		return marketregime.NewV3Analyzer(config)
	case marketregime.VersionV4:
		return marketregime.NewV4Analyzer(marketregime.DefaultV4Config())
	case marketregime.VersionV5:
		return marketregime.NewV5Analyzer(marketregime.DefaultV5Config())
	case marketregime.VersionV6:
		return marketregime.NewV6Analyzer(marketregime.DefaultV6Config())
	default:
		return nil, fmt.Errorf("unsupported regime version %q", versionText)
	}
}

func parsePositiveList(name string, text string) ([]float64, error) {
	parts := strings.Split(text, ",")
	values := make([]float64, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil || value <= 0 {
			return nil, fmt.Errorf("%s contains invalid positive number %q", name, part)
		}
		values = append(values, value)
	}
	return values, nil
}

func parsePositiveIntList(name string, text string) ([]int, error) {
	parts := strings.Split(text, ",")
	values := make([]int, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || value <= 0 {
			return nil, fmt.Errorf("%s contains invalid positive integer %q", name, part)
		}
		values = append(values, value)
	}
	return values, nil
}
