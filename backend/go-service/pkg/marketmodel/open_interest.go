package marketmodel

type OpenInterest struct {
	Exchange     string `json:"exchange"`
	Market       string `json:"market"`
	Symbol       string `json:"symbol"`
	OpenInterest string `json:"open_interest"`
	Time         int64  `json:"time"`
}
