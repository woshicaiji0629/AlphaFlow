package gate

import exchangegate "alphaflow/go-service/pkg/exchangeclient/gate"

type HTTPClient = exchangegate.HTTPClient
type RESTClient = exchangegate.RESTClient

func NewRESTClient(baseURL string, settle string, httpClient HTTPClient) *RESTClient {
	return exchangegate.NewRESTClient(baseURL, settle, httpClient)
}
