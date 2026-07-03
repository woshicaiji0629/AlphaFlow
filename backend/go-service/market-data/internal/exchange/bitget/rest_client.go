package bitget

import exchangebitget "alphaflow/go-service/pkg/exchangeclient/bitget"

type HTTPClient = exchangebitget.HTTPClient
type RESTClient = exchangebitget.RESTClient

func NewRESTClient(baseURL string, productType string, httpClient HTTPClient) *RESTClient {
	return exchangebitget.NewRESTClient(baseURL, productType, httpClient)
}
