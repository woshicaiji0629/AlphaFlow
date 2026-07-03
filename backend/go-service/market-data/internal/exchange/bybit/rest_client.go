package bybit

import exchangebybit "alphaflow/go-service/pkg/exchangeclient/bybit"

type HTTPClient = exchangebybit.HTTPClient
type RESTClient = exchangebybit.RESTClient

func NewRESTClient(baseURL string, category string, httpClient HTTPClient) *RESTClient {
	return exchangebybit.NewRESTClient(baseURL, category, httpClient)
}

func bybitInterval(interval string) string {
	return exchangebybit.Interval(interval)
}
