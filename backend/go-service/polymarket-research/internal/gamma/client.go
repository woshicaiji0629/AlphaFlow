package gamma

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"alphaflow/go-service/pkg/httpclient"
	"alphaflow/go-service/polymarket-research/internal/model"
)

type HTTPClient interface {
	Get(context.Context, string, map[string]string) ([]byte, error)
}

type Client struct {
	baseURL  string
	client   HTTPClient
	pageSize int
	now      func() time.Time
}

type Options struct {
	BaseURL    string
	PageSize   int
	HTTPClient HTTPClient
	Now        func() time.Time
}

type gammaEvent struct {
	ID            string        `json:"id"`
	Markets       []gammaMarket `json:"markets"`
	EventMetadata struct {
		PriceToBeat json.Number `json:"priceToBeat"`
		FinalPrice  json.Number `json:"finalPrice"`
	} `json:"eventMetadata"`
}
type gammaMarket struct {
	ID               string       `json:"id"`
	Question         string       `json:"question"`
	ConditionID      string       `json:"conditionId"`
	Slug             string       `json:"slug"`
	ResolutionSource string       `json:"resolutionSource"`
	Description      string       `json:"description"`
	StartDate        string       `json:"startDate"`
	EventStartTime   string       `json:"eventStartTime"`
	EndDate          string       `json:"endDate"`
	Outcomes         string       `json:"outcomes"`
	OutcomePrices    string       `json:"outcomePrices"`
	ClobTokenIDs     string       `json:"clobTokenIds"`
	Active           bool         `json:"active"`
	Closed           bool         `json:"closed"`
	EnableOrderBook  bool         `json:"enableOrderBook"`
	AcceptingOrders  bool         `json:"acceptingOrders"`
	UpdatedAt        string       `json:"updatedAt"`
	Events           []gammaEvent `json:"events"`
	PriceToBeat      string       `json:"-"`
	FinalPrice       string       `json:"-"`
}

func New(options Options) *Client {
	if options.HTTPClient == nil {
		options.HTTPClient = httpclient.New()
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Client{strings.TrimRight(options.BaseURL, "/"), options.HTTPClient, options.PageSize, options.Now}
}

func (c *Client) Discover(ctx context.Context, symbols, durations []string) ([]model.Market, error) {
	allowed := make(map[string]struct{}, len(symbols))
	for _, symbol := range symbols {
		allowed[strings.ToUpper(symbol)] = struct{}{}
	}
	allowedDurations := make(map[string]struct{}, len(durations))
	for _, duration := range durations {
		allowedDurations[strings.ToLower(duration)] = struct{}{}
	}
	result := map[string]model.Market{}
	now := c.now().UTC()
	queries := []map[string]string{
		{"active": "true", "closed": "false", "end_date_min": now.Add(-2 * time.Hour).Format(time.RFC3339), "end_date_max": now.Add(2 * time.Hour).Format(time.RFC3339), "order": "endDate", "ascending": "true"},
		{"closed": "true", "end_date_min": now.Add(-2 * time.Hour).Format(time.RFC3339), "end_date_max": now.Format(time.RFC3339), "order": "endDate", "ascending": "false"},
	}
	for _, query := range queries {
		if err := c.discoverPages(ctx, allowed, allowedDurations, query, result); err != nil {
			return nil, err
		}
	}
	markets := make([]model.Market, 0, len(result))
	for _, market := range result {
		markets = append(markets, market)
	}
	return markets, nil
}

func (c *Client) discoverPages(ctx context.Context, allowed, allowedDurations map[string]struct{}, query map[string]string, result map[string]model.Market) error {
	for offset := 0; offset < c.pageSize*20; offset += c.pageSize {
		params := map[string]string{"limit": strconv.Itoa(c.pageSize), "offset": strconv.Itoa(offset)}
		for key, value := range query {
			params[key] = value
		}
		params["tag_slug"] = "crypto"
		body, err := c.client.Get(ctx, c.baseURL+"/events", params)
		if err != nil {
			return fmt.Errorf("list gamma markets at offset %d: %w", offset, err)
		}
		var page []gammaEvent
		if err := json.Unmarshal(body, &page); err != nil {
			return fmt.Errorf("decode gamma events: %w", err)
		}
		for _, event := range page {
			for _, raw := range event.Markets {
				raw.PriceToBeat = event.EventMetadata.PriceToBeat.String()
				raw.FinalPrice = event.EventMetadata.FinalPrice.String()
				if len(raw.Events) == 0 {
					raw.Events = []gammaEvent{{ID: event.ID}}
				}
				market, ok, err := parseMarket(raw, allowed, allowedDurations, c.now().UnixMilli())
				if err != nil {
					return fmt.Errorf("parse gamma market %s: %w", raw.ID, err)
				}
				if ok {
					result[market.MarketID] = market
				}
			}
		}
		if len(page) < c.pageSize {
			break
		}
	}
	return nil
}

func parseMarket(raw gammaMarket, allowed, allowedDurations map[string]struct{}, nowMS int64) (model.Market, bool, error) {
	if (!raw.Active && !raw.Closed) || (!raw.Closed && !raw.EnableOrderBook) {
		return model.Market{}, false, nil
	}
	symbol, ok := symbolFromSlug(raw.Slug)
	if !ok {
		return model.Market{}, false, nil
	}
	if _, ok := allowed[symbol]; !ok {
		return model.Market{}, false, nil
	}
	duration, ok := durationFromSlug(raw.Slug)
	if !ok {
		return model.Market{}, false, nil
	}
	if _, ok := allowedDurations[duration]; !ok {
		return model.Market{}, false, nil
	}
	startValue := raw.EventStartTime
	if startValue == "" {
		startValue = raw.StartDate
	}
	start, err := time.Parse(time.RFC3339, startValue)
	if err != nil {
		return model.Market{}, false, fmt.Errorf("parse start date: %w", err)
	}
	end, err := time.Parse(time.RFC3339, raw.EndDate)
	if err != nil {
		return model.Market{}, false, fmt.Errorf("parse end date: %w", err)
	}
	if end.Sub(start) != durationValue(duration) {
		return model.Market{}, false, nil
	}
	var outcomes, tokenIDs []string
	if err := json.Unmarshal([]byte(raw.Outcomes), &outcomes); err != nil {
		return model.Market{}, false, fmt.Errorf("decode outcomes: %w", err)
	}
	if err := json.Unmarshal([]byte(raw.ClobTokenIDs), &tokenIDs); err != nil {
		return model.Market{}, false, fmt.Errorf("decode clob token ids: %w", err)
	}
	if len(outcomes) != 2 || len(tokenIDs) != 2 {
		return model.Market{}, false, nil
	}
	yesIndex, noIndex := outcomeIndexes(outcomes)
	if yesIndex < 0 || noIndex < 0 {
		return model.Market{}, false, nil
	}
	eventID := ""
	if len(raw.Events) > 0 {
		eventID = raw.Events[0].ID
	}
	resolved := resolvedOutcome(raw.OutcomePrices, outcomes)
	updatedAt := nowMS
	if parsed, err := time.Parse(time.RFC3339, raw.UpdatedAt); err == nil {
		updatedAt = parsed.UnixMilli()
	}
	return model.Market{
		MarketID: raw.ID, ConditionID: raw.ConditionID, EventID: eventID, Slug: raw.Slug, Title: raw.Question,
		Symbol: symbol, Duration: duration, StartTimeMS: start.UnixMilli(), EndTimeMS: end.UnixMilli(),
		YesTokenID: tokenIDs[yesIndex], NoTokenID: tokenIDs[noIndex], ResolutionSource: firstNonEmpty(raw.ResolutionSource, raw.Description),
		Active: raw.Active, Closed: raw.Closed, AcceptingOrders: raw.AcceptingOrders, ResolvedOutcome: resolved, UpdatedAtMS: updatedAt,
		PriceToBeat: raw.PriceToBeat, FinalPrice: raw.FinalPrice,
	}, true, nil
}

func durationFromSlug(slug string) (string, bool) {
	slug = strings.ToLower(slug)
	for _, duration := range []string{"5m", "15m"} {
		if strings.Contains(slug, "-updown-"+duration+"-") {
			return duration, true
		}
	}
	return "", false
}

func durationValue(duration string) time.Duration {
	if duration == "5m" {
		return 5 * time.Minute
	}
	return 15 * time.Minute
}

func symbolFromSlug(slug string) (string, bool) {
	prefix, _, ok := strings.Cut(strings.ToLower(slug), "-")
	if !ok {
		return "", false
	}
	symbols := map[string]string{
		"btc": "BTC", "bitcoin": "BTC",
		"eth": "ETH", "ethereum": "ETH",
		"sol": "SOL", "solana": "SOL",
		"xrp": "XRP", "ripple": "XRP",
		"doge": "DOGE", "dogecoin": "DOGE",
		"bnb":  "BNB",
		"hype": "HYPE", "hyperliquid": "HYPE",
	}
	symbol, ok := symbols[prefix]
	return symbol, ok
}
func outcomeIndexes(outcomes []string) (int, int) {
	yes, no := -1, -1
	for index, outcome := range outcomes {
		switch strings.ToLower(strings.TrimSpace(outcome)) {
		case "yes", "up":
			yes = index
		case "no", "down":
			no = index
		}
	}
	return yes, no
}
func resolvedOutcome(raw string, outcomes []string) string {
	var prices []string
	if json.Unmarshal([]byte(raw), &prices) != nil || len(prices) != len(outcomes) {
		return ""
	}
	for index, price := range prices {
		if price == "1" || price == "1.0" {
			return strings.ToLower(outcomes[index])
		}
	}
	return ""
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
