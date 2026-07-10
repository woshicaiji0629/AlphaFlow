package gate

import (
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
	var row struct{ Total, Available, UnrealisedPnl string }
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
		Contract, Size, EntryPrice, Mode string
		UpdateTime                       int64
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
func (a *Adapter) Execute(context.Context, execution.OrderIntent) (execution.ExecutionReport, error) {
	return execution.ExecutionReport{}, fmt.Errorf("gate trading is not enabled")
}
func (a *Adapter) CancelOrder(context.Context, string, string) error {
	return fmt.Errorf("gate trading is not enabled")
}
func (a *Adapter) Recover(context.Context, execution.OrderIntent) (execution.ExecutionReport, bool, error) {
	return execution.ExecutionReport{}, false, nil
}
func (a *Adapter) get(ctx context.Context, path string, query map[string]string) ([]byte, error) {
	values := url.Values{}
	for k, v := range query {
		values.Set(k, v)
	}
	rawQuery := values.Encode()
	timestamp := strconv.FormatInt(a.now().Unix(), 10)
	emptyHash := sha512.Sum512(nil)
	signing := http.MethodGet + "\n/api/v4" + path + "\n" + rawQuery + "\n" + hex.EncodeToString(emptyHash[:]) + "\n" + timestamp
	mac := hmac.New(sha512.New, []byte(a.credential.APISecret))
	_, _ = mac.Write([]byte(signing))
	endpoint := a.baseURL + path
	if rawQuery != "" {
		endpoint += "?" + rawQuery
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("KEY", a.credential.APIKey)
	req.Header.Set("SIGN", hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("Timestamp", timestamp)
	body, err := a.client.DoBytes(req)
	if err != nil {
		return nil, fmt.Errorf("gate %s: %w", path, err)
	}
	return body, nil
}
func scope(e executionaccount.Environment) strategy.PositionScope {
	if e == executionaccount.EnvironmentLive {
		return strategy.PositionScopeLive
	}
	return strategy.PositionScopeTestnet
}
