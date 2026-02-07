package patterns

import (
	"logstellar/gpu"

	"github.com/gagliardetto/solana-go"
)

type Detector struct {
	patterns []gpu.Pattern
}

func NewDetector() *Detector {
	return &Detector{
		patterns: make([]gpu.Pattern, 0),
	}
}

func (d *Detector) GetFilterPrograms() []solana.PublicKey {
	programMap := make(map[string]bool)
	var programs []solana.PublicKey

	for _, p := range d.patterns {
		for _, sig := range p.Signatures {
			if len(sig) >= 32 && len(sig) <= 44 {
				pk, err := solana.PublicKeyFromBase58(sig)
				if err == nil {
					if !programMap[pk.String()] {
						programMap[pk.String()] = true
						programs = append(programs, pk)
					}
				}
			}
		}
	}
	return programs
}

func (d *Detector) LoadPatterns() {
	d.patterns = []gpu.Pattern{
		{
			Name: "Raydium LP Creation",
			Signatures: []string{
				"675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8",
				"initialize",
				"CreatePool",
			},
			Threshold: 0.6,
			Category:  "DeFi",
		},
		{
			Name: "Orca LP Creation",
			Signatures: []string{
				"9W959DqEETiGZocYWCQPaJ6sBmUzgfxXfqGeTEdp3aQP",
				"initializePool",
			},
			Threshold: 0.5,
			Category:  "DeFi",
		},
		{
			Name: "MEV Sandwich Attack",
			Signatures: []string{
				"swap",
				"Program log: profit",
				"arbitrage",
			},
			Threshold: 0.7,
			Category:  "MEV",
		},
		{
			Name: "Jupiter Aggregator Swap",
			Signatures: []string{
				"JUP6LkbZbjS1jKKwapdHNy74zcZ3tLUZoi5QNyVTaV4",
				"sharedAccountsRoute",
			},
			Threshold: 0.5,
			Category:  "DeFi",
		},
		{
			Name: "Magic Eden NFT Sale",
			Signatures: []string{
				"M2mx93ekt1fmXSVkTrUL9xVFHkmME8HTUi5Cyc5aF7K",
				"ExecuteSale",
				"Transfer",
			},
			Threshold: 0.6,
			Category:  "NFT",
		},
		{
			Name: "New Token Launch",
			Signatures: []string{
				"TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA",
				"InitializeMint",
			},
			Threshold: 0.5,
			Category:  "Token",
		},
		{
			Name: "Flash Loan Execution",
			Signatures: []string{
				"borrow",
				"repay",
				"flashloan",
			},
			Threshold: 0.7,
			Category:  "DeFi",
		},
		{
			Name: "Large Transfer Detection",
			Signatures: []string{
				"Transfer",
				"amount: 1000000",
			},
			Threshold: 0.5,
			Category:  "Transfer",
		},
		{
			Name: "Failed Transaction",
			Signatures: []string{
				"failed",
				"error",
				"revert",
			},
			Threshold: 0.4,
			Category:  "Error",
		},
		{
			Name: "Pump.fun Launch",
			Signatures: []string{
				"6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P",
				"create",
			},
			Threshold: 0.5,
			Category:  "Meme",
		},
		{
			Name: "Raydium V4 Swap",
			Signatures: []string{
				"675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8",
				"swap",
			},
			Threshold: 0.6,
			Category:  "DeFi",
		},
		{
			Name: "Orca Whirlpool Swap",
			Signatures: []string{
				"whirLbMiqauG3qyUmg2S64P2f24WoYM6vnyUshKDqqV",
				"swap",
			},
			Threshold: 0.6,
			Category:  "DeFi",
		},
	}
}

func (d *Detector) GetPatterns() []gpu.Pattern {
	return d.patterns
}

func (d *Detector) AddCustomPattern(pattern gpu.Pattern) {
	d.patterns = append(d.patterns, pattern)
}

func (d *Detector) GetPatternsByCategory(category string) []gpu.Pattern {
	filtered := make([]gpu.Pattern, 0)
	for _, p := range d.patterns {
		if p.Category == category {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

func (d *Detector) RemovePattern(name string) {
	newPatterns := make([]gpu.Pattern, 0)
	for _, p := range d.patterns {
		if p.Name != name {
			newPatterns = append(newPatterns, p)
		}
	}
	d.patterns = newPatterns
}