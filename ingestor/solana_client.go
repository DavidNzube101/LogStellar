package ingestor

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

type Ingestor struct {
	client   *rpc.Client
	ctx      context.Context
	lastSlot uint64
}

type IngestedLog struct {
	Slot       uint64
	Signature  string
	LogMessage string
	ProgramID  string
	Timestamp  time.Time
}

// Regex to extract Program ID from "Program <ID> invoke" logs
var programInvokeRegex = regexp.MustCompile(`^Program (\w+) invoke`)

func NewIngestor(rpcEndpoint string) (*Ingestor, error) {
	client := rpc.New(rpcEndpoint)

	return &Ingestor{
		client:   client,
		ctx:      context.Background(),
		lastSlot: 0,
	}, nil
}

func (i *Ingestor) Close() {
	// Cleanup if needed
}

// Start begins the parallel ingestion process in the background.
// It returns a channel that receives batches of logs.
func (i *Ingestor) Start(ctx context.Context, startSlot uint64, workers int) <-chan []IngestedLog {
	outCh := make(chan []IngestedLog, workers*2)
	
	if startSlot == 0 {
		// Get current slot if 0
		current, err := i.client.GetSlot(ctx, rpc.CommitmentFinalized)
		if err != nil {
			log.Printf("❌ Failed to get current slot: %v", err)
			startSlot = 0 
		} else {
			startSlot = current
		}
	}

	go func() {
		defer close(outCh)

		// Job queue
		type job struct {
			slot uint64
		}
		jobs := make(chan job, workers*2)

		// Start workers
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range jobs {
					logs, err := i.FetchSlotLogs(ctx, j.slot)
					if err != nil {
						// Quietly skip or handle rate limits
						continue
					}
					if len(logs) > 0 {
						outCh <- logs
					}
				}
			}()
		}

		currentSlot := startSlot
		ticker := time.NewTicker(400 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				close(jobs)
				wg.Wait()
				return
			case <-ticker.C:
				tip, err := i.client.GetSlot(ctx, rpc.CommitmentFinalized)
				if err != nil {
					continue
				}

				for s := currentSlot; s <= tip; s++ {
					select {
					case jobs <- job{slot: s}:
						currentSlot++
					case <-ctx.Done():
						close(jobs)
						wg.Wait()
						return
					}
				}
			}
		}
	}()

	return outCh
}

func (i *Ingestor) FetchSlotLogs(ctx context.Context, slot uint64) ([]IngestedLog, error) {
	block, err := i.client.GetBlockWithOpts(
		ctx,
		slot,
		&rpc.GetBlockOpts{
			Commitment:                     rpc.CommitmentFinalized,
			MaxSupportedTransactionVersion: func() *uint64 { v := uint64(0); return &v }(),
		},
	)
	if err != nil {
		return nil, err
	}

	if block == nil || block.Transactions == nil {
		return nil, nil
	}

	blockTime := time.Now()
	if block.BlockTime != nil {
		blockTime = time.Unix(int64(*block.BlockTime), 0)
	}

	logs := make([]IngestedLog, 0, len(block.Transactions)*4)

	for _, tx := range block.Transactions {
		if tx.Meta == nil || tx.Meta.LogMessages == nil {
			continue
		}

		signature := ""
		parsedTx, err := tx.GetTransaction()
		if err == nil && len(parsedTx.Signatures) > 0 {
			signature = parsedTx.Signatures[0].String()
		}

		activeProgram := "system" 

		for _, logMsg := range tx.Meta.LogMessages {
			if match := programInvokeRegex.FindStringSubmatch(logMsg); len(match) > 1 {
				activeProgram = match[1]
			}

			logs = append(logs, IngestedLog{
				Slot:       slot,
				Signature:  signature,
				LogMessage: logMsg,
				ProgramID:  activeProgram,
				Timestamp:  blockTime,
			})
		}
	}
	return logs, nil
}

// Deprecated: Use Start() for parallel ingestion
func (i *Ingestor) FetchRecentLogs(limit int) ([]IngestedLog, error) {
	slot, err := i.client.GetSlot(i.ctx, rpc.CommitmentFinalized)
	if err != nil {
		return nil, fmt.Errorf("failed to get slot: %w", err)
	}

	if i.lastSlot == 0 {
		i.lastSlot = slot - 2
	}

	logs := make([]IngestedLog, 0, limit)
	for s := i.lastSlot; s <= slot && len(logs) < limit; s++ {
		slotLogs, err := i.FetchSlotLogs(i.ctx, s)
		if err != nil {
			continue
		}
		if len(slotLogs) > 0 {
			logs = append(logs, slotLogs...)
		}
	}

	i.lastSlot = slot + 1
	return logs, nil
}

func (i *Ingestor) GetTransactionSignatures(address string, limit int) ([]string, error) {
	pubkey, err := solana.PublicKeyFromBase58(address)
	if err != nil {
		return nil, fmt.Errorf("invalid address: %w", err)
	}

	sigs, err := i.client.GetSignaturesForAddress(i.ctx, pubkey)
	if err != nil {
		return nil, fmt.Errorf("failed to get signatures: %w", err)
	}

	signatures := make([]string, 0, limit)
	for idx, sig := range sigs {
		if idx >= limit {
			break
		}
		signatures = append(signatures, sig.Signature.String())
	}

	return signatures, nil
}

func (i *Ingestor) GetTransactionDetails(signature string) ([]string, error) {
	sig, err := solana.SignatureFromBase58(signature)
	if err != nil {
		return nil, fmt.Errorf("invalid signature: %w", err)
	}

	tx, err := i.client.GetTransaction(
		i.ctx,
		sig,
		&rpc.GetTransactionOpts{
			Encoding:   solana.EncodingBase64,
			Commitment: rpc.CommitmentFinalized,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction: %w", err)
	}

	if tx == nil || tx.Meta == nil {
		return nil, fmt.Errorf("transaction not found")
	}

	return tx.Meta.LogMessages, nil
}

func DecodeInstructionData(data string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(data)
}

func (i *Ingestor) GetProgramLogs(programID string, limit int) ([]string, error) {
	pubkey, err := solana.PublicKeyFromBase58(programID)
	if err != nil {
		return nil, fmt.Errorf("invalid program ID: %w", err)
	}

	sigs, err := i.client.GetSignaturesForAddress(i.ctx, pubkey)
	if err != nil {
		return nil, fmt.Errorf("failed to get signatures: %w", err)
	}

	logs := make([]string, 0)

	for idx, sig := range sigs {
		if idx >= limit {
			break
		}
		txLogs, err := i.GetTransactionDetails(sig.Signature.String())
		if err != nil {
			log.Printf("Warning: Failed to get tx %s: %v", sig.Signature, err)
			continue
		}
		logs = append(logs, txLogs...)
	}

	return logs, nil
}