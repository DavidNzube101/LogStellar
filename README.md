# LOGSTELLAR

![LogStellar Dashboard](public/images/dashboard.png)

**LOGSTELLAR** is a high-frequency Solana log indexer and observability engine designed for sub-second pattern detection and big-data blockchain analytics.

## TOOLSTACK
- **LANGUAGE:** GO (GOLANG)
- **DATABASE:** CLICKHOUSE (OLAP)
- **INGESTION:** HYBRID WEBSOCKET + RPC POLLING
- **UI:** SSE-POWERED REAL-TIME TERMINAL
- **INFRA:** DOCKER + VPS (AMD EPYC)

## CORE LOGIC
1. **INGEST:** CAPTURES RAW TRANSACTION LOGS VIA SOLANA WS/RPC.
2. **SCAN:** PARALLEL PATTERN MATCHING FOR DEX SWAPS, TOKEN LAUNCHES, AND WHALE MOVEMENTS.
3. **INDEX:** STORES ENRICHED DATA (SIGNERS, CU, ERRORS) IN CLICKHOUSE.
4. **STREAM:** PUSHES LIVE SIGNALS TO THE DASHBOARD VIA SERVER-SENT EVENTS.

## USAGE
```bash
docker run -d --name clickhouse-logstellar -p 9000:9000 clickhouse/clickhouse-server # starting the db

export SOLANA_RPC="your_rpc_url" # from helius/quicknode; ideal 'casue it's not rate-limited like fallbacks
./logstellar # running the binary
```