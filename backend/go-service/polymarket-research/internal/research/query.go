package research

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	_ "github.com/ClickHouse/clickhouse-go/v2"
)

type QueryOptions struct {
	Addr, Database, Username, Password string
	StartMS, EndMS                     int64
	EntrySecondsToExpiry               int64
}

func Query(ctx context.Context, o QueryOptions) ([]Observation, error) {
	u := url.URL{Scheme: "clickhouse", Host: o.Addr, Path: "/" + o.Database}
	if o.Username != "" {
		u.User = url.UserPassword(o.Username, o.Password)
	}
	db, err := sql.Open("clickhouse", u.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if o.EntrySecondsToExpiry < 0 {
		return nil, fmt.Errorf("entry seconds to expiry must not be negative")
	}
	rows, err := db.QueryContext(ctx, `SELECT m.symbol,m.duration,b.token_id,b.outcome,b.best_ask,b.spread,r.winning_token_id,?
		FROM (SELECT * FROM polymarket_markets FINAL) AS m
		INNER JOIN polymarket_book_ticks AS b ON b.market_id=m.market_id
		INNER JOIN (SELECT * FROM polymarket_resolutions FINAL) AS r ON r.market_id=m.market_id
		WHERE m.start_time_ms>=? AND m.start_time_ms<? AND b.event_time_ms<=m.end_time_ms-?*1000
		QUALIFY row_number() OVER (PARTITION BY b.market_id,b.token_id ORDER BY b.event_time_ms DESC,b.received_at_ms DESC)=1`, o.EntrySecondsToExpiry, o.StartMS, o.EndMS, o.EntrySecondsToExpiry)
	if err != nil {
		return nil, fmt.Errorf("query research observations: %w", err)
	}
	defer rows.Close()
	var out []Observation
	for rows.Next() {
		var v Observation
		var ask, spread, winningToken string
		if err := rows.Scan(&v.Symbol, &v.Duration, &v.TokenID, &v.Outcome, &ask, &spread, &winningToken, &v.SecondsToExpiry); err != nil {
			return nil, err
		}
		v.EntryPrice, err = strconv.ParseFloat(strings.TrimSpace(ask), 64)
		if err != nil {
			return nil, fmt.Errorf("parse best ask %q for token %s: %w", ask, v.TokenID, err)
		}
		v.Spread, err = strconv.ParseFloat(strings.TrimSpace(spread), 64)
		if err != nil {
			return nil, fmt.Errorf("parse spread %q for token %s: %w", spread, v.TokenID, err)
		}
		if v.EntryPrice < 0 || v.EntryPrice > 1 || v.Spread < 0 {
			return nil, fmt.Errorf("invalid quote for token %s: ask=%f spread=%f", v.TokenID, v.EntryPrice, v.Spread)
		}
		v.Won = v.TokenID == winningToken
		out = append(out, v)
	}
	return out, rows.Err()
}
