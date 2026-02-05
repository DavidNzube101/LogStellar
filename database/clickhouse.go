package database

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type Client struct {
	conn driver.Conn
}

func NewClient(addr string) (*Client, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{addr},
		Auth: clickhouse.Auth{
			Database: "default",
			Username: "default",
			Password: "",
		},
		ClientInfo: clickhouse.ClientInfo{
			Products: []struct {
				Name    string
				Version string
			}{
				{Name: "logstellar-indexer", Version: "1.0.0"},
			},
		},
		Debug: false,
	})

	if err != nil {
		return nil, err
	}

	if err := conn.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to ping clickhouse: %w", err)
	}

	client := &Client{conn: conn}
	if err := client.initSchema(); err != nil {
		return nil, err
	}

	return client, nil
}

func (c *Client) Close() {
	c.conn.Close()
}

func (c *Client) initSchema() error {
	ctx := context.Background()

	err := c.conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS solana_logs (
			timestamp DateTime64(3),
			slot UInt64,
			signature String,
			log_message String,
			program_id String
		) ENGINE = MergeTree()
		ORDER BY (timestamp, slot)
	`)
	if err != nil {
		return fmt.Errorf("failed to create logs table: %w", err)
	}

	err = c.conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS pattern_alerts (
			timestamp DateTime64(3),
			pattern_name String,
			confidence Float32,
			signature String,
			raw_match String
		) ENGINE = MergeTree()
		ORDER BY (timestamp, pattern_name)
	`)
	if err != nil {
		return fmt.Errorf("failed to create alerts table: %w", err)
	}

	return nil
}

func (c *Client) BatchInsertLogs(logs []LogEntry) error {
	ctx := context.Background()
	batch, err := c.conn.PrepareBatch(ctx, "INSERT INTO solana_logs")
	if err != nil {
		return err
	}

	for _, l := range logs {
		err := batch.Append(
			l.Timestamp,
			l.Slot,
			l.Signature,
			l.LogMessage,
			l.ProgramID,
		)
		if err != nil {
			return err
		}
	}

	return batch.Send()
}

func (c *Client) BatchInsertAlerts(alerts []AlertEntry) error {
	ctx := context.Background()
	batch, err := c.conn.PrepareBatch(ctx, "INSERT INTO pattern_alerts")
	if err != nil {
		return err
	}

	for _, a := range alerts {
		err := batch.Append(
			a.Timestamp,
			a.PatternName,
			a.Confidence,
			a.Signature,
			a.RawMatch,
		)
		if err != nil {
			return err
		}
	}

	return batch.Send()
}

type LogEntry struct {
	Timestamp  time.Time
	Slot       uint64
	Signature  string
	LogMessage string
	ProgramID  string
}

type AlertEntry struct {
	Timestamp   time.Time
	PatternName string
	Confidence  float32
	Signature   string
	RawMatch    string
}