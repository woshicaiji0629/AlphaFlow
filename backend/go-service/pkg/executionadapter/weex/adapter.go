package weex

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

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"alphaflow/go-service/pkg/execution"
	"alphaflow/go-service/pkg/executionaccount"
	"alphaflow/go-service/pkg/executionadapter"
	"alphaflow/go-service/pkg/httpclient"
	"alphaflow/go-service/pkg/strategy"
)

const defaultURL = "https://api-contract.weex.com"
const privateWSURL = "wss://ws-contract.weex.com/v3/ws/private"

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

func New(o Options) (*Adapter, error) {
	if err := o.Account.Validate(); err != nil {
		return nil, err
	}
	if err := o.Credential.Validate("weex"); err != nil {
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
	return &Adapter{o.Account, o.Credential, strings.TrimRight(o.BaseURL, "/"), o.HTTPClient, o.Now}, nil
}
func Register(r *executionadapter.Registry) error {
	return r.Register("weex", func(a executionaccount.Account, c executionaccount.Credential) (executionadapter.Adapter, error) {
		return New(Options{Account: a, Credential: c})
	})
}
func (a *Adapter) TestConnection(ctx context.Context) error { _, err := a.Account(ctx); return err }
func (a *Adapter) ClientOrderID(intentID string) string {
	return executionadapter.ClientOrderID("af-", intentID, 40)
}
func (a *Adapter) StreamPrivate(ctx context.Context, sink executionadapter.PrivateEventSink) error {
	if a.account.Environment == executionaccount.EnvironmentTestnet {
		return fmt.Errorf("weex demo private websocket is not documented; REST reconciliation is required")
	}
	return executionadapter.RunPrivateStream(ctx, func(runCtx context.Context) error {
		timestamp := strconv.FormatInt(a.now().UnixMilli(), 10)
		mac := hmac.New(sha256.New, []byte(a.credential.APISecret))
		_, _ = mac.Write([]byte(timestamp + "/v3/ws/private"))
		header := http.Header{"User-Agent": {"AlphaFlow/1.0"}, "ACCESS-KEY": {a.credential.APIKey}, "ACCESS-PASSPHRASE": {a.credential.Passphrase}, "ACCESS-TIMESTAMP": {timestamp}, "ACCESS-SIGN": {base64.StdEncoding.EncodeToString(mac.Sum(nil))}}
		conn, _, err := websocket.Dial(runCtx, privateWSURL, &websocket.DialOptions{HTTPHeader: header})
		if err != nil {
			return err
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		for id, channel := range []string{"orders", "fill", "positions", "account"} {
			if err := wsjson.Write(runCtx, conn, map[string]any{"method": "SUBSCRIBE", "params": []string{channel}, "id": id + 1}); err != nil {
				return err
			}
		}
		for {
			var raw map[string]any
			if err := wsjson.Read(runCtx, conn, &raw); err != nil {
				return err
			}
			if raw["type"] == "ping" {
				if err := wsjson.Write(runCtx, conn, map[string]any{"method": "PONG", "id": 1}); err != nil {
					return err
				}
				continue
			}
			events, err := parsePrivateEvents(a.account, raw)
			if err != nil {
				return err
			}
			for _, event := range events {
				if err := sink(runCtx, event); err != nil {
					return err
				}
			}
		}
	}, executionadapter.PrivateStreamOptions{})
}

func parsePrivateEvents(account executionaccount.Account, raw map[string]any) ([]executionadapter.PrivateEvent, error) {
	kind := text(raw, "e")
	if kind == "" {
		return nil, nil
	}
	sequence, _ := strconv.ParseInt(text(raw, "v"), 10, 64)
	updated, _ := strconv.ParseInt(text(raw, "E"), 10, 64)
	rows, ok := raw["d"].([]any)
	if !ok {
		return nil, nil
	}
	events := make([]executionadapter.PrivateEvent, 0, len(rows))
	for _, item := range rows {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		event := executionadapter.PrivateEvent{Exchange: "weex", Account: account.ID, Sequence: sequence, UpdatedAt: updated}
		switch kind {
		case "orders":
			o := mapOrder(account.ID, row)
			event.Type = executionadapter.PrivateEventOrder
			event.Order = &o
			if o.Status == execution.ExecutionStatusFilled || o.Status == execution.ExecutionStatusPartial || o.Status == execution.ExecutionStatusCanceled {
				report := execution.ExecutionReport{ExchangeOrderID: o.OrderID, Status: o.Status, FilledQuantity: o.FilledQuantity, AveragePrice: o.AveragePrice, UpdatedAt: o.UpdatedAt}
				event.Report = &report
			}
		case "positions":
			size, _ := strconv.ParseFloat(first(row, "size"), 64)
			side := strategy.PositionSideLong
			ps := strategy.ExchangePositionSideLong
			if strings.EqualFold(first(row, "side"), "SHORT") {
				side = strategy.PositionSideShort
				ps = strategy.ExchangePositionSideShort
			}
			p := strategy.Position{Scope: scope(account.Environment), Account: account.ID, Exchange: "weex", Market: account.Market, Symbol: first(row, "symbol", "contractId"), Mode: strategy.ExchangePositionModeHedge, PositionSide: ps, Side: side, Size: size, EntryPrice: first(row, "openPriceAvg"), UpdatedAt: updated}
			event.Type = executionadapter.PrivateEventPosition
			event.Position = &p
		case "account":
			if !strings.EqualFold(first(row, "coin"), "USDT") {
				continue
			}
			s := execution.AccountSnapshot{Scope: string(scope(account.Environment)), Account: account.ID, Exchange: "weex", Market: account.Market, AvailableBalance: first(row, "amount"), UpdatedAt: updated}
			event.Type = executionadapter.PrivateEventAccount
			event.Snapshot = &s
		case "fill":
			report := execution.ExecutionReport{ExchangeOrderID: first(row, "orderId"), Status: execution.ExecutionStatusPartial, AveragePrice: first(row, "fillPrice"), UpdatedAt: updated}
			report.FilledQuantity, _ = strconv.ParseFloat(first(row, "fillSize"), 64)
			event.Type = executionadapter.PrivateEventFill
			event.Report = &report
		default:
			continue
		}
		events = append(events, event)
	}
	return events, nil
}
func (a *Adapter) Account(ctx context.Context) (execution.AccountSnapshot, error) {
	path := "/capi/v3/account/balance"
	if a.account.Environment == executionaccount.EnvironmentTestnet {
		path = "/capi/v3/sim/balance"
	}
	body, err := a.request(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return execution.AccountSnapshot{}, err
	}
	var rows []struct{ Asset, Balance, AvailableBalance, UnrealizePnl string }
	if err := json.Unmarshal(body, &rows); err != nil {
		return execution.AccountSnapshot{}, fmt.Errorf("decode weex balance: %w", err)
	}
	for _, r := range rows {
		if strings.EqualFold(r.Asset, "USDT") || strings.EqualFold(r.Asset, "SUSDT") {
			return execution.AccountSnapshot{Scope: string(scope(a.account.Environment)), Account: a.account.ID, Exchange: "weex", Market: a.account.Market, Equity: r.Balance, AvailableBalance: r.AvailableBalance, UnrealizedPnL: r.UnrealizePnl, UpdatedAt: a.now().UnixMilli()}, nil
		}
	}
	return execution.AccountSnapshot{}, fmt.Errorf("weex USDT balance missing")
}
func (a *Adapter) Positions(ctx context.Context) ([]strategy.Position, error) {
	path := "/capi/v3/account/position/allPosition"
	if a.account.Environment == executionaccount.EnvironmentTestnet {
		path = "/capi/v3/sim/position/allPosition"
	}
	body, err := a.request(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Symbol       string `json:"symbol"`
		Side         string `json:"side"`
		Size         string `json:"size"`
		OpenPriceAvg string `json:"open_price_avg"`
		MarginMode   string `json:"margin_mode"`
		UpdatedTime  int64  `json:"updated_time"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("decode weex positions: %w", err)
	}
	out := []strategy.Position{}
	for _, r := range rows {
		size, _ := strconv.ParseFloat(r.Size, 64)
		if size == 0 {
			continue
		}
		side := strategy.PositionSideLong
		ps := strategy.ExchangePositionSideLong
		if strings.EqualFold(r.Side, "SHORT") {
			side = strategy.PositionSideShort
			ps = strategy.ExchangePositionSideShort
		}
		out = append(out, strategy.Position{Scope: scope(a.account.Environment), Account: a.account.ID, Exchange: "weex", Market: a.account.Market, Symbol: r.Symbol, Mode: strategy.ExchangePositionModeHedge, PositionSide: ps, Side: side, Size: size, EntryPrice: r.OpenPriceAvg, UpdatedAt: r.UpdatedTime})
	}
	return out, nil
}
func (a *Adapter) Execute(ctx context.Context, intent execution.OrderIntent) (execution.ExecutionReport, error) {
	if err := executionadapter.EnsureTradingEnabled(a.account); err != nil {
		return execution.ExecutionReport{}, err
	}
	if intent.Quantity <= 0 {
		return execution.ExecutionReport{}, fmt.Errorf("quantity must be positive")
	}
	clientID := executionadapter.ClientOrderID("af-", intent.IntentID, 40)
	if a.account.Environment == executionaccount.EnvironmentTestnet {
		payload := map[string]any{"symbol": intent.Symbol, "clientOrderId": clientID, "side": strings.ToUpper(string(intent.Side)), "positionSide": strings.ToUpper(intent.PositionSide), "type": strings.ToUpper(string(intent.Type)), "quantity": strconv.FormatFloat(intent.Quantity, 'f', -1, 64), "reduceOnly": intent.ReduceOnly}
		if intent.Type == execution.OrderTypeLimit {
			payload["price"] = intent.LimitPrice
		}
		body, _ := json.Marshal(payload)
		raw, err := a.request(ctx, http.MethodPost, "/capi/v3/sim/order", nil, body)
		if err != nil {
			return execution.ExecutionReport{}, err
		}
		return decodeReport(raw, intent.IntentID, a.now().UnixMilli())
	}
	direction := weexDirection(intent)
	payload := map[string]any{"symbol": intent.Symbol, "client_oid": clientID, "size": strconv.FormatFloat(intent.Quantity, 'f', -1, 64), "type": direction, "order_type": "0", "match_price": "1", "price": "0", "marginMode": weexMargin(a.account.MarginMode)}
	if intent.Type == execution.OrderTypeLimit {
		payload["match_price"] = "0"
		payload["price"] = intent.LimitPrice
	}
	body, _ := json.Marshal(payload)
	raw, err := a.request(ctx, http.MethodPost, "/capi/v2/order/placeOrder", nil, body)
	if err != nil {
		return execution.ExecutionReport{}, err
	}
	return decodeReport(raw, intent.IntentID, a.now().UnixMilli())
}
func (a *Adapter) CancelOrder(ctx context.Context, symbol, intentID string) error {
	if err := executionadapter.EnsureTradingEnabled(a.account); err != nil {
		return err
	}
	found, ok, err := a.findOrder(ctx, symbol, intentID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("weex order for intent %s missing", intentID)
	}
	path := "/capi/v2/order/cancel_order"
	body, _ := json.Marshal(map[string]string{"symbol": symbol, "orderId": found.OrderID})
	_, err = a.request(ctx, http.MethodPost, path, nil, body)
	return err
}
func (a *Adapter) OpenOrders(ctx context.Context, symbol string) ([]execution.ExchangeOrder, error) {
	q := map[string]string{}
	if symbol != "" {
		q["symbol"] = symbol
	}
	body, err := a.request(ctx, http.MethodGet, "/capi/v2/order/current", q, nil)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		List []map[string]any `json:"list"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("decode weex orders: %w", err)
	}
	out := make([]execution.ExchangeOrder, 0, len(wrapper.List))
	for _, raw := range wrapper.List {
		out = append(out, mapOrder(a.account.ID, raw))
	}
	return out, nil
}
func (a *Adapter) Capability(ctx context.Context, symbol string) (execution.SymbolCapability, error) {
	body, err := a.request(ctx, http.MethodGet, "/capi/v2/market/contracts", nil, nil)
	if err != nil {
		return execution.SymbolCapability{}, err
	}
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		var w struct{ Data []map[string]any }
		if e := json.Unmarshal(body, &w); e != nil {
			return execution.SymbolCapability{}, fmt.Errorf("decode weex contracts: %w", err)
		}
		rows = w.Data
	}
	for _, r := range rows {
		if text(r, "symbol") != symbol {
			continue
		}
		return execution.SymbolCapability{Exchange: "weex", Market: a.account.Market, Symbol: symbol, MinQty: first(r, "minTradeNum", "minOrderSize", "minSize"), QtyStep: first(r, "sizeMultiplier", "sizePlace", "quantityPrecision"), PriceTick: first(r, "priceEndStep", "priceTick", "tickSize"), MaxLeverage: first(r, "maxLever", "maxLeverage"), MaxOrderQty: first(r, "maxMarketOrderQty", "maxOrderSize"), ContractSize: first(r, "contractSize", "contractVal"), QuantityUnit: "base", UpdatedAt: a.now().UnixMilli()}, nil
	}
	return execution.SymbolCapability{}, fmt.Errorf("weex symbol %s missing", symbol)
}
func (a *Adapter) Recover(ctx context.Context, intent execution.OrderIntent) (execution.ExecutionReport, bool, error) {
	o, ok, err := a.findOrder(ctx, intent.Symbol, intent.IntentID)
	if err != nil || !ok {
		return execution.ExecutionReport{}, ok, err
	}
	return execution.ExecutionReport{IntentID: intent.IntentID, ExchangeOrderID: o.OrderID, Status: o.Status, FilledQuantity: o.FilledQuantity, AveragePrice: o.AveragePrice, UpdatedAt: o.UpdatedAt}, true, nil
}
func (a *Adapter) findOrder(ctx context.Context, symbol, intentID string) (execution.ExchangeOrder, bool, error) {
	id := executionadapter.ClientOrderID("af-", intentID, 40)
	orders, err := a.OpenOrders(ctx, symbol)
	if err != nil {
		return execution.ExchangeOrder{}, false, err
	}
	for _, o := range orders {
		if o.ClientOrderID == id {
			return o, true, nil
		}
	}
	body, err := a.request(ctx, http.MethodGet, "/capi/v2/order/history", map[string]string{"symbol": symbol}, nil)
	if err != nil {
		return execution.ExchangeOrder{}, false, err
	}
	var w struct {
		List []map[string]any `json:"list"`
	}
	if err := json.Unmarshal(body, &w); err != nil {
		return execution.ExchangeOrder{}, false, err
	}
	for _, r := range w.List {
		o := mapOrder(a.account.ID, r)
		if o.ClientOrderID == id {
			return o, true, nil
		}
	}
	return execution.ExchangeOrder{}, false, nil
}
func (a *Adapter) request(ctx context.Context, method, path string, q map[string]string, body []byte) ([]byte, error) {
	v := url.Values{}
	for k, x := range q {
		v.Set(k, x)
	}
	raw := v.Encode()
	suffix := ""
	if raw != "" {
		suffix = "?" + raw
	}
	ts := strconv.FormatInt(a.now().UnixMilli(), 10)
	mac := hmac.New(sha256.New, []byte(a.credential.APISecret))
	_, _ = mac.Write([]byte(ts + method + path + suffix + string(body)))
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path+suffix, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("ACCESS-KEY", a.credential.APIKey)
	req.Header.Set("ACCESS-SIGN", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	req.Header.Set("ACCESS-PASSPHRASE", a.credential.Passphrase)
	req.Header.Set("ACCESS-TIMESTAMP", ts)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("locale", "en-US")
	out, err := a.client.DoBytes(req)
	if err != nil {
		return nil, fmt.Errorf("weex %s: %w", path, err)
	}
	return out, nil
}
func decodeReport(body []byte, id string, now int64) (execution.ExecutionReport, error) {
	var r map[string]any
	if err := json.Unmarshal(body, &r); err != nil {
		return execution.ExecutionReport{}, fmt.Errorf("decode weex order: %w", err)
	}
	orderID := first(r, "order_id", "orderId")
	if orderID == "" {
		return execution.ExecutionReport{}, fmt.Errorf("weex order id missing")
	}
	return execution.ExecutionReport{IntentID: id, ExchangeOrderID: orderID, Status: execution.ExecutionStatusAccepted, UpdatedAt: now}, nil
}
func mapOrder(account string, r map[string]any) execution.ExchangeOrder {
	qty, _ := strconv.ParseFloat(first(r, "size", "quantity"), 64)
	filled, _ := strconv.ParseFloat(first(r, "cumFillSize", "filledQty", "filled_size", "filledSize"), 64)
	created, _ := strconv.ParseInt(first(r, "createdTime", "cTime"), 10, 64)
	updated, _ := strconv.ParseInt(first(r, "updatedTime", "uTime"), 10, 64)
	direction := strings.ToLower(first(r, "type", "direction"))
	side := execution.OrderSideBuy
	if strings.Contains(direction, "short") || strings.Contains(direction, "sell") {
		side = execution.OrderSideSell
	}
	ps := "long"
	if strings.Contains(direction, "short") {
		ps = "short"
	}
	return execution.ExchangeOrder{Exchange: "weex", Account: account, Symbol: first(r, "symbol"), OrderID: first(r, "order_id", "orderId"), ClientOrderID: first(r, "client_oid", "clientOrderId"), Side: side, PositionSide: ps, Type: execution.OrderType(strings.ToLower(first(r, "order_type", "orderType"))), Status: weexStatus(strings.ToLower(first(r, "status", "state"))), Quantity: qty, FilledQuantity: filled, AveragePrice: first(r, "avgPrice", "priceAvg"), ReduceOnly: strings.Contains(direction, "close"), CreatedAt: created, UpdatedAt: updated}
}
func text(m map[string]any, k string) string {
	v := m[k]
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		if v != nil {
			return fmt.Sprint(v)
		}
		return ""
	}
}
func first(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v := text(m, k); v != "" {
			return v
		}
	}
	return ""
}
func weexStatus(v string) execution.ExecutionStatus {
	switch v {
	case "filled", "full_fill":
		return execution.ExecutionStatusFilled
	case "partial_fill", "partially_filled":
		return execution.ExecutionStatusPartial
	case "cancelled", "canceled":
		return execution.ExecutionStatusCanceled
	case "rejected":
		return execution.ExecutionStatusRejected
	default:
		return execution.ExecutionStatusAccepted
	}
}
func weexDirection(i execution.OrderIntent) string {
	if i.ReduceOnly || i.Action == execution.OrderActionClose || i.Action == execution.OrderActionReduce {
		if strings.EqualFold(i.PositionSide, "short") {
			return "4"
		}
		return "3"
	}
	if strings.EqualFold(i.PositionSide, "short") || i.Side == execution.OrderSideSell {
		return "2"
	}
	return "1"
}
func weexMargin(m executionaccount.MarginMode) int {
	if m == executionaccount.MarginModeIsolated {
		return 3
	}
	return 1
}
func scope(e executionaccount.Environment) strategy.PositionScope {
	if e == executionaccount.EnvironmentLive {
		return strategy.PositionScopeLive
	}
	return strategy.PositionScopeTestnet
}
