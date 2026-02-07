package ingestor

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/ws"
)

type Ingestor struct {
	client      *rpc.Client
	wsClient    *ws.Client
	rpcEndpoint string
	wsEndpoint  string
	ctx         context.Context
	lastSlot    uint64
}

type IngestedLog struct {
	Slot         uint64
	Signature    string
	LogMessage   string
	ProgramID    string
	Timestamp    time.Time
	Signer       string
	ComputeUnits uint32
	IsError      bool
}

var programInvokeRegex = regexp.MustCompile(`^Program (\w+) invoke`)

func NewIngestor(rpcEndpoint string) (*Ingestor, error) {
	client := rpc.New(rpcEndpoint)
	
	wsEndpoint := strings.Replace(rpcEndpoint, "https://", "wss://", 1)
	wsEndpoint = strings.Replace(wsEndpoint, "http://", "ws://", 1)

	return &Ingestor{
		client:      client,
		rpcEndpoint: rpcEndpoint,
		wsEndpoint:  wsEndpoint,
		ctx:         context.Background(),
		lastSlot:    0,
	}, nil
}

func (i *Ingestor) Connect() error {
	var err error
	i.wsClient, err = ws.Connect(context.Background(), i.wsEndpoint)
	if err != nil {
		return fmt.Errorf("WS CONNECT ERROR: %w", err)
	}
	return nil
}

func (i *Ingestor) HasWS() bool {
	return i.wsClient != nil
}

func (i *Ingestor) Close() {
	if i.wsClient != nil {
		i.wsClient.Close()
	}
}

func (i *Ingestor) StreamLogs(ctx context.Context, outCh chan<- []IngestedLog, programs []solana.PublicKey) error {
	var filter ws.LogsSubscribeFilter = ws.LogsSubscribeFilterAll
	if len(programs) > 0 {
		filter = ws.LogsSubscribeFilterMentions(programs)
	}

	sub, err := i.wsClient.LogsSubscribe(
		filter,
		rpc.CommitmentProcessed,
	)
	if err != nil {
		return fmt.Errorf("WS SUBSCRIBE ERROR: %w", err)
	}

	go func() {
		defer sub.Unsubscribe()
		
		for {
			select {
			case <-ctx.Done():
				return
			case result, ok := <-sub.Response():
				if !ok {
					return
				}

				logs := make([]IngestedLog, 0, len(result.Value.Logs))
				sig := result.Value.Signature.String()
				ts := time.Now()

				activeProgram := "system"
				for _, logMsg := range result.Value.Logs {
					if match := programInvokeRegex.FindStringSubmatch(logMsg); len(match) > 1 {
						activeProgram = match[1]
					}
					
					logs = append(logs, IngestedLog{
						Slot:         result.Context.Slot,
						Signature:    sig,
						LogMessage:   logMsg,
						ProgramID:    activeProgram,
						Timestamp:    ts,
						Signer:       "unknown",
						ComputeUnits: 0,
						IsError:      result.Value.Err != nil,
					})
				}
				
				if len(logs) > 0 {
					outCh <- logs
				}
			}
		}
	}()

	return nil
}

func (i *Ingestor) Start(ctx context.Context, startSlot uint64, workers int) <-chan []IngestedLog {
	outCh := make(chan []IngestedLog, workers*2)
	
	if startSlot == 0 {
		current, err := i.client.GetSlot(ctx, rpc.CommitmentFinalized)
		if err != nil {
			log.Printf("FAILED TO GET CURRENT SLOT: %v", err)
			startSlot = 0 
		} else {
			startSlot = current
		}
	}

	go func() {
		defer close(outCh)

		type job struct {
			slot uint64
		}
		jobs := make(chan job, workers*2)

		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range jobs {
					logs, err := i.FetchSlotLogs(ctx, j.slot)
					if err != nil {
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
		signer := ""
		parsedTx, err := tx.GetTransaction()
		if err == nil && len(parsedTx.Signatures) > 0 {
			signature = parsedTx.Signatures[0].String()
			signer = signature 
		}

		cu := uint32(0)
		if tx.Meta.ComputeUnitsConsumed != nil {
			cu = uint32(*tx.Meta.ComputeUnitsConsumed)
		}
		
		isError := tx.Meta.Err != nil

		activeProgram := "system" 

		for _, logMsg := range tx.Meta.LogMessages {
			if match := programInvokeRegex.FindStringSubmatch(logMsg); len(match) > 1 {
				activeProgram = match[1]
			}

			logs = append(logs, IngestedLog{
				Slot:         slot,
				Signature:    signature,
				LogMessage:   logMsg,
				ProgramID:    activeProgram,
				Timestamp:    blockTime,
				Signer:       signer,
				ComputeUnits: cu,
				IsError:      isError,
			})
		}
	}
	return logs, nil
}

func (i *Ingestor) FetchRecentLogs(limit int) ([]IngestedLog, error) {
	slot, err := i.client.GetSlot(i.ctx, rpc.CommitmentFinalized)
	if err != nil {
		return nil, fmt.Errorf("FAILED TO GET SLOT: %w", err)
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