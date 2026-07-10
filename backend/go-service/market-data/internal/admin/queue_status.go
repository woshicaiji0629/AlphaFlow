package admin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"alphaflow/go-service/market-data/internal/config"
	"github.com/nats-io/nats.go"
	"github.com/spf13/cobra"
)

type natsConnection struct {
	*nats.Conn
	JetStreamContext nats.JetStreamContext
}

type queueStatusTarget struct {
	Name     string
	Stream   string
	Consumer string
}

type queueStatusRow struct {
	Name            string
	Stream          string
	Consumer        string
	StreamMessages  uint64
	StreamBytes     uint64
	ConsumerPending uint64
	AckPending      int
	Redelivered     int
	Waiting         int
	Status          string
}

var queueStatusTargets = []queueStatusTarget{
	{
		Name:     "market-snapshot",
		Stream:   "ALPHAFLOW_MARKET",
		Consumer: "strategy-engine-market",
	},
	{
		Name:     "market-indicator",
		Stream:   "ALPHAFLOW_MARKET_INDICATOR",
		Consumer: "market-data-indicator-worker",
	},
	{
		Name:     "market-pending",
		Stream:   "ALPHAFLOW_MARKET_PENDING",
		Consumer: "market-data-clickhouse-pending",
	},
	{
		Name:     "market-backfill",
		Stream:   "ALPHAFLOW_MARKET_BACKFILL",
		Consumer: "market-data-backfill-worker",
	},
	{
		Name:     "strategy-decision",
		Stream:   "ALPHAFLOW_STRATEGY",
		Consumer: "position-engine",
	},
}

func newQueueStatusCommand(ctx context.Context, root *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "queue-status",
		Short: "Print NATS JetStream queue lag for AlphaFlow queues",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQueueStatus(ctx, root.configPath)
		},
	}
}

func runQueueStatus(ctx context.Context, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	conn, err := connectNATS(cfg.NATS.URL)
	if err != nil {
		return err
	}
	defer conn.Close()
	rows, err := buildQueueStatusRows(ctx, conn.JetStreamContext, queueStatusTargets)
	if err != nil {
		return err
	}
	return printQueueStatusRows(rows)
}

func connectNATS(url string) (*natsConnection, error) {
	conn, err := nats.Connect(url)
	if err != nil {
		return nil, fmt.Errorf("connect nats: %w", err)
	}
	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("create nats jetstream context: %w", err)
	}
	return &natsConnection{Conn: conn, JetStreamContext: js}, nil
}

func buildQueueStatusRows(
	ctx context.Context,
	js nats.JetStreamContext,
	targets []queueStatusTarget,
) ([]queueStatusRow, error) {
	rows := make([]queueStatusRow, 0, len(targets))
	for _, target := range targets {
		row := queueStatusRow{
			Name:     target.Name,
			Stream:   target.Stream,
			Consumer: target.Consumer,
			Status:   "ok",
		}

		streamInfo, err := js.StreamInfo(target.Stream, nats.Context(ctx))
		if errors.Is(err, nats.ErrStreamNotFound) {
			row.Status = "missing_stream"
			rows = append(rows, row)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read nats stream %s: %w", target.Stream, err)
		}
		row.StreamMessages = streamInfo.State.Msgs
		row.StreamBytes = streamInfo.State.Bytes

		consumerInfo, err := js.ConsumerInfo(target.Stream, target.Consumer, nats.Context(ctx))
		if errors.Is(err, nats.ErrConsumerNotFound) {
			row.Status = "missing_consumer"
			rows = append(rows, row)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read nats consumer %s/%s: %w", target.Stream, target.Consumer, err)
		}
		row.ConsumerPending = consumerInfo.NumPending
		row.AckPending = consumerInfo.NumAckPending
		row.Redelivered = consumerInfo.NumRedelivered
		row.Waiting = consumerInfo.NumWaiting
		rows = append(rows, row)
	}
	return rows, nil
}

func printQueueStatusRows(rows []queueStatusRow) error {
	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "QUEUE\tSTREAM\tCONSUMER\tSTREAM_MSGS\tBYTES\tCONSUMER_PENDING\tACK_PENDING\tREDELIVERED\tWAITING\tSTATUS")
	for _, row := range rows {
		fmt.Fprintf(
			writer,
			"%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%s\n",
			row.Name,
			row.Stream,
			row.Consumer,
			row.StreamMessages,
			row.StreamBytes,
			row.ConsumerPending,
			row.AckPending,
			row.Redelivered,
			row.Waiting,
			row.Status,
		)
	}
	return writer.Flush()
}
