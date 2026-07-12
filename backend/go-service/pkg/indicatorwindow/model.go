package indicatorwindow

import model "alphaflow/go-service/pkg/marketmodel"

const (
	Version         = "v1"
	DefaultLookback = 20
)

type Result struct {
	OpenTime  int64
	CloseTime int64
	Version   string
	Values    map[string]string
	Signals   map[string]string
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
