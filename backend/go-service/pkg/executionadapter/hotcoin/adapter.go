package hotcoin

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

const defaultURL = "https://api-ct.hotcoin.fit"

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
	account       executionaccount.Account
	credential    executionaccount.Credential
	baseURL, host string
	client        HTTPClient
	now           func() time.Time
}
type order struct {
	ID                                                            json.RawMessage `json:"id"`
	ContractCode, DetailSide, Amount, DealAmount, AvgPrice, Price string
	Status, SystemType                                            int
	CreatedDate, UpdatedDate                                      int64
	Tag                                                           string
}

func New(o Options) (*Adapter, error) {
	if err := o.Account.Validate(); err != nil {
		return nil, err
	}
	if err := o.Credential.Validate("hotcoin"); err != nil {
		return nil, err
	}
	if o.Account.Environment == executionaccount.EnvironmentTestnet {
		return nil, fmt.Errorf("hotcoin testnet is not supported by the official API")
	}
	if o.BaseURL == "" {
		o.BaseURL = defaultURL
	}
	u, err := url.Parse(o.BaseURL)
	if err != nil {
		return nil, err
	}
	if o.HTTPClient == nil {
		o.HTTPClient = httpclient.New()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Adapter{o.Account, o.Credential, strings.TrimRight(o.BaseURL, "/"), strings.ToLower(u.Host), o.HTTPClient, o.Now}, nil
}
func Register(r *executionadapter.Registry) error {
	return r.Register("hotcoin", func(a executionaccount.Account, c executionaccount.Credential) (executionadapter.Adapter, error) {
		return New(Options{Account: a, Credential: c})
	})
}
func (a *Adapter) TestConnection(ctx context.Context) error { _, err := a.Account(ctx); return err }
func (a *Adapter) ClientOrderID(intentID string) string {
	return executionadapter.ClientOrderID("af", intentID, 16)
}

func (a *Adapter) Account(ctx context.Context) (execution.AccountSnapshot, error) {
	body, err := a.request(ctx, http.MethodGet, "/api/v1/perpetual/account/assets", nil, nil)
	if err != nil {
		return execution.AccountSnapshot{}, err
	}
	var rows []struct{ CurrencyCode, AvailableBalance, PositionAccountRights, UnRealizedSurplus string }
	if err := json.Unmarshal(body, &rows); err != nil {
		return execution.AccountSnapshot{}, fmt.Errorf("decode hotcoin assets: %w", err)
	}
	for _, r := range rows {
		if strings.EqualFold(r.CurrencyCode, "USDT") {
			return execution.AccountSnapshot{Scope: string(scope(a.account.Environment)), Account: a.account.ID, Exchange: "hotcoin", Market: a.account.Market, Equity: r.PositionAccountRights, AvailableBalance: r.AvailableBalance, UnrealizedPnL: r.UnRealizedSurplus, UpdatedAt: a.now().UnixMilli()}, nil
		}
	}
	return execution.AccountSnapshot{}, fmt.Errorf("hotcoin USDT account missing")
}
func (a *Adapter) Positions(ctx context.Context) ([]strategy.Position, error) {
	body, err := a.request(ctx, http.MethodGet, "/api/v1/perpetual/position/list", nil, nil)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID                                    json.RawMessage
		ContractCode, Side, Amount, OpenPrice string
		UpdatedDate                           int64
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("decode hotcoin positions: %w", err)
	}
	out := []strategy.Position{}
	for _, r := range rows {
		size, _ := strconv.ParseFloat(r.Amount, 64)
		if size == 0 {
			continue
		}
		side := strategy.PositionSideLong
		ps := strategy.ExchangePositionSideLong
		if strings.EqualFold(r.Side, "short") {
			side = strategy.PositionSideShort
			ps = strategy.ExchangePositionSideShort
		}
		out = append(out, strategy.Position{Scope: scope(a.account.Environment), Account: a.account.ID, Exchange: "hotcoin", Market: a.account.Market, Symbol: r.ContractCode, Mode: strategy.ExchangePositionModeHedge, PositionSide: ps, Side: side, Size: size, EntryPrice: r.OpenPrice, UpdatedAt: r.UpdatedDate})
	}
	return out, nil
}
func (a *Adapter) Execute(ctx context.Context, i execution.OrderIntent) (execution.ExecutionReport, error) {
	if err := executionadapter.EnsureTradingEnabled(a.account); err != nil {
		return execution.ExecutionReport{}, err
	}
	if i.Quantity <= 0 {
		return execution.ExecutionReport{}, fmt.Errorf("quantity must be positive")
	}
	payload := map[string]any{"type": 11, "side": hotcoinSide(i), "price": "0", "amount": i.Quantity, "tag": executionadapter.ClientOrderID("af", i.IntentID, 16)}
	if i.Type == execution.OrderTypeLimit {
		payload["type"] = 10
		payload["price"] = i.LimitPrice
	}
	body, _ := json.Marshal(payload)
	raw, err := a.request(ctx, http.MethodPost, "/api/v1/perpetual/products/"+url.PathEscape(i.Symbol)+"/order", nil, body)
	if err != nil {
		return execution.ExecutionReport{}, err
	}
	var result struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return execution.ExecutionReport{}, fmt.Errorf("decode hotcoin order: %w", err)
	}
	id := rawID(result.ID)
	if id == "" {
		return execution.ExecutionReport{}, fmt.Errorf("hotcoin order id missing")
	}
	return execution.ExecutionReport{IntentID: i.IntentID, ExchangeOrderID: id, Status: execution.ExecutionStatusAccepted, UpdatedAt: a.now().UnixMilli()}, nil
}
func (a *Adapter) CancelOrder(ctx context.Context, symbol, intentID string) error {
	if err := executionadapter.EnsureTradingEnabled(a.account); err != nil {
		return err
	}
	o, ok, err := a.findOrder(ctx, symbol, intentID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("hotcoin order for intent %s missing", intentID)
	}
	_, err = a.request(ctx, http.MethodDelete, "/api/v1/perpetual/products/"+url.PathEscape(symbol)+"/order/"+url.PathEscape(o.OrderID), nil, nil)
	return err
}
func (a *Adapter) OpenOrders(ctx context.Context, symbol string) ([]execution.ExchangeOrder, error) {
	if strings.TrimSpace(symbol) == "" {
		return nil, fmt.Errorf("hotcoin open orders requires symbol")
	}
	body, err := a.request(ctx, http.MethodGet, "/api/v1/perpetual/products/"+url.PathEscape(symbol)+"/list", nil, nil)
	if err != nil {
		return nil, err
	}
	var rows []order
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("decode hotcoin orders: %w", err)
	}
	out := make([]execution.ExchangeOrder, 0, len(rows))
	for _, r := range rows {
		out = append(out, mapOrder(a.account.ID, r))
	}
	return out, nil
}
func (a *Adapter) Capability(ctx context.Context, symbol string) (execution.SymbolCapability, error) {
	body, err := a.request(ctx, http.MethodGet, "/api/v1/perpetual/public", nil, nil)
	if err != nil {
		return execution.SymbolCapability{}, err
	}
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		var w struct{ Data []map[string]any }
		if e := json.Unmarshal(body, &w); e != nil {
			return execution.SymbolCapability{}, fmt.Errorf("decode hotcoin products: %w", err)
		}
		rows = w.Data
	}
	for _, r := range rows {
		if first(r, "code", "contractCode") != symbol {
			continue
		}
		qtyStep := decimalPrecision(first(r, "minTradeDigit"))
		return execution.SymbolCapability{Exchange: "hotcoin", Market: a.account.Market, Symbol: symbol, MinQty: qtyStep, QtyStep: qtyStep, PriceTick: decimalPrecision(first(r, "minQuoteDigit")), MaxLeverage: first(r, "maxLever", "maxLeverage"), MaxOrderQty: first(r, "maxOrderVolume", "maxOrderAmount"), ContractSize: first(r, "unitAmount", "faceValue", "contractSize"), QuantityUnit: "contract", UpdatedAt: a.now().UnixMilli()}, nil
	}
	return execution.SymbolCapability{}, fmt.Errorf("hotcoin symbol %s missing", symbol)
}
func (a *Adapter) Recover(ctx context.Context, i execution.OrderIntent) (execution.ExecutionReport, bool, error) {
	o, ok, err := a.findOrder(ctx, i.Symbol, i.IntentID)
	if err != nil || !ok {
		return execution.ExecutionReport{}, ok, err
	}
	return execution.ExecutionReport{IntentID: i.IntentID, ExchangeOrderID: o.OrderID, Status: o.Status, FilledQuantity: o.FilledQuantity, AveragePrice: o.AveragePrice, UpdatedAt: o.UpdatedAt}, true, nil
}
func (a *Adapter) findOrder(ctx context.Context, symbol, intentID string) (execution.ExchangeOrder, bool, error) {
	tag := executionadapter.ClientOrderID("af", intentID, 16)
	for _, path := range []string{"/list", "/history-list"} {
		body, err := a.request(ctx, http.MethodGet, "/api/v1/perpetual/products/"+url.PathEscape(symbol)+path, nil, nil)
		if err != nil {
			return execution.ExchangeOrder{}, false, err
		}
		var rows []order
		if err := json.Unmarshal(body, &rows); err != nil {
			return execution.ExchangeOrder{}, false, err
		}
		for _, r := range rows {
			if r.Tag == tag {
				return mapOrder(a.account.ID, r), true, nil
			}
		}
	}
	return execution.ExchangeOrder{}, false, nil
}
func (a *Adapter) request(ctx context.Context, method, path string, q map[string]string, body []byte) ([]byte, error) {
	v := url.Values{"AccessKeyId": {a.credential.APIKey}, "SignatureMethod": {"HmacSHA256"}, "SignatureVersion": {"2"}, "Timestamp": {a.now().UTC().Format("2006-01-02T15:04:05")}}
	for k, x := range q {
		v.Set(k, x)
	}
	canonical := v.Encode()
	signing := method + "\n" + a.host + "\n" + path + "\n" + canonical
	mac := hmac.New(sha256.New, []byte(a.credential.APISecret))
	_, _ = mac.Write([]byte(signing))
	v.Set("Signature", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path+"?"+v.Encode(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	out, err := a.client.DoBytes(req)
	if err != nil {
		return nil, fmt.Errorf("hotcoin %s: %w", path, err)
	}
	return out, nil
}
func mapOrder(account string, r order) execution.ExchangeOrder {
	qty, _ := strconv.ParseFloat(r.Amount, 64)
	filled, _ := strconv.ParseFloat(r.DealAmount, 64)
	direction := strings.ToLower(r.DetailSide)
	side := execution.OrderSideBuy
	if strings.Contains(direction, "short") || strings.Contains(direction, "sell") {
		side = execution.OrderSideSell
	}
	ps := "long"
	if strings.Contains(direction, "short") {
		ps = "short"
	}
	typ := execution.OrderTypeLimit
	if r.SystemType == 11 {
		typ = execution.OrderTypeMarket
	}
	return execution.ExchangeOrder{Exchange: "hotcoin", Account: account, Symbol: r.ContractCode, OrderID: rawID(r.ID), ClientOrderID: r.Tag, Side: side, PositionSide: ps, Type: typ, Status: status(r.Status), Quantity: qty, FilledQuantity: filled, AveragePrice: r.AvgPrice, ReduceOnly: strings.Contains(direction, "close"), CreatedAt: r.CreatedDate, UpdatedAt: r.UpdatedDate}
}
func rawID(v json.RawMessage) string { return strings.Trim(string(v), "\"") }
func status(v int) execution.ExecutionStatus {
	switch v {
	case 1:
		return execution.ExecutionStatusPartial
	case 2:
		return execution.ExecutionStatusFilled
	case -1:
		return execution.ExecutionStatusCanceled
	default:
		return execution.ExecutionStatusAccepted
	}
}
func hotcoinSide(i execution.OrderIntent) string {
	if i.ReduceOnly || i.Action == execution.OrderActionClose || i.Action == execution.OrderActionReduce {
		if strings.EqualFold(i.PositionSide, "short") {
			return "close_short"
		}
		return "close_long"
	}
	if strings.EqualFold(i.PositionSide, "short") || i.Side == execution.OrderSideSell {
		return "open_short"
	}
	return "open_long"
}
func first(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
			return fmt.Sprint(v)
		}
	}
	return ""
}
func decimalPrecision(value string) string {
	digits, err := strconv.Atoi(value)
	if err != nil || digits <= 0 {
		return "1"
	}
	return "0." + strings.Repeat("0", digits-1) + "1"
}
func scope(e executionaccount.Environment) strategy.PositionScope {
	if e == executionaccount.EnvironmentLive {
		return strategy.PositionScopeLive
	}
	return strategy.PositionScopeTestnet
}
