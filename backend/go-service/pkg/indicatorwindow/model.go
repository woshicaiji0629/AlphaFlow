package indicatorwindow

import model "alphaflow/go-service/pkg/marketmodel"

const (
	Version         = "v1"
	DefaultLookback = 20
)

type Result struct {
	OpenTime       int64
	CloseTime      int64
	Version        string
	Values         map[string]string
	NumericValues  map[string]float64
	Signals        map[string]string
	NumericWindows []NumericWindow
	SignalWindows  []SignalWindow
}

// NumericWindow is the grouped representation of one numeric indicator's
// rolling statistics. It avoids expanding a stable indicator key into a set
// of string-keyed map entries on the backtest path.
type NumericWindow struct {
	Name             string
	Count            int
	Latest           float64
	Previous         float64
	Change           float64
	ChangePct        float64
	Slope            float64
	Direction        string
	RisingCount      int
	FallingCount     int
	StableCount      int
	Minimum          float64
	Maximum          float64
	RangePositionPct float64
}

// SignalWindow is the grouped representation of one signal's rolling state.
type SignalWindow struct {
	Name           string
	Count          int
	Latest         string
	Previous       string
	StableCount    int
	LastChangedAgo int
}

type point struct {
	openTime      int64
	closeTime     int64
	values        map[string]string
	numericValues map[string]float64
	signals       map[string]string
}

func pointFromSnapshot(snapshot model.IndicatorSnapshot) point {
	return point{
		openTime:      snapshot.OpenTime,
		closeTime:     snapshot.CloseTime,
		values:        snapshot.Values,
		numericValues: snapshot.NumericValues,
		signals:       snapshot.Signals,
	}
}
