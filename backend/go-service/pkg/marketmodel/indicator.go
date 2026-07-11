package marketmodel

type FeatureMetadata struct {
	SchemaVersion     string `json:"schema_version,omitempty"`
	CalculatorVersion string `json:"calculator_version,omitempty"`
	ParameterHash     string `json:"parameter_hash,omitempty"`
}

type IndicatorSnapshot struct {
	Exchange  string            `json:"exchange"`
	Market    string            `json:"market"`
	Symbol    string            `json:"symbol"`
	Interval  string            `json:"interval"`
	OpenTime  int64             `json:"open_time"`
	CloseTime int64             `json:"close_time"`
	Values    map[string]string `json:"values"`
	Signals   map[string]string `json:"signals,omitempty"`
	Feature   FeatureMetadata   `json:"feature,omitempty"`
	UpdatedAt int64             `json:"updated_at"`
}

type IndicatorWindowSnapshot struct {
	Exchange  string            `json:"exchange"`
	Market    string            `json:"market"`
	Symbol    string            `json:"symbol"`
	Interval  string            `json:"interval"`
	OpenTime  int64             `json:"open_time"`
	CloseTime int64             `json:"close_time"`
	Version   string            `json:"version"`
	Values    map[string]string `json:"values"`
	Signals   map[string]string `json:"signals,omitempty"`
	Feature   FeatureMetadata   `json:"feature,omitempty"`
	UpdatedAt int64             `json:"updated_at"`
}

type IndicatorRealtimeSnapshot struct {
	Exchange  string            `json:"exchange"`
	Market    string            `json:"market"`
	Symbol    string            `json:"symbol"`
	Interval  string            `json:"interval"`
	OpenTime  int64             `json:"open_time"`
	CloseTime int64             `json:"close_time"`
	Kline     Kline             `json:"kline"`
	Values    map[string]string `json:"values"`
	Signals   map[string]string `json:"signals,omitempty"`
	Feature   FeatureMetadata   `json:"feature,omitempty"`
	UpdatedAt int64             `json:"updated_at"`
}
