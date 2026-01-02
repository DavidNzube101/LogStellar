package patterns

import (
	"logstellar/gpu"
)

// Detector manages pattern definitions for log analysis
type Detector struct {
	patterns []gpu.Pattern
}

func NewDetector() *Detector {
	return &Detector{
		patterns: make([]gpu.Pattern, 0),
	}
}

// LoadPatterns initializes all signature patterns
func (d *Detector) LoadPatterns() {
	d.patterns = []gpu.Pattern{
		// Raydium Liquidity Pool Detection
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

		// Orca Liquidity Pool
		{
			Name: "Orca LP Creation",
			Signatures: []string{
				"9W959DqEETiGZocYWCQPaJ6sBmUzgfxXfqGeTEdp3aQP",
				"initializePool",
			},
			Threshold: 0.5,
			Category:  "DeFi",
		},

		// MEV Bot Activity Detection
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

		// Jupiter Aggregator Swaps
		{
			Name: "Jupiter Aggregator Swap",
			Signatures: []string{
				"JUP6LkbZbjS1jKKwapdHNy74zcZ3tLUZoi5QNyVTaV4",
				"sharedAccountsRoute",
			},
			Threshold: 0.5,
			Category:  "DeFi",
		},

		// NFT Marketplace Activity
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

		// Token Launch Detection
		{
			Name: "New Token Launch",
			Signatures: []string{
				"TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA",
				"InitializeMint",
			},
			Threshold: 0.5,
			Category:  "Token",
		},

		// Flash Loan Detection
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

		// Large Transfer Detection
		{
			Name: "Whale Transfer",
			Signatures: []string{
				"Transfer",
				"amount: 1000000",
			},
			Threshold: 0.5,
			Category:  "Transfer",
		},

		// Failed Transaction Pattern
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

		// Pump.fun Token Creation
		{
			Name: "Pump.fun Launch",
			Signatures: []string{
				"6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P",
				"create",
			},
			Threshold: 0.5,
			Category:  "Meme",
		},
	}
}

// GetPatterns returns all loaded patterns
func (d *Detector) GetPatterns() []gpu.Pattern {
	return d.patterns
}

// AddCustomPattern allows adding user-defined patterns
func (d *Detector) AddCustomPattern(pattern gpu.Pattern) {
	d.patterns = append(d.patterns, pattern)
}

// GetPatternsByCategory filters patterns by category
func (d *Detector) GetPatternsByCategory(category string) []gpu.Pattern {
	filtered := make([]gpu.Pattern, 0)
	for _, p := range d.patterns {
		if p.Category == category {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

// RemovePattern removes a pattern by name
func (d *Detector) RemovePattern(name string) {
	newPatterns := make([]gpu.Pattern, 0)
	for _, p := range d.patterns {
		if p.Name != name {
			newPatterns = append(newPatterns, p)
		}
	}
	d.patterns = newPatterns
}
