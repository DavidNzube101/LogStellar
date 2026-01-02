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
	"logstellar/gpu"
	"logstellar/ingestor"
	"logstellar/patterns"
)

const (
	// Solana RPC endpoints
	MAINNET_RPC = "https://api.mainnet-beta.solana.com"
	DEVNET_RPC  = "https://api.devnet.solana.com"
	
	// Batch size for GPU processing
	BATCH_SIZE = 10000
)

func main() {
	fmt.Println("🌟 LogStellar - GPU-Accelerated Solana Log Analyzer")
	fmt.Println("================================================")

	// Initialize components
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Initialize GPU Scanner
	fmt.Println("\n[1/4] Initializing GPU Scanner...")
	scanner, err := gpu.NewScanner(BATCH_SIZE)
	if err != nil {
		log.Fatalf("Failed to initialize GPU scanner: %v", err)
	}
	defer scanner.Close()

	// 2. Initialize Pattern Detector
	fmt.Println("[2/4] Loading Pattern Definitions...")
	detector := patterns.NewDetector()
	detector.LoadPatterns()

	// 3. Initialize Solana Ingestor
	fmt.Println("[3/4] Connecting to Solana RPC...")
	rpcEndpoint := DEVNET_RPC // Use devnet for testing
	if endpoint := os.Getenv("SOLANA_RPC"); endpoint != "" {
		rpcEndpoint = endpoint
	}
	
	ing, err := ingestor.NewIngestor(rpcEndpoint)
	if err != nil {
		log.Fatalf("Failed to initialize Solana ingestor: %v", err)
	}
	defer ing.Close()

	// 4. Start Dashboard Server
	fmt.Println("[4/4] Starting Dashboard Server...")
	dashServer := dashboard.NewServer(8080)
	go dashServer.Start()

	fmt.Println("\n✅ LogStellar is running!")
	fmt.Println("📊 Dashboard: http://localhost:8080")
	fmt.Println("🔍 Scanning Solana logs for patterns...")
	fmt.Println("\nPress Ctrl+C to stop\n")

	// Main processing loop
	go processLogs(ctx, ing, scanner, detector, dashServer)

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\n\n🛑 Shutting down LogStellar...")
	cancel()
	time.Sleep(1 * time.Second)
	fmt.Println("👋 Goodbye!")
}

func processLogs(ctx context.Context, ing *ingestor.Ingestor, scanner *gpu.Scanner, 
	detector *patterns.Detector, dash *dashboard.Server) {
	
	logBatch := make([]string, 0, BATCH_SIZE)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Fetch recent transactions
			logs, err := ing.FetchRecentLogs(100)
			if err != nil {
				log.Printf("Error fetching logs: %v", err)
				continue
			}

			logBatch = append(logBatch, logs...)

			// Process when batch is full
			if len(logBatch) >= BATCH_SIZE {
				processBatch(logBatch, scanner, detector, dash)
				logBatch = logBatch[:0] // Clear batch
			}
		}
	}
}

func processBatch(logs []string, scanner *gpu.Scanner, detector *patterns.Detector, dash *dashboard.Server) {
	startTime := time.Now()
	
	// Get patterns to search for
	patterns := detector.GetPatterns()
	
	// Execute GPU scan
	results, err := scanner.ScanLogs(logs, patterns)
	if err != nil {
		log.Printf("GPU scan error: %v", err)
		return
	}

	duration := time.Since(startTime)
	
	// Process results
	alertCount := 0
	for _, result := range results {
		if result.Match {
			alert := fmt.Sprintf("🎯 Pattern detected: %s (confidence: %.2f)", 
				result.PatternName, result.Confidence)
			dash.AddAlert(alert, result)
			alertCount++
		}
	}

	// Log statistics
	log.Printf("✨ Processed %d logs in %v (GPU-accelerated) - %d alerts", 
		len(logs), duration, alertCount)
	
	dash.UpdateStats(len(logs), duration, alertCount)
}
