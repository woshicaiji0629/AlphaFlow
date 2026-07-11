package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"alphaflow/go-service/polymarket-research/internal/config"
	"alphaflow/go-service/polymarket-research/internal/research"
)

func main() {
	path := flag.String("config", "", "config path")
	start := flag.String("start", "", "start YYYYMMDDHHMM")
	end := flag.String("end", "", "end YYYYMMDDHHMM")
	entrySeconds := flag.Int64("entry-seconds", 60, "select the latest quote at least this many seconds before expiry")
	flag.Parse()
	cfg, err := config.Load(*path)
	if err != nil {
		log.Fatal(err)
	}
	parse := func(v string) (int64, error) {
		t, e := time.ParseInLocation("200601021504", v, time.Local)
		return t.UnixMilli(), e
	}
	startMS, err := parse(*start)
	if err != nil {
		log.Fatal(err)
	}
	endMS, err := parse(*end)
	if err != nil {
		log.Fatal(err)
	}
	if startMS >= endMS {
		log.Fatal("start must be before end")
	}
	values, err := research.Query(context.Background(), research.QueryOptions{Addr: cfg.ClickHouse.Addr, Database: cfg.ClickHouse.Database, Username: cfg.ClickHouse.Username, Password: cfg.ClickHouse.Password, StartMS: startMS, EndMS: endMS, EntrySecondsToExpiry: *entrySeconds})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("symbol duration outcome seconds_to_expiry samples wins win_rate avg_entry avg_spread gross_pnl")
	for _, v := range research.Summarize(values) {
		rate := 0.0
		if v.Samples > 0 {
			rate = float64(v.Wins) / float64(v.Samples)
		}
		fmt.Printf("%s %s %s %d %d %d %.4f %.4f %.4f %.4f\n", v.Symbol, v.Duration, v.Outcome, v.SecondsToExpiry, v.Samples, v.Wins, rate, v.AverageEntry, v.AverageSpread, v.PnL)
	}
}
