package database

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type Client struct {
	conn driver.Conn
}

func NewClient(addr string) (*Client, error) {
	password := os.Getenv("CLICKHOUSE_PASSWORD")
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{addr},
		Auth: clickhouse.Auth{
			Database: "default",
			Username: "default",
			Password: password,
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
		return nil, fmt.Errorf("FAILED TO PING CLICKHOUSE: %w", err)
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
		CREATE TABLE IF NOT EXISTS solana_logs_v2 (
			timestamp DateTime64(3),
			slot UInt64,
			signature String,
			log_message String,
			program_id String,
			signer String,
			compute_units UInt32,
			is_error Bool
		) ENGINE = MergeTree()
		PARTITION BY toYYYYMM(timestamp)
		ORDER BY (timestamp, slot, program_id)
	`)
	if err != nil {
		return fmt.Errorf("FAILED TO CREATE LOGS TABLE: %w", err)
	}

	err = c.conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS program_stats (
			timestamp DateTime,
			program_id String,
			total_calls UInt64,
			total_cu UInt64,
			error_count UInt64
		) ENGINE = SummingMergeTree()
		PARTITION BY toYYYYMM(timestamp)
		ORDER BY (timestamp, program_id)
	`)
	if err != nil {
		return fmt.Errorf("FAILED TO CREATE STATS TABLE: %w", err)
	}

	err = c.conn.Exec(ctx, `
		CREATE MATERIALIZED VIEW IF NOT EXISTS program_stats_mv TO program_stats AS
		SELECT
			toStartOfMinute(timestamp) as timestamp,
			program_id,
			count() as total_calls,
			sum(compute_units) as total_cu,
			sum(if(is_error, 1, 0)) as error_count
		FROM solana_logs_v2
		GROUP BY timestamp, program_id
	`)
	if err != nil {
		return fmt.Errorf("FAILED TO CREATE STATS VIEW: %w", err)
	}

	err = c.conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS pattern_alerts (
			timestamp DateTime64(3),
			pattern_name String,
			confidence Float32,
			signature String,
			raw_match String
		) ENGINE = MergeTree()
		PARTITION BY toYYYYMM(timestamp)
		ORDER BY (timestamp, pattern_name)
	`)
	if err != nil {
		return fmt.Errorf("FAILED TO CREATE ALERTS TABLE: %w", err)
	}

	err = c.conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS indexer_state (
			id String,
			last_slot UInt64,
			updated_at DateTime64(3)
		) ENGINE = ReplacingMergeTree(updated_at)
		ORDER BY id
	`)
	if err != nil {
		return fmt.Errorf("FAILED TO CREATE INDEXER STATE TABLE: %w", err)
	}

	return nil
}

func (c *Client) BatchInsertLogs(logs []LogEntry) error {
	ctx := context.Background()
	batch, err := c.conn.PrepareBatch(ctx, "INSERT INTO solana_logs_v2 (timestamp, slot, signature, log_message, program_id, signer, compute_units, is_error)")
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
			l.Signer,
			l.ComputeUnits,
			l.IsError,
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

func (c *Client) GetLastIndexedSlot() (uint64, error) {
	ctx := context.Background()
	var slot uint64
	err := c.conn.QueryRow(ctx, "SELECT last_slot FROM indexer_state WHERE id = 'main' FINAL").Scan(&slot)
	if err != nil {
		return 0, nil
	}
	return slot, nil
}

func (c *Client) UpdateLastIndexedSlot(slot uint64) error {
	ctx := context.Background()
	return c.conn.Exec(ctx, "INSERT INTO indexer_state (id, last_slot, updated_at) VALUES ('main', ?, ?)", slot, time.Now())
}

func (c *Client) GetRecentLogs(limit int) ([]LogEntry, error) {
	ctx := context.Background()
	rows, err := c.conn.Query(ctx, "SELECT timestamp, slot, signature, log_message, program_id, signer, compute_units, is_error FROM solana_logs_v2 ORDER BY timestamp DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []LogEntry
	for rows.Next() {
		var l LogEntry
		if err := rows.Scan(&l.Timestamp, &l.Slot, &l.Signature, &l.LogMessage, &l.ProgramID, &l.Signer, &l.ComputeUnits, &l.IsError); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, nil
}

type LogEntry struct {
	Timestamp    time.Time
	Slot         uint64
	Signature    string
	LogMessage   string
	ProgramID    string
	Signer       string
	ComputeUnits uint32
	IsError      bool
}

type AlertEntry struct {
	Timestamp   time.Time
	PatternName string
	Confidence  float32
	Signature   string
	RawMatch    string
}