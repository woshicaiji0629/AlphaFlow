package binance

import exchangebinance "alphaflow/go-service/pkg/exchangeclient/binance"

type HTTPClient = exchangebinance.HTTPClient
type RESTClient = exchangebinance.RESTClient

func NewRESTClient(baseURL string, httpClient HTTPClient) *RESTClient {
	return exchangebinance.NewRESTClient(baseURL, httpClient)
}
