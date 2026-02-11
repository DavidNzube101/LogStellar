package logstellar

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/DavidNzube101/LogStellar/dashboard"
	"github.com/DavidNzube101/LogStellar/database"
	"github.com/DavidNzube101/LogStellar/gpu"
	"github.com/DavidNzube101/LogStellar/ingestor"
	"github.com/DavidNzube101/LogStellar/patterns"
)

type Signal struct {
	Result gpu.ScanResult
	Log    ingestor.IngestedLog
}

type Config struct {
	RPCEndpoint    string
	ClickHouseAddr string
	DashboardPort  int
	BatchSize      int
	Workers        int
}

type Engine struct {
	config     Config
	scanner    *gpu.Scanner
	detector   *patterns.Detector
	dbClient   *database.Client
	dashServer *dashboard.Server
	ingestor   *ingestor.Ingestor
	signals    chan Signal
}

func NewEngine(cfg Config) (*Engine, error) {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 1000
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 10
	}

	scanner, err := gpu.NewScanner(cfg.BatchSize)
	if err != nil {
		return nil, fmt.Errorf("FAILED TO INITIALIZE GPU SCANNER: %w", err)
	}

	detector := patterns.NewDetector()
	detector.LoadPatterns()

	ing, err := ingestor.NewIngestor(cfg.RPCEndpoint)
	if err != nil {
		scanner.Close()
		return nil, fmt.Errorf("FAILED TO INITIALIZE SOLANA INGESTOR: %w", err)
	}

	var dbClient *database.Client
	if cfg.ClickHouseAddr != "" {
		dbClient, err = database.NewClient(cfg.ClickHouseAddr)
		if err != nil {
			log.Printf("WARNING: FAILED TO CONNECT TO CLICKHOUSE: %v. INDEXING DISABLED.", err)
		}
	}

	var dashServer *dashboard.Server
	if cfg.DashboardPort > 0 {
		dashServer = dashboard.NewServer(cfg.DashboardPort, dbClient, scanner)
	}

	return &Engine{
		config:     cfg,
		scanner:    scanner,
		detector:   detector,
		dbClient:   dbClient,
		dashServer: dashServer,
		ingestor:   ing,
		signals:    make(chan Signal, 1000),
	}, nil
}

func (e *Engine) Start(ctx context.Context) error {
	if err := e.ingestor.Connect(); err != nil {
		log.Printf("WEBSOCKET CONNECT FAILED: %v. FALLING BACK TO POLL-ONLY MODE.", err)
	} else {
		log.Println("CONNECTED TO SOLANA WEBSOCKET: REAL-TIME STREAM ENABLED")
	}

	var startSlot uint64
	if e.dbClient != nil {
		lastSlot, err := e.dbClient.GetLastIndexedSlot()
		if err == nil && lastSlot > 0 {
			startSlot = lastSlot + 1
			fmt.Printf("RESUMING INDEXER FROM SLOT: %d\n", startSlot)
		}
	}

	if e.dashServer != nil {
		go e.dashServer.Start()
		fmt.Printf("DASHBOARD: HTTP://LOCALHOST:%d\n", e.config.DashboardPort)
	}

	fmt.Println("LOGSTELLAR ENGINE STARTED")

	logChan := make(chan []ingestor.IngestedLog, 1000)

	pollChan := e.ingestor.Start(ctx, startSlot, e.config.Workers)
	go func() {
		for logs := range pollChan {
			logChan <- logs
		}
	}()

	if e.ingestor.HasWS() {
		go func() {
			filterPrograms := e.detector.GetFilterPrograms()
			if err := e.ingestor.StreamLogs(ctx, logChan, filterPrograms); err != nil {
				log.Printf("STREAM ERROR: %v", err)
			}
		}()
	}

	go func() {
		var maxSlot uint64
		for {
			select {
			case <-ctx.Done():
				return
			case logs := <-logChan:
				if len(logs) == 0 {
					continue
				}

				for _, l := range logs {
					if l.Slot > maxSlot {
						maxSlot = l.Slot
					}
				}

				e.processBatch(logs)

				if e.dbClient != nil && maxSlot > 0 {
					e.dbClient.UpdateLastIndexedSlot(maxSlot)
				}
			}
		}
	}()

	return nil
}

func (e *Engine) Signals() <-chan Signal {
	return e.signals
}

func (e *Engine) Close() {
	if e.scanner != nil {
		e.scanner.Close()
	}
	if e.ingestor != nil {
		e.ingestor.Close()
	}
	if e.dbClient != nil {
		e.dbClient.Close()
	}
}

func (e *Engine) processBatch(logs []ingestor.IngestedLog) {
	startTime := time.Now()

	gpuLogs := make([]string, len(logs))
	for i, l := range logs {
		gpuLogs[i] = l.LogMessage
	}

	pats := e.detector.GetPatterns()

	results, err := e.scanner.ScanLogs(gpuLogs, pats)
	if err != nil {
		log.Printf("GPU SCAN ERROR: %v", err)
		return
	}

	duration := time.Since(startTime)

	if e.dbClient != nil || e.dashServer != nil {
		dbLogs := make([]database.LogEntry, len(logs))
		for i, l := range logs {
			dbLogs[i] = database.LogEntry{
				Timestamp:    l.Timestamp,
				Slot:         l.Slot,
				Signature:    l.Signature,
				LogMessage:   l.LogMessage,
				ProgramID:    l.ProgramID,
				Signer:       l.Signer,
				ComputeUnits: l.ComputeUnits,
				IsError:      l.IsError,
			}
		}

		if e.dbClient != nil {
			go func(entries []database.LogEntry) {
				if err := e.dbClient.BatchInsertLogs(entries); err != nil {
					log.Printf("FAILED TO INDEX LOGS: %v", err)
				}
			}(dbLogs)
		}

		if e.dashServer != nil {
			for _, l := range dbLogs {
				e.dashServer.BroadcastLog(l)
			}
		}
	}

	alertCount := 0
	for _, result := range results {
		if result.Match {
			originalLog := logs[result.LogIndex]
			
			// Send to signals channel
			select {
			case e.signals <- Signal{Result: result, Log: originalLog}:
			default:
				// Channel full, drop signal or handle accordingly
			}

			if e.dashServer != nil {
				alert := fmt.Sprintf("PATTERN DETECTED: %s (CONFIDENCE: %.2f)",
					result.PatternName, result.Confidence)
				e.dashServer.AddAlert(alert, result)
			}
			alertCount++

			if e.dbClient != nil {
				go func(pName string, conf float32, sig string, raw string) {
					e.dbClient.BatchInsertAlerts([]database.AlertEntry{{
						Timestamp:   time.Now(),
						PatternName: pName,
						Confidence:  conf,
						Signature:   sig,
						RawMatch:    raw,
					}})
				}(result.PatternName, float32(result.Confidence), originalLog.Signature, originalLog.LogMessage)
			}
		}
	}

	if e.dashServer != nil {
		e.dashServer.UpdateStats(len(logs), duration, alertCount)
	}
}
