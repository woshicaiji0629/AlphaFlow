package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"alphaflow/go-service/pkg/logger"
	"alphaflow/go-service/polymarket-research/internal/config"
	"alphaflow/go-service/polymarket-research/internal/research"
)

func main() {
	setupLogger()

	path := flag.String("config", "", "config path")
	start := flag.String("start", "", "start YYYYMMDDHHMM")
	end := flag.String("end", "", "end YYYYMMDDHHMM")
	entrySeconds := flag.Int64("entry-seconds", 60, "select the latest quote at least this many seconds before expiry")
	flag.Parse()
	cfg, err := config.Load(*path)
	if err != nil {
		fail("load Polymarket report config failed", err)
	}
	parse := func(v string) (int64, error) {
		t, e := time.ParseInLocation("200601021504", v, time.Local)
		return t.UnixMilli(), e
	}
	startMS, err := parse(*start)
	if err != nil {
		fail("parse report start time failed", err)
	}
	endMS, err := parse(*end)
	if err != nil {
		fail("parse report end time failed", err)
	}
	if startMS >= endMS {
		slog.Error("start must be before end", "start", *start, "end", *end)
		os.Exit(1)
	}
	values, err := research.Query(context.Background(), research.QueryOptions{Addr: cfg.ClickHouse.Addr, Database: cfg.ClickHouse.Database, Username: cfg.ClickHouse.Username, Password: cfg.ClickHouse.Password, StartMS: startMS, EndMS: endMS, EntrySecondsToExpiry: *entrySeconds})
	if err != nil {
		fail("query Polymarket report failed", err)
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

func setupLogger() {
	if err := logger.Setup(logger.Config{
		Service: "polymarket-research-report", Level: "info", Format: "text", Output: "stderr",
	}); err != nil {
		fmt.Fprintf(os.Stderr, "setup logger: %v\n", err)
		os.Exit(1)
	}
}

func fail(message string, err error) {
	slog.Error(message, "error", err)
	os.Exit(1)
}
