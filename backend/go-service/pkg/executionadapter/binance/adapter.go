package binance

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/executionaccount"
	"alphaflow/go-service/pkg/executionadapter"
	"alphaflow/go-service/pkg/httpclient"
	"alphaflow/go-service/pkg/strategy"
)

const (
	liveURL    = "https://fapi.binance.com"
	testnetURL = "https://testnet.binancefuture.com"
)

type HTTPClient interface {
	DoBytes(*http.Request) ([]byte, error)
}
type Options struct {
	Account    executionaccount.Account
	Credential executionaccount.Credential
	BaseURL    string
	HTTPClient HTTPClient
	Now        func() time.Time
}
type Adapter struct {
	account    executionaccount.Account
	credential executionaccount.Credential
	baseURL    string
	client     HTTPClient
	now        func() time.Time
}

func New(options Options) (*Adapter, error) {
	if err := options.Account.Validate(); err != nil {
		return nil, err
	}
	if err := options.Credential.Validate("binance"); err != nil {
		return nil, err
	}
	if options.BaseURL == "" {
		options.BaseURL = liveURL
		if options.Account.Environment == executionaccount.EnvironmentTestnet {
			options.BaseURL = testnetURL
		}
	}
	if options.HTTPClient == nil {
		options.HTTPClient = httpclient.New()
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Adapter{account: options.Account, credential: options.Credential, baseURL: strings.TrimRight(options.BaseURL, "/"), client: options.HTTPClient, now: options.Now}, nil
}
func Register(registry *executionadapter.Registry) error {
	return registry.Register("binance", func(a executionaccount.Account, c executionaccount.Credential) (executionadapter.Adapter, error) {
		return New(Options{Account: a, Credential: c})
	})
}

func (a *Adapter) TestConnection(ctx context.Context) error { _, err := a.Account(ctx); return err }
func (a *Adapter) Account(ctx context.Context) (execution.AccountSnapshot, error) {
	body, err := a.signedGet(ctx, "/fapi/v3/balance", nil)
	if err != nil {
		return execution.AccountSnapshot{}, err
	}
	var rows []struct {
		Asset, Balance, AvailableBalance, CrossUnPnl string
		UpdateTime                                   int64
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return execution.AccountSnapshot{}, fmt.Errorf("decode binance balance: %w", err)
	}
	for _, row := range rows {
		if row.Asset == "USDT" {
			return execution.AccountSnapshot{Scope: string(scope(a.account.Environment)), Account: a.account.ID, Exchange: "binance", Market: a.account.Market, Equity: row.Balance, AvailableBalance: row.AvailableBalance, UnrealizedPnL: row.CrossUnPnl, UpdatedAt: row.UpdateTime}, nil
		}
	}
	return execution.AccountSnapshot{}, fmt.Errorf("binance USDT balance missing")
}
func (a *Adapter) Positions(ctx context.Context) ([]strategy.Position, error) {
	body, err := a.signedGet(ctx, "/fapi/v3/positionRisk", nil)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Symbol, PositionAmt, EntryPrice, PositionSide string
		UpdateTime                                    int64
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("decode binance positions: %w", err)
	}
	result := []strategy.Position{}
	for _, row := range rows {
		amount, err := strconv.ParseFloat(row.PositionAmt, 64)
		if err != nil || amount == 0 {
			continue
		}
		side := strategy.PositionSideLong
		if amount < 0 {
			side = strategy.PositionSideShort
			amount = -amount
		}
		positionSide := strategy.ExchangePositionSide(strings.ToLower(row.PositionSide))
		if positionSide == "both" {
			positionSide = strategy.ExchangePositionSideNet
		}
		result = append(result, strategy.Position{Scope: scope(a.account.Environment), Account: a.account.ID, Exchange: "binance", Market: a.account.Market, Symbol: row.Symbol, Mode: strategy.ExchangePositionModeNet, PositionSide: positionSide, Side: side, Size: amount, EntryPrice: row.EntryPrice, UpdatedAt: row.UpdateTime})
	}
	return result, nil
}
func (a *Adapter) Execute(context.Context, execution.OrderIntent) (execution.ExecutionReport, error) {
	return execution.ExecutionReport{}, fmt.Errorf("binance trading is not enabled")
}
func (a *Adapter) CancelOrder(context.Context, string, string) error {
	return fmt.Errorf("binance trading is not enabled")
}
func (a *Adapter) OpenOrders(ctx context.Context, symbol string) ([]execution.ExchangeOrder, error) {
	query := map[string]string{}
	if symbol != "" {
		query["symbol"] = symbol
	}
	body, err := a.signedGet(ctx, "/fapi/v1/openOrders", query)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		OrderID                                                                  int64  `json:"orderId"`
		ClientOrderID                                                            string `json:"clientOrderId"`
		Symbol, Side, PositionSide, Type, Status, OrigQty, ExecutedQty, AvgPrice string
		ReduceOnly                                                               bool
		Time, UpdateTime                                                         int64
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("decode binance open orders: %w", err)
	}
	orders := make([]execution.ExchangeOrder, 0, len(rows))
	for _, row := range rows {
		quantity, _ := strconv.ParseFloat(row.OrigQty, 64)
		filled, _ := strconv.ParseFloat(row.ExecutedQty, 64)
		orders = append(orders, execution.ExchangeOrder{Exchange: "binance", Account: a.account.ID, Symbol: row.Symbol, OrderID: strconv.FormatInt(row.OrderID, 10), ClientOrderID: row.ClientOrderID, Side: execution.OrderSide(strings.ToLower(row.Side)), PositionSide: strings.ToLower(row.PositionSide), Type: execution.OrderType(strings.ToLower(row.Type)), Status: mapStatus(row.Status), Quantity: quantity, FilledQuantity: filled, AveragePrice: row.AvgPrice, ReduceOnly: row.ReduceOnly, CreatedAt: row.Time, UpdatedAt: row.UpdateTime})
	}
	return orders, nil
}
func (a *Adapter) Capability(ctx context.Context, symbol string) (execution.SymbolCapability, error) {
	body, err := a.publicGet(ctx, "/fapi/v1/exchangeInfo", map[string]string{"symbol": symbol})
	if err != nil {
		return execution.SymbolCapability{}, err
	}
	var response struct {
		ServerTime int64
		Symbols    []struct {
			Symbol, Status string
			Filters        []struct{ FilterType, MinQty, MaxQty, StepSize, TickSize, Notional string }
		}
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return execution.SymbolCapability{}, fmt.Errorf("decode binance exchange info: %w", err)
	}
	for _, item := range response.Symbols {
		if item.Symbol != symbol {
			continue
		}
		capability := execution.SymbolCapability{Exchange: "binance", Market: a.account.Market, Symbol: symbol, ContractSize: "1", UpdatedAt: response.ServerTime}
		for _, filter := range item.Filters {
			switch filter.FilterType {
			case "LOT_SIZE":
				capability.MinQty = filter.MinQty
				capability.QtyStep = filter.StepSize
			case "MARKET_LOT_SIZE":
				capability.MaxOrderQty = filter.MaxQty
			case "PRICE_FILTER":
				capability.PriceTick = filter.TickSize
			case "MIN_NOTIONAL":
				capability.MinNotional = filter.Notional
			}
		}
		return capability, nil
	}
	return execution.SymbolCapability{}, fmt.Errorf("binance symbol %s missing", symbol)
}
func (a *Adapter) Recover(ctx context.Context, intent execution.OrderIntent) (execution.ExecutionReport, bool, error) {
	body, err := a.signedGet(ctx, "/fapi/v1/order", map[string]string{"symbol": intent.Symbol, "origClientOrderId": intent.IntentID})
	if err != nil {
		return execution.ExecutionReport{}, false, err
	}
	var row struct {
		ClientOrderID                 string `json:"clientOrderId"`
		OrderID                       int64  `json:"orderId"`
		Status, ExecutedQty, AvgPrice string
		UpdateTime                    int64
	}
	if err := json.Unmarshal(body, &row); err != nil {
		return execution.ExecutionReport{}, false, err
	}
	status := mapStatus(row.Status)
	quantity, _ := strconv.ParseFloat(row.ExecutedQty, 64)
	return execution.ExecutionReport{IntentID: intent.IntentID, ExchangeOrderID: strconv.FormatInt(row.OrderID, 10), Status: status, FilledQuantity: quantity, AveragePrice: row.AvgPrice, UpdatedAt: row.UpdateTime}, true, nil
}
func (a *Adapter) signedGet(ctx context.Context, path string, query map[string]string) ([]byte, error) {
	values := url.Values{}
	for key, value := range query {
		values.Set(key, value)
	}
	values.Set("timestamp", strconv.FormatInt(a.now().UnixMilli(), 10))
	values.Set("recvWindow", "5000")
	mac := hmac.New(sha256.New, []byte(a.credential.APISecret))
	_, _ = mac.Write([]byte(values.Encode()))
	values.Set("signature", hex.EncodeToString(mac.Sum(nil)))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+path+"?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-MBX-APIKEY", a.credential.APIKey)
	body, err := a.client.DoBytes(req)
	if err != nil {
		return nil, fmt.Errorf("binance %s: %w", path, err)
	}
	return body, nil
}

func (a *Adapter) publicGet(ctx context.Context, path string, query map[string]string) ([]byte, error) {
	values := url.Values{}
	for key, value := range query {
		values.Set(key, value)
	}
	endpoint := a.baseURL + path
	if encoded := values.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	body, err := a.client.DoBytes(req)
	if err != nil {
		return nil, fmt.Errorf("binance %s: %w", path, err)
	}
	return body, nil
}
func mapStatus(value string) execution.ExecutionStatus {
	switch value {
	case "FILLED":
		return execution.ExecutionStatusFilled
	case "PARTIALLY_FILLED":
		return execution.ExecutionStatusPartial
	case "CANCELED", "EXPIRED":
		return execution.ExecutionStatusCanceled
	case "REJECTED":
		return execution.ExecutionStatusRejected
	default:
		return execution.ExecutionStatusAccepted
	}
}
func scope(environment executionaccount.Environment) strategy.PositionScope {
	if environment == executionaccount.EnvironmentLive {
		return strategy.PositionScopeLive
	}
	return strategy.PositionScopeTestnet
}
