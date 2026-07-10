package bitget

import (
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
	body, err := a.get(ctx, "/api/v2/mix/order/detail", map[string]string{"symbol": intent.Symbol, "productType": "USDT-FUTURES", "clientOid": intent.IntentID})
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
func (a *Adapter) Execute(context.Context, execution.OrderIntent) (execution.ExecutionReport, error) {
	return execution.ExecutionReport{}, fmt.Errorf("bitget trading is not enabled")
}
func (a *Adapter) CancelOrder(context.Context, string, string) error {
	return fmt.Errorf("bitget trading is not enabled")
}
func (a *Adapter) get(ctx context.Context, path string, query map[string]string) ([]byte, error) {
	values := url.Values{}
	for k, v := range query {
		values.Set(k, v)
	}
	rawQuery := values.Encode()
	timestamp := strconv.FormatInt(a.now().UnixMilli(), 10)
	prehash := timestamp + http.MethodGet + path
	if rawQuery != "" {
		prehash += "?" + rawQuery
	}
	mac := hmac.New(sha256.New, []byte(a.credential.APISecret))
	_, _ = mac.Write([]byte(prehash))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+path+func() string {
		if rawQuery == "" {
			return ""
		}
		return "?" + rawQuery
	}(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("ACCESS-KEY", a.credential.APIKey)
	req.Header.Set("ACCESS-SIGN", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	req.Header.Set("ACCESS-PASSPHRASE", a.credential.Passphrase)
	req.Header.Set("ACCESS-TIMESTAMP", timestamp)
	if a.account.Environment == executionaccount.EnvironmentTestnet {
		req.Header.Set("paptrading", "1")
	}
	body, err := a.client.DoBytes(req)
	if err != nil {
		return nil, fmt.Errorf("bitget %s: %w", path, err)
	}
	return body, nil
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
