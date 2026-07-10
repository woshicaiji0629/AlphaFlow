package bitget

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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

const defaultURL = "https://api.bitget.com"

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
type response[T any] struct {
	Code, Msg   string
	RequestTime int64
	Data        T
}

func New(o Options) (*Adapter, error) {
	if err := o.Account.Validate(); err != nil {
		return nil, err
	}
	if err := o.Credential.Validate("bitget"); err != nil {
		return nil, err
	}
	if o.BaseURL == "" {
		o.BaseURL = defaultURL
	}
	if o.HTTPClient == nil {
		o.HTTPClient = httpclient.New()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Adapter{account: o.Account, credential: o.Credential, baseURL: strings.TrimRight(o.BaseURL, "/"), client: o.HTTPClient, now: o.Now}, nil
}
func Register(r *executionadapter.Registry) error {
	return r.Register("bitget", func(a executionaccount.Account, c executionaccount.Credential) (executionadapter.Adapter, error) {
		return New(Options{Account: a, Credential: c})
	})
}
func (a *Adapter) TestConnection(ctx context.Context) error { _, err := a.Account(ctx); return err }
func (a *Adapter) ClientOrderID(intentID string) string {
	return executionadapter.ClientOrderID("af-", intentID, 40)
}
func (a *Adapter) Account(ctx context.Context) (execution.AccountSnapshot, error) {
	body, err := a.get(ctx, "/api/v2/mix/account/accounts", map[string]string{"productType": "USDT-FUTURES"})
	if err != nil {
		return execution.AccountSnapshot{}, err
	}
	var result response[[]struct{ MarginCoin, AccountEquity, Available, UnrealizedPL string }]
	if err := json.Unmarshal(body, &result); err != nil {
		return execution.AccountSnapshot{}, err
	}
	if result.Code != "00000" {
		return execution.AccountSnapshot{}, fmt.Errorf("bitget account code %s: %s", result.Code, result.Msg)
	}
	for _, row := range result.Data {
		if row.MarginCoin == "USDT" {
			return execution.AccountSnapshot{Scope: string(scope(a.account.Environment)), Account: a.account.ID, Exchange: "bitget", Market: a.account.Market, Equity: row.AccountEquity, AvailableBalance: row.Available, UnrealizedPnL: row.UnrealizedPL, UpdatedAt: result.RequestTime}, nil
		}
	}
	return execution.AccountSnapshot{}, fmt.Errorf("bitget USDT account missing")
}
func (a *Adapter) Positions(ctx context.Context) ([]strategy.Position, error) {
	body, err := a.get(ctx, "/api/v2/mix/position/all-position", map[string]string{"productType": "USDT-FUTURES", "marginCoin": "USDT"})
	if err != nil {
		return nil, err
	}
	var result response[[]struct {
		Symbol, HoldSide, Total, OpenPriceAvg string
		UTime                                 int64
	}]
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.Code != "00000" {
		return nil, fmt.Errorf("bitget positions code %s: %s", result.Code, result.Msg)
	}
	positions := []strategy.Position{}
	for _, row := range result.Data {
		size, err := strconv.ParseFloat(row.Total, 64)
		if err != nil || size == 0 {
			continue
		}
		side := strategy.PositionSideLong
		positionSide := strategy.ExchangePositionSideLong
		if row.HoldSide == "short" {
			side = strategy.PositionSideShort
			positionSide = strategy.ExchangePositionSideShort
		}
		positions = append(positions, strategy.Position{Scope: scope(a.account.Environment), Account: a.account.ID, Exchange: "bitget", Market: a.account.Market, Symbol: row.Symbol, Mode: strategy.ExchangePositionModeHedge, PositionSide: positionSide, Side: side, Size: size, EntryPrice: row.OpenPriceAvg, UpdatedAt: row.UTime})
	}
	return positions, nil
}
func (a *Adapter) Recover(ctx context.Context, intent execution.OrderIntent) (execution.ExecutionReport, bool, error) {
	body, err := a.get(ctx, "/api/v2/mix/order/detail", map[string]string{"symbol": intent.Symbol, "productType": "USDT-FUTURES", "clientOid": executionadapter.ClientOrderID("af-", intent.IntentID, 40)})
	if err != nil {
		return execution.ExecutionReport{}, false, err
	}
	var result response[struct {
		OrderID, ClientOid, State, BaseVolume, PriceAvg string
		UTime                                           int64
	}]
	if err := json.Unmarshal(body, &result); err != nil {
		return execution.ExecutionReport{}, false, err
	}
	if result.Code != "00000" {
		return execution.ExecutionReport{}, false, fmt.Errorf("bitget order code %s: %s", result.Code, result.Msg)
	}
	quantity, _ := strconv.ParseFloat(result.Data.BaseVolume, 64)
	return execution.ExecutionReport{IntentID: intent.IntentID, ExchangeOrderID: result.Data.OrderID, Status: mapStatus(result.Data.State), FilledQuantity: quantity, AveragePrice: result.Data.PriceAvg, UpdatedAt: result.Data.UTime}, true, nil
}
func (a *Adapter) Execute(ctx context.Context, intent execution.OrderIntent) (execution.ExecutionReport, error) {
	if err := executionadapter.EnsureTradingEnabled(a.account); err != nil {
		return execution.ExecutionReport{}, err
	}
	if intent.Quantity <= 0 {
		return execution.ExecutionReport{}, fmt.Errorf("quantity must be positive")
	}
	payload := map[string]any{"symbol": intent.Symbol, "productType": "USDT-FUTURES", "marginMode": bitgetMarginMode(a.account.MarginMode), "marginCoin": "USDT", "size": strconv.FormatFloat(intent.Quantity, 'f', -1, 64), "side": string(intent.Side), "orderType": "market", "clientOid": executionadapter.ClientOrderID("af-", intent.IntentID, 40)}
	if a.account.PositionMode == executionaccount.PositionModeHedge {
		if intent.PositionSide == "long" {
			payload["side"] = "buy"
		} else {
			payload["side"] = "sell"
		}
		if intent.ReduceOnly {
			payload["tradeSide"] = "close"
		} else {
			payload["tradeSide"] = "open"
		}
	} else if intent.ReduceOnly {
		payload["reduceOnly"] = "YES"
	}
	body, _ := json.Marshal(payload)
	responseBody, err := a.request(ctx, http.MethodPost, "/api/v2/mix/order/place-order", nil, body)
	if err != nil {
		return execution.ExecutionReport{}, err
	}
	var result response[struct {
		OrderID string `json:"orderId"`
	}]
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return execution.ExecutionReport{}, err
	}
	if result.Code != "00000" {
		return execution.ExecutionReport{}, fmt.Errorf("bitget place order code %s: %s", result.Code, result.Msg)
	}
	return execution.ExecutionReport{IntentID: intent.IntentID, ExchangeOrderID: result.Data.OrderID, Status: execution.ExecutionStatusAccepted, UpdatedAt: result.RequestTime}, nil
}
func (a *Adapter) CancelOrder(ctx context.Context, symbol string, intentID string) error {
	if err := executionadapter.EnsureTradingEnabled(a.account); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{"symbol": symbol, "productType": "USDT-FUTURES", "marginCoin": "USDT", "clientOid": executionadapter.ClientOrderID("af-", intentID, 40)})
	responseBody, err := a.request(ctx, http.MethodPost, "/api/v2/mix/order/cancel-order", nil, body)
	if err != nil {
		return err
	}
	var result response[any]
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return err
	}
	if result.Code != "00000" {
		return fmt.Errorf("bitget cancel order code %s: %s", result.Code, result.Msg)
	}
	return nil
}
func (a *Adapter) OpenOrders(ctx context.Context, symbol string) ([]execution.ExchangeOrder, error) {
	query := map[string]string{"productType": "USDT-FUTURES"}
	if symbol != "" {
		query["symbol"] = symbol
	}
	body, err := a.get(ctx, "/api/v2/mix/order/orders-pending", query)
	if err != nil {
		return nil, err
	}
	var result response[struct {
		EntrustedList []struct{ Symbol, Size, OrderID, ClientOid, BaseVolume, PriceAvg, Status, Side, PosSide, OrderType, ReduceOnly, CTime, UTime string } `json:"entrustedList"`
	}]
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode bitget open orders: %w", err)
	}
	if result.Code != "00000" {
		return nil, fmt.Errorf("bitget open orders code %s: %s", result.Code, result.Msg)
	}
	orders := make([]execution.ExchangeOrder, 0, len(result.Data.EntrustedList))
	for _, row := range result.Data.EntrustedList {
		quantity, _ := strconv.ParseFloat(row.Size, 64)
		filled, _ := strconv.ParseFloat(row.BaseVolume, 64)
		created, _ := strconv.ParseInt(row.CTime, 10, 64)
		updated, _ := strconv.ParseInt(row.UTime, 10, 64)
		orders = append(orders, execution.ExchangeOrder{Exchange: "bitget", Account: a.account.ID, Symbol: row.Symbol, OrderID: row.OrderID, ClientOrderID: row.ClientOid, Side: execution.OrderSide(row.Side), PositionSide: row.PosSide, Type: execution.OrderType(row.OrderType), Status: mapStatus(row.Status), Quantity: quantity, FilledQuantity: filled, AveragePrice: row.PriceAvg, ReduceOnly: strings.EqualFold(row.ReduceOnly, "YES"), CreatedAt: created, UpdatedAt: updated})
	}
	return orders, nil
}
func (a *Adapter) Capability(ctx context.Context, symbol string) (execution.SymbolCapability, error) {
	body, err := a.get(ctx, "/api/v2/mix/market/contracts", map[string]string{"productType": "USDT-FUTURES", "symbol": symbol})
	if err != nil {
		return execution.SymbolCapability{}, err
	}
	var result response[[]struct{ Symbol, MinTradeNum, PriceEndStep, PricePlace, SizeMultiplier, MinTradeUSDT, MaxLever, MaxMarketOrderQty string }]
	if err := json.Unmarshal(body, &result); err != nil {
		return execution.SymbolCapability{}, fmt.Errorf("decode bitget contracts: %w", err)
	}
	if result.Code != "00000" {
		return execution.SymbolCapability{}, fmt.Errorf("bitget contracts code %s: %s", result.Code, result.Msg)
	}
	for _, row := range result.Data {
		if row.Symbol == symbol {
			return execution.SymbolCapability{Exchange: "bitget", Market: a.account.Market, Symbol: symbol, MinQty: row.MinTradeNum, QtyStep: row.SizeMultiplier, PriceTick: decimalStep(row.PriceEndStep, row.PricePlace), MinNotional: row.MinTradeUSDT, MaxLeverage: row.MaxLever, MaxOrderQty: row.MaxMarketOrderQty, ContractSize: "1", QuantityUnit: "base", UpdatedAt: result.RequestTime}, nil
		}
	}
	return execution.SymbolCapability{}, fmt.Errorf("bitget symbol %s missing", symbol)
}
func (a *Adapter) get(ctx context.Context, path string, query map[string]string) ([]byte, error) {
	return a.request(ctx, http.MethodGet, path, query, nil)
}
func (a *Adapter) request(ctx context.Context, method string, path string, query map[string]string, requestBody []byte) ([]byte, error) {
	values := url.Values{}
	for k, v := range query {
		values.Set(k, v)
	}
	rawQuery := values.Encode()
	timestamp := strconv.FormatInt(a.now().UnixMilli(), 10)
	prehash := timestamp + method + path
	if rawQuery != "" {
		prehash += "?" + rawQuery
	}
	prehash += string(requestBody)
	mac := hmac.New(sha256.New, []byte(a.credential.APISecret))
	_, _ = mac.Write([]byte(prehash))
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path+func() string {
		if rawQuery == "" {
			return ""
		}
		return "?" + rawQuery
	}(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("ACCESS-KEY", a.credential.APIKey)
	req.Header.Set("ACCESS-SIGN", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	req.Header.Set("ACCESS-PASSPHRASE", a.credential.Passphrase)
	req.Header.Set("ACCESS-TIMESTAMP", timestamp)
	if len(requestBody) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if a.account.Environment == executionaccount.EnvironmentTestnet {
		req.Header.Set("paptrading", "1")
	}
	body, err := a.client.DoBytes(req)
	if err != nil {
		return nil, fmt.Errorf("bitget %s: %w", path, err)
	}
	return body, nil
}

func bitgetMarginMode(mode executionaccount.MarginMode) string {
	if mode == executionaccount.MarginModeIsolated {
		return "isolated"
	}
	return "crossed"
}
func mapStatus(v string) execution.ExecutionStatus {
	switch v {
	case "filled":
		return execution.ExecutionStatusFilled
	case "partially_filled":
		return execution.ExecutionStatusPartial
	case "cancelled", "canceled":
		return execution.ExecutionStatusCanceled
	case "rejected":
		return execution.ExecutionStatusRejected
	default:
		return execution.ExecutionStatusAccepted
	}
}
func scope(e executionaccount.Environment) strategy.PositionScope {
	if e == executionaccount.EnvironmentLive {
		return strategy.PositionScopeLive
	}
	return strategy.PositionScopeTestnet
}

func decimalStep(step, places string) string {
	digits, err := strconv.Atoi(places)
	if err != nil || digits <= 0 {
		return step
	}
	step = strings.TrimLeft(step, "0")
	if step == "" {
		step = "0"
	}
	if len(step) > digits {
		return step[:len(step)-digits] + "." + step[len(step)-digits:]
	}
	return "0." + strings.Repeat("0", digits-len(step)) + step
}
