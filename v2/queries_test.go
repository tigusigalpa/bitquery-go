package v2

import (
	"reflect"
	"strings"
	"testing"

	"github.com/tigusigalpa/bitquery-go"
)

func TestQueryFactoriesKeepValuesInVariables(t *testing.T) {
	const (
		currency = "0x1234567890abcdef"
		mint     = "So11111111111111111111111111111111111111112"
		account  = "ExampleAccount111111111111111111111111111111"
		program  = "ExampleProgram111111111111111111111111111111"
	)

	tests := []struct {
		name      string
		op        bitquery.Operation
		wantField string
		wantVars  map[string]any
		forbidden []string
	}{
		{
			name:      "EVM DEX trades",
			op:        EVMDexTrades(bitquery.NetworkBSC, 10, true),
			wantField: "DEXTrades",
			wantVars:  map[string]any{"network": "bsc", "limit": 10, "mempool": true},
			forbidden: []string{"bsc"},
		},
		{
			name:      "EVM transfers",
			op:        EVMTransfers(bitquery.NetworkEthereum, 20, currency),
			wantField: "Transfers",
			wantVars:  map[string]any{"network": "eth", "limit": 20, "currency": currency},
			forbidden: []string{"eth", currency},
		},
		{
			name:      "EVM transactions",
			op:        EVMTransactions(bitquery.NetworkArbitrum, 30, "0xsender"),
			wantField: "Transactions",
			wantVars:  map[string]any{"network": "arbitrum", "limit": 30, "from": "0xsender"},
			forbidden: []string{"arbitrum", "0xsender"},
		},
		{
			name:      "Solana DEX trades",
			op:        SolanaDexTrades(40),
			wantField: "DEXTrades",
			wantVars:  map[string]any{"limit": 40},
		},
		{
			name:      "Solana transfers",
			op:        SolanaTransfers(50, mint),
			wantField: "Transfers",
			wantVars:  map[string]any{"limit": 50, "mint": mint},
			forbidden: []string{mint},
		},
		{
			name:      "Solana balance updates",
			op:        SolanaBalanceUpdates(60, account),
			wantField: "BalanceUpdates",
			wantVars:  map[string]any{"limit": 60, "account": account},
			forbidden: []string{account},
		},
		{
			name:      "Solana instructions",
			op:        SolanaInstructions(70, program),
			wantField: "Instructions",
			wantVars:  map[string]any{"limit": 70, "program": program},
			forbidden: []string{program},
		},
		{
			name:      "Solana transactions",
			op:        SolanaTransactions(80, account),
			wantField: "Transactions",
			wantVars:  map[string]any{"limit": 80, "signer": account},
			forbidden: []string{account},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.op.Query, tt.wantField) {
				t.Fatalf("query does not select %q: %s", tt.wantField, tt.op.Query)
			}
			if !reflect.DeepEqual(tt.op.Variables, tt.wantVars) {
				t.Fatalf("variables = %#v, want %#v", tt.op.Variables, tt.wantVars)
			}
			for key := range tt.wantVars {
				if !strings.Contains(tt.op.Query, "$"+key) {
					t.Fatalf("query does not reference variable $%s", key)
				}
			}
			for _, value := range tt.forbidden {
				if strings.Contains(tt.op.Query, value) {
					t.Fatalf("query interpolates dynamic value %q", value)
				}
			}
		})
	}
}
