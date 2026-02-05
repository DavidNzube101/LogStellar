package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"logstellar/dashboard"
	"logstellar/database"
	"logstellar/gpu"
	"logstellar/ingestor"
	"logstellar/patterns"
)

const (
	MAINNET_RPC = "https://api.mainnet-beta.solana.com"
	DEVNET_RPC  = "https://api.devnet.solana.com"
	BATCH_SIZE = 1000
)

func main() {
	fmt.Println("LOGSTELLAR - GPU-ACCELERATED SOLANA LOG ANALYZER")
	fmt.Println("================================================")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("INITIALIZING GPU SCANNER...")
	scanner, err := gpu.NewScanner(BATCH_SIZE)
	if err != nil {
		log.Fatalf("FAILED TO INITIALIZE GPU SCANNER: %v", err)
	}
	defer scanner.Close()

	fmt.Println("LOADING PATTERN DEFINITIONS...")
	detector := patterns.NewDetector()
	detector.LoadPatterns()

	fmt.Println("CONNECTING TO SOLANA RPC...")
	rpcEndpoint := MAINNET_RPC
	if endpoint := os.Getenv("SOLANA_RPC"); endpoint != "" {
		rpcEndpoint = endpoint
	}
	
	ing, err := ingestor.NewIngestor(rpcEndpoint)
	if err != nil {
		log.Fatalf("FAILED TO INITIALIZE SOLANA INGESTOR: %v", err)
	}
	defer ing.Close()

	fmt.Println("CONNECTING TO CLICKHOUSE...")
	dbClient, err := database.NewClient("127.0.0.1:9000")
	var startSlot uint64
	if err != nil {
		log.Printf("WARNING: FAILED TO CONNECT TO CLICKHOUSE: %v. INDEXING DISABLED.", err)
	} else {
		defer dbClient.Close()
		log.Println("CONNECTED TO CLICKHOUSE")
		
		lastSlot, err := dbClient.GetLastIndexedSlot()
		if err == nil && lastSlot > 0 {
			startSlot = lastSlot + 1
			fmt.Printf("RESUMING INDEXER FROM SLOT: %d\n", startSlot)
		}
	}

	fmt.Println("STARTING DASHBOARD SERVER...")
	dashServer := dashboard.NewServer(8080)
	go dashServer.Start()

	fmt.Println("LOGSTELLAR IS RUNNING")
	fmt.Println("DASHBOARD: HTTP://LOCALHOST:8080")
	fmt.Println("SCANNING SOLANA LOGS FOR PATTERNS...")

	logChan := ing.Start(ctx, startSlot, 10)

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
				
				// Track highest slot in this batch
				for _, l := range logs {
					if l.Slot > maxSlot {
						maxSlot = l.Slot
					}
				}

				processBatch(logs, scanner, detector, dashServer, dbClient)
				
				if dbClient != nil && maxSlot > 0 {
					dbClient.UpdateLastIndexedSlot(maxSlot)
				}
			}
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("SHUTTING DOWN LOGSTELLAR...")
	cancel()
	time.Sleep(1 * time.Second)
	fmt.Println("GOODBYE")
}

func processBatch(logs []ingestor.IngestedLog, scanner *gpu.Scanner, detector *patterns.Detector, 
	dash *dashboard.Server, db *database.Client) {
	
	startTime := time.Now()
	
	gpuLogs := make([]string, len(logs))
	for i, l := range logs {
		gpuLogs[i] = l.LogMessage
	}

	patterns := detector.GetPatterns()
	
	results, err := scanner.ScanLogs(gpuLogs, patterns)
	if err != nil {
		log.Printf("GPU SCAN ERROR: %v", err)
		return
	}

	duration := time.Since(startTime)
	
	if db != nil {
		dbLogs := make([]database.LogEntry, len(logs))
		for i, l := range logs {
			dbLogs[i] = database.LogEntry{
				Timestamp:  l.Timestamp,
				Slot:       l.Slot,
				Signature:  l.Signature,
				LogMessage: l.LogMessage,
				ProgramID:  l.ProgramID,
			}
		}
		
		go func(entries []database.LogEntry) {
			if err := db.BatchInsertLogs(entries); err != nil {
				log.Printf("FAILED TO INDEX LOGS: %v", err)
			}
		}(dbLogs)
	}

	alertCount := 0
	for _, result := range results {
		if result.Match {
			alert := fmt.Sprintf("PATTERN DETECTED: %s (CONFIDENCE: %.2f)", 
				result.PatternName, result.Confidence)
			dash.AddAlert(alert, result)
			alertCount++

			if db != nil {
				originalLog := logs[result.LogIndex]
				go func(pName string, conf float32, sig string, raw string) {
					db.BatchInsertAlerts([]database.AlertEntry{{
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

	dash.UpdateStats(len(logs), duration, alertCount)
}