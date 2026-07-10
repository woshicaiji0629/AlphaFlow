package deepcoin

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

const defaultURL = "https://api.deepcoin.com"
const privateWSURL = "wss://stream.deepcoin.com/v1/private"

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
	Code json.RawMessage `json:"code"`
	Msg  string          `json:"msg"`
	Data T               `json:"data"`
}
type order struct{ InstID, OrdID, ClOrdID, Px, Sz, OrdType, Side, PosSide, AccFillSz, AvgPx, State, ReduceOnly, UTime, CTime string }

func New(o Options) (*Adapter, error) {
	if err := o.Account.Validate(); err != nil {
		return nil, err
	}
	if err := o.Credential.Validate("deepcoin"); err != nil {
		return nil, err
	}
	if o.Account.Environment == executionaccount.EnvironmentTestnet {
		return nil, fmt.Errorf("deepcoin testnet is not supported by the official API")
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
	return r.Register("deepcoin", func(a executionaccount.Account, c executionaccount.Credential) (executionadapter.Adapter, error) {
		return New(Options{Account: a, Credential: c})
	})
}
func (a *Adapter) TestConnection(ctx context.Context) error { _, err := a.Account(ctx); return err }
func (a *Adapter) ClientOrderID(intentID string) string     { return clientID(intentID) }
func (a *Adapter) StreamPrivate(ctx context.Context, sink executionadapter.PrivateEventSink) error {
	return executionadapter.RunPrivateStream(ctx, func(runCtx context.Context) error {
		body, err := a.get(runCtx, "/deepcoin/listenkey/acquire", nil)
		if err != nil {
			return err
		}
		var key response[struct {
			ListenKey string `json:"listenkey"`
		}]
		if err := decode(body, &key); err != nil {
			return err
		}
		if key.Data.ListenKey == "" {
			return fmt.Errorf("deepcoin listen key missing")
		}
		conn, _, err := websocket.Dial(runCtx, privateWSURL+"?listenKey="+url.QueryEscape(key.Data.ListenKey), nil)
		if err != nil {
			return err
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		if err := wsjson.Write(runCtx, conn, map[string]any{"action": "subscribe", "tables": []string{"Account", "Order", "Position", "Trade"}}); err != nil {
			return err
		}
		for {
			var raw map[string]any
			if err := wsjson.Read(runCtx, conn, &raw); err != nil {
				return err
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
	items, ok := raw["result"].([]any)
	if !ok {
		return nil, nil
	}
	out := make([]executionadapter.PrivateEvent, 0, len(items))
	for _, item := range items {
		wrapper, ok := item.(map[string]any)
		if !ok {
			continue
		}
		table := fmt.Sprint(wrapper["table"])
		data, ok := wrapper["data"].(map[string]any)
		if !ok {
			continue
		}
		updated := number(data, "UM")
		if updated == 0 {
			updated = number(data, "U") * 1000
		}
		event := executionadapter.PrivateEvent{Exchange: "deepcoin", Account: account.ID, UpdatedAt: updated}
		switch table {
		case "Order":
			qty := numberFloat(data, "V")
			filled := numberFloat(data, "v")
			side := execution.OrderSideBuy
			if fmt.Sprint(data["D"]) == "1" {
				side = execution.OrderSideSell
			}
			ps := "long"
			if fmt.Sprint(data["p"]) == "1" {
				ps = "short"
			}
			o := execution.ExchangeOrder{Exchange: "deepcoin", Account: account.ID, Symbol: fmt.Sprint(data["I"]), OrderID: fmt.Sprint(data["OS"]), ClientOrderID: fmt.Sprint(data["L"]), Side: side, PositionSide: ps, Status: deepcoinWSStatus(fmt.Sprint(data["Or"])), Quantity: qty, FilledQuantity: filled, AveragePrice: fmt.Sprint(data["t"]), UpdatedAt: updated}
			event.Type = executionadapter.PrivateEventOrder
			event.Order = &o
			if filled > 0 || o.Status == execution.ExecutionStatusCanceled {
				r := execution.ExecutionReport{ExchangeOrderID: o.OrderID, Status: o.Status, FilledQuantity: filled, AveragePrice: o.AveragePrice, UpdatedAt: updated}
				event.Report = &r
			}
		case "Trade":
			r := execution.ExecutionReport{ExchangeOrderID: fmt.Sprint(data["OS"]), Status: execution.ExecutionStatusPartial, FilledQuantity: numberFloat(data, "V"), AveragePrice: fmt.Sprint(data["P"]), Fee: numberFloat(data, "F"), UpdatedAt: number(data, "TT") * 1000}
			event.Type = executionadapter.PrivateEventFill
			event.Report = &r
		case "Position":
			size := numberFloat(data, "Po")
			side := strategy.PositionSideLong
			ps := strategy.ExchangePositionSideLong
			if fmt.Sprint(data["p"]) == "1" {
				side = strategy.PositionSideShort
				ps = strategy.ExchangePositionSideShort
			}
			p := strategy.Position{Scope: scope(account.Environment), Account: account.ID, Exchange: "deepcoin", Market: account.Market, Symbol: fmt.Sprint(data["I"]), Mode: strategy.ExchangePositionModeHedge, PositionSide: ps, Side: side, Size: size, EntryPrice: fmt.Sprint(data["OP"]), UpdatedAt: updated}
			event.Type = executionadapter.PrivateEventPosition
			event.Position = &p
		case "Account":
			if fmt.Sprint(data["C"]) != "USDT" {
				continue
			}
			s := execution.AccountSnapshot{Scope: string(scope(account.Environment)), Account: account.ID, Exchange: "deepcoin", Market: account.Market, Equity: fmt.Sprint(data["B"]), AvailableBalance: fmt.Sprint(data["a"]), UsedMargin: fmt.Sprint(data["u"]), UpdatedAt: updated}
			event.Type = executionadapter.PrivateEventAccount
			event.Snapshot = &s
		default:
			continue
		}
		out = append(out, event)
	}
	return out, nil
}
func number(m map[string]any, key string) int64 {
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	}
	return 0
}
func numberFloat(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case json.Number:
		n, _ := v.Float64()
		return n
	case string:
		n, _ := strconv.ParseFloat(v, 64)
		return n
	}
	return 0
}
func deepcoinWSStatus(v string) execution.ExecutionStatus {
	switch v {
	case "1":
		return execution.ExecutionStatusFilled
	case "2":
		return execution.ExecutionStatusPartial
	case "5", "6":
		return execution.ExecutionStatusCanceled
	default:
		return execution.ExecutionStatusAccepted
	}
}
func (a *Adapter) Account(ctx context.Context) (execution.AccountSnapshot, error) {
	body, err := a.get(ctx, "/deepcoin/account/balances", map[string]string{"instType": "SWAP"})
	if err != nil {
		return execution.AccountSnapshot{}, err
	}
	var result response[[]struct{ Ccy, Bal, AvailBal, Upl, Eq string }]
	if err := decode(body, &result); err != nil {
		return execution.AccountSnapshot{}, err
	}
	for _, row := range result.Data {
		if strings.EqualFold(row.Ccy, "USDT") {
			return execution.AccountSnapshot{Scope: string(scope(a.account.Environment)), Account: a.account.ID, Exchange: "deepcoin", Market: a.account.Market, Equity: row.Eq, AvailableBalance: row.AvailBal, UnrealizedPnL: row.Upl, UpdatedAt: a.now().UnixMilli()}, nil
		}
	}
	return execution.AccountSnapshot{}, fmt.Errorf("deepcoin USDT account missing")
}
func (a *Adapter) Positions(ctx context.Context) ([]strategy.Position, error) {
	body, err := a.get(ctx, "/deepcoin/account/positions", map[string]string{"instType": "SWAP"})
	if err != nil {
		return nil, err
	}
	var result response[[]struct{ InstID, PosSide, Pos, AvgPx, MgnMode, UTime string }]
	if err := decode(body, &result); err != nil {
		return nil, err
	}
	positions := []strategy.Position{}
	for _, row := range result.Data {
		size, _ := strconv.ParseFloat(row.Pos, 64)
		if size == 0 {
			continue
		}
		side := strategy.PositionSideLong
		ps := strategy.ExchangePositionSideLong
		if row.PosSide == "short" {
			side = strategy.PositionSideShort
			ps = strategy.ExchangePositionSideShort
		}
		updated, _ := strconv.ParseInt(row.UTime, 10, 64)
		positions = append(positions, strategy.Position{Scope: scope(a.account.Environment), Account: a.account.ID, Exchange: "deepcoin", Market: a.account.Market, Symbol: row.InstID, Mode: strategy.ExchangePositionModeHedge, PositionSide: ps, Side: side, Size: size, EntryPrice: row.AvgPx, UpdatedAt: updated})
	}
	return positions, nil
}
func (a *Adapter) Execute(ctx context.Context, intent execution.OrderIntent) (execution.ExecutionReport, error) {
	if err := executionadapter.EnsureTradingEnabled(a.account); err != nil {
		return execution.ExecutionReport{}, err
	}
	if intent.Quantity <= 0 {
		return execution.ExecutionReport{}, fmt.Errorf("quantity must be positive")
	}
	payload := map[string]any{"instId": intent.Symbol, "tdMode": marginMode(a.account.MarginMode), "clOrdId": clientID(intent.IntentID), "side": string(intent.Side), "posSide": intent.PositionSide, "mrgPosition": "merge", "ordType": string(intent.Type), "sz": strconv.FormatFloat(intent.Quantity, 'f', -1, 64), "reduceOnly": intent.ReduceOnly}
	if intent.Type == execution.OrderTypeLimit {
		payload["px"] = intent.LimitPrice
	}
	body, _ := json.Marshal(payload)
	raw, err := a.request(ctx, http.MethodPost, "/deepcoin/trade/order", nil, body)
	if err != nil {
		return execution.ExecutionReport{}, err
	}
	var result response[struct{ OrdID, SCode, SMsg string }]
	if err := decode(raw, &result); err != nil {
		return execution.ExecutionReport{}, err
	}
	if result.Data.SCode != "0" {
		return execution.ExecutionReport{}, fmt.Errorf("deepcoin place order %s: %s", result.Data.SCode, result.Data.SMsg)
	}
	return execution.ExecutionReport{IntentID: intent.IntentID, ExchangeOrderID: result.Data.OrdID, Status: execution.ExecutionStatusAccepted, UpdatedAt: a.now().UnixMilli()}, nil
}
func (a *Adapter) CancelOrder(ctx context.Context, symbol, intentID string) error {
	if err := executionadapter.EnsureTradingEnabled(a.account); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{"instId": symbol, "clOrdId": clientID(intentID)})
	raw, err := a.request(ctx, http.MethodPost, "/deepcoin/trade/cancel-order", nil, body)
	if err != nil {
		return err
	}
	var result response[struct{ SCode, SMsg string }]
	if err := decode(raw, &result); err != nil {
		return err
	}
	if result.Data.SCode != "0" {
		return fmt.Errorf("deepcoin cancel order %s: %s", result.Data.SCode, result.Data.SMsg)
	}
	return nil
}
func (a *Adapter) OpenOrders(ctx context.Context, symbol string) ([]execution.ExchangeOrder, error) {
	q := map[string]string{"index": "1", "limit": "100"}
	if symbol != "" {
		q["instId"] = symbol
	}
	body, err := a.get(ctx, "/deepcoin/trade/v2/orders-pending", q)
	if err != nil {
		return nil, err
	}
	var result response[[]order]
	if err := decode(body, &result); err != nil {
		return nil, err
	}
	orders := make([]execution.ExchangeOrder, 0, len(result.Data))
	for _, row := range result.Data {
		orders = append(orders, mapOrder(a.account.ID, row))
	}
	return orders, nil
}
func (a *Adapter) Capability(ctx context.Context, symbol string) (execution.SymbolCapability, error) {
	body, err := a.get(ctx, "/deepcoin/market/instruments", map[string]string{"instType": "SWAP", "instId": symbol})
	if err != nil {
		return execution.SymbolCapability{}, err
	}
	var result response[[]struct{ InstID, CtVal, Lever, TickSz, LotSz, MinSz, MaxMktSz, ListTime string }]
	if err := decode(body, &result); err != nil {
		return execution.SymbolCapability{}, err
	}
	for _, r := range result.Data {
		if r.InstID == symbol {
			updated, _ := strconv.ParseInt(r.ListTime, 10, 64)
			return execution.SymbolCapability{Exchange: "deepcoin", Market: a.account.Market, Symbol: symbol, MinQty: r.MinSz, QtyStep: r.LotSz, PriceTick: r.TickSz, MaxLeverage: r.Lever, MaxOrderQty: r.MaxMktSz, ContractSize: r.CtVal, QuantityUnit: "contract", UpdatedAt: updated}, nil
		}
	}
	return execution.SymbolCapability{}, fmt.Errorf("deepcoin symbol %s missing", symbol)
}
func (a *Adapter) Recover(ctx context.Context, intent execution.OrderIntent) (execution.ExecutionReport, bool, error) {
	body, err := a.get(ctx, "/deepcoin/trade/order", map[string]string{"instId": intent.Symbol, "clOrdId": clientID(intent.IntentID)})
	if err != nil {
		return execution.ExecutionReport{}, false, err
	}
	var result response[[]order]
	if err := decode(body, &result); err != nil {
		return execution.ExecutionReport{}, false, err
	}
	if len(result.Data) == 0 {
		return execution.ExecutionReport{}, false, nil
	}
	o := mapOrder(a.account.ID, result.Data[0])
	return execution.ExecutionReport{IntentID: intent.IntentID, ExchangeOrderID: o.OrderID, Status: o.Status, FilledQuantity: o.FilledQuantity, AveragePrice: o.AveragePrice, UpdatedAt: o.UpdatedAt}, true, nil
}
func (a *Adapter) get(ctx context.Context, path string, q map[string]string) ([]byte, error) {
	return a.request(ctx, http.MethodGet, path, q, nil)
}
func (a *Adapter) request(ctx context.Context, method, path string, q map[string]string, body []byte) ([]byte, error) {
	v := url.Values{}
	for k, x := range q {
		v.Set(k, x)
	}
	raw := v.Encode()
	requestPath := path
	if raw != "" {
		requestPath += "?" + raw
	}
	ts := a.now().UTC().Format("2006-01-02T15:04:05.000Z")
	mac := hmac.New(sha256.New, []byte(a.credential.APISecret))
	_, _ = mac.Write([]byte(ts + method + requestPath + string(body)))
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+requestPath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("DC-ACCESS-KEY", a.credential.APIKey)
	req.Header.Set("DC-ACCESS-SIGN", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	req.Header.Set("DC-ACCESS-TIMESTAMP", ts)
	req.Header.Set("DC-ACCESS-PASSPHRASE", a.credential.Passphrase)
	req.Header.Set("Content-Type", "application/json")
	out, err := a.client.DoBytes(req)
	if err != nil {
		return nil, fmt.Errorf("deepcoin %s: %w", path, err)
	}
	return out, nil
}
func decode[T any](body []byte, result *response[T]) error {
	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("decode deepcoin response: %w", err)
	}
	code := strings.Trim(string(result.Code), "\"")
	if code != "" && code != "0" {
		return fmt.Errorf("deepcoin code %s: %s", code, result.Msg)
	}
	return nil
}
func mapOrder(account string, r order) execution.ExchangeOrder {
	qty, _ := strconv.ParseFloat(r.Sz, 64)
	filled, _ := strconv.ParseFloat(r.AccFillSz, 64)
	created, _ := strconv.ParseInt(r.CTime, 10, 64)
	updated, _ := strconv.ParseInt(r.UTime, 10, 64)
	return execution.ExchangeOrder{Exchange: "deepcoin", Account: account, Symbol: r.InstID, OrderID: r.OrdID, ClientOrderID: r.ClOrdID, Side: execution.OrderSide(r.Side), PositionSide: r.PosSide, Type: execution.OrderType(r.OrdType), Status: mapStatus(r.State), Quantity: qty, FilledQuantity: filled, AveragePrice: r.AvgPx, ReduceOnly: r.ReduceOnly == "true", CreatedAt: created, UpdatedAt: updated}
}
func mapStatus(v string) execution.ExecutionStatus {
	switch v {
	case "filled":
		return execution.ExecutionStatusFilled
	case "partially_filled":
		return execution.ExecutionStatusPartial
	case "canceled":
		return execution.ExecutionStatusCanceled
	case "rejected":
		return execution.ExecutionStatusRejected
	default:
		return execution.ExecutionStatusAccepted
	}
}
func clientID(id string) string {
	return strings.ReplaceAll(executionadapter.ClientOrderID("af", id, 20), "-", "")
}
func marginMode(m executionaccount.MarginMode) string {
	if m == executionaccount.MarginModeIsolated {
		return "isolated"
	}
	return "cross"
}
func scope(e executionaccount.Environment) strategy.PositionScope {
	if e == executionaccount.EnvironmentLive {
		return strategy.PositionScopeLive
	}
	return strategy.PositionScopeTestnet
}
