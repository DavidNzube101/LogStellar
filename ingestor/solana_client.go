package ingestor

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

type Ingestor struct {
	client    *rpc.Client
	ctx       context.Context
	lastSlot  uint64
}

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

// FetchRecentLogs fetches recent transaction logs from Solana
func (i *Ingestor) FetchRecentLogs(limit int) ([]string, error) {
	// Get recent slot
	slot, err := i.client.GetSlot(i.ctx, rpc.CommitmentFinalized)
	if err != nil {
		return nil, fmt.Errorf("failed to get slot: %w", err)
	}

	if i.lastSlot == 0 {
		i.lastSlot = slot - 10 // Start from 10 slots back
	}

	logs := make([]string, 0, limit)
	
	// Fetch blocks from lastSlot to current
	for s := i.lastSlot; s <= slot && len(logs) < limit; s++ {
		block, err := i.client.GetBlock(i.ctx, s)
		if err != nil {
			log.Printf("Warning: Failed to get block %d: %v", s, err)
			continue
		}

		if block == nil || block.Transactions == nil {
			continue
		}

		// Extract logs from transactions
		for _, tx := range block.Transactions {
			if tx.Meta == nil || tx.Meta.LogMessages == nil {
				continue
			}

			// Collect log messages
			for _, logMsg := range tx.Meta.LogMessages {
				logs = append(logs, logMsg)
				if len(logs) >= limit {
					break
				}
			}
		}
	}

	i.lastSlot = slot + 1
	return logs, nil
}

// GetTransactionSignatures fetches recent transaction signatures
func (i *Ingestor) GetTransactionSignatures(address string, limit int) ([]string, error) {
	pubkey, err := solana.PublicKeyFromBase58(address)
	if err != nil {
		return nil, fmt.Errorf("invalid address: %w", err)
	}

	// Note: Limit parameter may need to be passed differently depending on SDK version
	sigs, err := i.client.GetSignaturesForAddress(i.ctx, pubkey)
	if err != nil {
		return nil, fmt.Errorf("failed to get signatures: %w", err)
	}

	// Take only 'limit' number of signatures
	signatures := make([]string, 0, limit)
	for idx, sig := range sigs {
		if idx >= limit {
			break
		}
		signatures = append(signatures, sig.Signature.String())
	}

	return signatures, nil
}

// GetTransactionDetails fetches full transaction details
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

// DecodeInstructionData decodes base64 instruction data
func DecodeInstructionData(data string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(data)
}

// GetProgramLogs fetches logs for a specific program ID
func (i *Ingestor) GetProgramLogs(programID string, limit int) ([]string, error) {
	pubkey, err := solana.PublicKeyFromBase58(programID)
	if err != nil {
		return nil, fmt.Errorf("invalid program ID: %w", err)
	}

	// Get signatures for the program
	sigs, err := i.client.GetSignaturesForAddress(i.ctx, pubkey)
	if err != nil {
		return nil, fmt.Errorf("failed to get signatures: %w", err)
	}

	logs := make([]string, 0)
	
	// Fetch logs for each transaction (up to limit)
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
