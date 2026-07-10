package gate

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
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
	liveURL    = "https://api.gateio.ws/api/v4"
	testnetURL = "https://fx-api-testnet.gateio.ws/api/v4"
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
	Settle     string
}
type Adapter struct {
	account    executionaccount.Account
	credential executionaccount.Credential
	baseURL    string
	client     HTTPClient
	now        func() time.Time
	settle     string
}

func New(o Options) (*Adapter, error) {
	if err := o.Account.Validate(); err != nil {
		return nil, err
	}
	if err := o.Credential.Validate("gate"); err != nil {
		return nil, err
	}
	if o.BaseURL == "" {
		o.BaseURL = liveURL
		if o.Account.Environment == executionaccount.EnvironmentTestnet {
			o.BaseURL = testnetURL
		}
	}
	if o.HTTPClient == nil {
		o.HTTPClient = httpclient.New()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Settle == "" {
		o.Settle = "usdt"
	}
	return &Adapter{account: o.Account, credential: o.Credential, baseURL: strings.TrimRight(o.BaseURL, "/"), client: o.HTTPClient, now: o.Now, settle: strings.ToLower(o.Settle)}, nil
}
func Register(r *executionadapter.Registry) error {
	return r.Register("gate", func(a executionaccount.Account, c executionaccount.Credential) (executionadapter.Adapter, error) {
		return New(Options{Account: a, Credential: c})
	})
}
func (a *Adapter) TestConnection(ctx context.Context) error { _, err := a.Account(ctx); return err }
func (a *Adapter) Account(ctx context.Context) (execution.AccountSnapshot, error) {
	body, err := a.get(ctx, "/futures/"+a.settle+"/accounts", nil)
	if err != nil {
		return execution.AccountSnapshot{}, err
	}
	var row struct {
		Total         string `json:"total"`
		Available     string `json:"available"`
		UnrealisedPnl string `json:"unrealised_pnl"`
	}
	if err := json.Unmarshal(body, &row); err != nil {
		return execution.AccountSnapshot{}, fmt.Errorf("decode gate account: %w", err)
	}
	return execution.AccountSnapshot{Scope: string(scope(a.account.Environment)), Account: a.account.ID, Exchange: "gate", Market: a.account.Market, Equity: row.Total, AvailableBalance: row.Available, UnrealizedPnL: row.UnrealisedPnl, UpdatedAt: a.now().UnixMilli()}, nil
}
func (a *Adapter) Positions(ctx context.Context) ([]strategy.Position, error) {
	body, err := a.get(ctx, "/futures/"+a.settle+"/positions", nil)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Contract   string `json:"contract"`
		Size       string `json:"size"`
		EntryPrice string `json:"entry_price"`
		Mode       string `json:"mode"`
		UpdateTime int64  `json:"update_time"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("decode gate positions: %w", err)
	}
	result := []strategy.Position{}
	for _, row := range rows {
		size, err := strconv.ParseFloat(row.Size, 64)
		if err != nil || size == 0 {
			continue
		}
		side := strategy.PositionSideLong
		if size < 0 {
			side = strategy.PositionSideShort
			size = -size
		}
		positionSide := strategy.ExchangePositionSideNet
		if row.Mode == "dual_long" {
			positionSide = strategy.ExchangePositionSideLong
		} else if row.Mode == "dual_short" {
			positionSide = strategy.ExchangePositionSideShort
		}
		result = append(result, strategy.Position{Scope: scope(a.account.Environment), Account: a.account.ID, Exchange: "gate", Market: a.account.Market, Symbol: row.Contract, Mode: strategy.ExchangePositionModeNet, PositionSide: positionSide, Side: side, Size: size, EntryPrice: row.EntryPrice, UpdatedAt: row.UpdateTime * 1000})
	}
	return result, nil
}
func (a *Adapter) Execute(ctx context.Context, intent execution.OrderIntent) (execution.ExecutionReport, error) {
	if err := executionadapter.EnsureTradingEnabled(a.account); err != nil {
		return execution.ExecutionReport{}, err
	}
	if a.account.PositionMode == executionaccount.PositionModeHedge {
		return execution.ExecutionReport{}, fmt.Errorf("gate hedge mode trading is not enabled")
	}
	if intent.Quantity <= 0 {
		return execution.ExecutionReport{}, fmt.Errorf("quantity must be positive")
	}
	size := intent.Quantity
	if intent.Side == execution.OrderSideSell {
		size = -size
	}
	payload := map[string]any{"contract": intent.Symbol, "size": strconv.FormatFloat(size, 'f', -1, 64), "price": "0", "tif": "ioc", "text": executionadapter.ClientOrderID("t-af-", intent.IntentID, 31), "reduce_only": intent.ReduceOnly}
	body, _ := json.Marshal(payload)
	responseBody, err := a.request(ctx, http.MethodPost, "/futures/"+a.settle+"/orders", nil, body)
	if err != nil {
		return execution.ExecutionReport{}, err
	}
	return decodeGateReport(responseBody, intent.IntentID)
}
func (a *Adapter) CancelOrder(ctx context.Context, symbol string, intentID string) error {
	if err := executionadapter.EnsureTradingEnabled(a.account); err != nil {
		return err
	}
	order, found, err := a.findOrder(ctx, symbol, intentID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("gate order for intent %s missing", intentID)
	}
	_, err = a.request(ctx, http.MethodDelete, "/futures/"+a.settle+"/orders/"+order.OrderID, nil, nil)
	return err
}
func (a *Adapter) OpenOrders(ctx context.Context, symbol string) ([]execution.ExchangeOrder, error) {
	query := map[string]string{"status": "open"}
	if symbol != "" {
		query["contract"] = symbol
	}
	body, err := a.get(ctx, "/futures/"+a.settle+"/orders", query)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID           int64   `json:"id"`
		Text         string  `json:"text"`
		Contract     string  `json:"contract"`
		Size         string  `json:"size"`
		Left         string  `json:"left"`
		Price        string  `json:"price"`
		FillPrice    string  `json:"fill_price"`
		Status       string  `json:"status"`
		Tif          string  `json:"tif"`
		IsClose      bool    `json:"is_close"`
		IsReduceOnly bool    `json:"is_reduce_only"`
		CreateTime   float64 `json:"create_time"`
		FinishTime   float64 `json:"finish_time"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("decode gate open orders: %w", err)
	}
	orders := make([]execution.ExchangeOrder, 0, len(rows))
	for _, row := range rows {
		size, _ := strconv.ParseFloat(row.Size, 64)
		left, _ := strconv.ParseFloat(row.Left, 64)
		side := execution.OrderSideBuy
		positionSide := "long"
		if size < 0 {
			side = execution.OrderSideSell
			positionSide = "short"
			size = -size
		}
		if left < 0 {
			left = -left
		}
		orderType := execution.OrderTypeLimit
		if row.Price == "0" && row.Tif == "ioc" {
			orderType = execution.OrderTypeMarket
		}
		orders = append(orders, execution.ExchangeOrder{Exchange: "gate", Account: a.account.ID, Symbol: row.Contract, OrderID: strconv.FormatInt(row.ID, 10), ClientOrderID: row.Text, Side: side, PositionSide: positionSide, Type: orderType, Status: execution.ExecutionStatusAccepted, Quantity: size, FilledQuantity: size - left, AveragePrice: row.FillPrice, ReduceOnly: row.IsReduceOnly || row.IsClose, CreatedAt: int64(row.CreateTime * 1000), UpdatedAt: int64(row.FinishTime * 1000)})
	}
	return orders, nil
}
func (a *Adapter) Capability(ctx context.Context, symbol string) (execution.SymbolCapability, error) {
	body, err := a.get(ctx, "/futures/"+a.settle+"/contracts/"+url.PathEscape(symbol), nil)
	if err != nil {
		return execution.SymbolCapability{}, err
	}
	var row struct {
		Name               string  `json:"name"`
		QuantoMultiplier   string  `json:"quanto_multiplier"`
		OrderPriceRound    string  `json:"order_price_round"`
		OrderSizeMin       string  `json:"order_size_min"`
		OrderSizeMax       string  `json:"order_size_max"`
		LeverageMax        string  `json:"leverage_max"`
		MarketOrderSizeMax string  `json:"market_order_size_max"`
		ConfigChangeTime   float64 `json:"config_change_time"`
	}
	if err := json.Unmarshal(body, &row); err != nil {
		return execution.SymbolCapability{}, fmt.Errorf("decode gate contract: %w", err)
	}
	maxOrder := row.MarketOrderSizeMax
	if maxOrder == "" || maxOrder == "0" {
		maxOrder = row.OrderSizeMax
	}
	return execution.SymbolCapability{Exchange: "gate", Market: a.account.Market, Symbol: row.Name, MinQty: row.OrderSizeMin, QtyStep: "1", PriceTick: row.OrderPriceRound, MaxLeverage: row.LeverageMax, MaxOrderQty: maxOrder, ContractSize: row.QuantoMultiplier, UpdatedAt: int64(row.ConfigChangeTime * 1000)}, nil
}
func (a *Adapter) Recover(ctx context.Context, intent execution.OrderIntent) (execution.ExecutionReport, bool, error) {
	order, found, err := a.findOrder(ctx, intent.Symbol, intent.IntentID)
	if err != nil || !found {
		return execution.ExecutionReport{}, found, err
	}
	return execution.ExecutionReport{IntentID: intent.IntentID, ExchangeOrderID: order.OrderID, Status: order.Status, FilledQuantity: order.FilledQuantity, AveragePrice: order.AveragePrice, UpdatedAt: order.UpdatedAt}, true, nil
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
	timestamp := strconv.FormatInt(a.now().Unix(), 10)
	bodyHash := sha512.Sum512(requestBody)
	signing := method + "\n/api/v4" + path + "\n" + rawQuery + "\n" + hex.EncodeToString(bodyHash[:]) + "\n" + timestamp
	mac := hmac.New(sha512.New, []byte(a.credential.APISecret))
	_, _ = mac.Write([]byte(signing))
	endpoint := a.baseURL + path
	if rawQuery != "" {
		endpoint += "?" + rawQuery
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("KEY", a.credential.APIKey)
	req.Header.Set("SIGN", hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("Timestamp", timestamp)
	if len(requestBody) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	body, err := a.client.DoBytes(req)
	if err != nil {
		return nil, fmt.Errorf("gate %s: %w", path, err)
	}
	return body, nil
}

func (a *Adapter) findOrder(ctx context.Context, symbol, intentID string) (execution.ExchangeOrder, bool, error) {
	clientID := executionadapter.ClientOrderID("t-af-", intentID, 31)
	for _, status := range []string{"open", "finished"} {
		body, err := a.get(ctx, "/futures/"+a.settle+"/orders", map[string]string{"status": status, "contract": symbol})
		if err != nil {
			return execution.ExchangeOrder{}, false, err
		}
		var rows []struct {
			ID         int64   `json:"id"`
			Text       string  `json:"text"`
			Contract   string  `json:"contract"`
			Size       string  `json:"size"`
			Left       string  `json:"left"`
			FillPrice  string  `json:"fill_price"`
			Status     string  `json:"status"`
			FinishAs   string  `json:"finish_as"`
			FinishTime float64 `json:"finish_time"`
		}
		if err := json.Unmarshal(body, &rows); err != nil {
			return execution.ExchangeOrder{}, false, err
		}
		for _, row := range rows {
			if row.Text != clientID {
				continue
			}
			size, _ := strconv.ParseFloat(row.Size, 64)
			left, _ := strconv.ParseFloat(row.Left, 64)
			if size < 0 {
				size = -size
			}
			if left < 0 {
				left = -left
			}
			return execution.ExchangeOrder{OrderID: strconv.FormatInt(row.ID, 10), ClientOrderID: row.Text, Symbol: row.Contract, Status: gateStatus(row.Status, row.FinishAs), Quantity: size, FilledQuantity: size - left, AveragePrice: row.FillPrice, UpdatedAt: int64(row.FinishTime * 1000)}, true, nil
		}
	}
	return execution.ExchangeOrder{}, false, nil
}
func gateStatus(status, finishAs string) execution.ExecutionStatus {
	if status == "open" {
		return execution.ExecutionStatusAccepted
	}
	switch finishAs {
	case "filled", "succeeded":
		return execution.ExecutionStatusFilled
	case "cancelled", "canceled", "expired":
		return execution.ExecutionStatusCanceled
	case "failed":
		return execution.ExecutionStatusRejected
	default:
		return execution.ExecutionStatusAccepted
	}
}
func decodeGateReport(body []byte, intentID string) (execution.ExecutionReport, error) {
	var row struct {
		ID         int64   `json:"id"`
		Status     string  `json:"status"`
		FinishAs   string  `json:"finish_as"`
		Size       string  `json:"size"`
		Left       string  `json:"left"`
		FillPrice  string  `json:"fill_price"`
		FinishTime float64 `json:"finish_time"`
	}
	if err := json.Unmarshal(body, &row); err != nil {
		return execution.ExecutionReport{}, err
	}
	size, _ := strconv.ParseFloat(row.Size, 64)
	left, _ := strconv.ParseFloat(row.Left, 64)
	if size < 0 {
		size = -size
	}
	if left < 0 {
		left = -left
	}
	return execution.ExecutionReport{IntentID: intentID, ExchangeOrderID: strconv.FormatInt(row.ID, 10), Status: gateStatus(row.Status, row.FinishAs), FilledQuantity: size - left, AveragePrice: row.FillPrice, UpdatedAt: int64(row.FinishTime * 1000)}, nil
}
func scope(e executionaccount.Environment) strategy.PositionScope {
	if e == executionaccount.EnvironmentLive {
		return strategy.PositionScopeLive
	}
	return strategy.PositionScopeTestnet
}
