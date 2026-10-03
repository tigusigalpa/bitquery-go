package v2

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/tigusigalpa/bitquery-go"
)

func TestPinnedProjectionOperations(t *testing.T) {
	source := EVMSource{
		Network:      bitquery.Network("evidence-net"),
		Dataset:      bitquery.Present("archive"),
		SelectBlocks: bitquery.Null[string](),
	}
	tests := []struct {
		name      string
		op        bitquery.Operation
		queryHash string
		wantVars  map[string]any
	}{
		{
			name:      "BQ1 transaction",
			op:        EVMTransactionByHash(EVMTransactionByHashRequest{Source: source, Hash: "0xtransaction"}),
			queryHash: "977f3495de925e3200aaf8cb61b898d04465ebadb9ebd58d81086f29b5e32519",
			wantVars:  map[string]any{"network": "evidence-net", "dataset": "archive", "selectBlocks": nil, "hash": "0xtransaction"},
		},
		{
			name:      "BQ2 DEX trades",
			op:        EVMDEXTrades(EVMDEXTradesRequest{Source: source, Limit: 0}),
			queryHash: "819e14d7a199f9761ad40ac7ce341ab97df5af3cccd1794ded8482c509444d48",
			wantVars:  map[string]any{"network": "evidence-net", "dataset": "archive", "selectBlocks": nil, "limit": 0},
		},
		{
			name:      "BQ3 balances",
			op:        EVMBalances(EVMBalancesRequest{Source: source, Address: "0xaddress"}),
			queryHash: "cf968d51bc91e8cad5a298abd48e74d522d01af64b18470e290ababf6539f079",
			wantVars:  map[string]any{"network": "evidence-net", "dataset": "archive", "selectBlocks": nil, "address": "0xaddress"},
		},
		{
			name:      "BQ3 holders",
			op:        EVMHolders(EVMHoldersRequest{Source: source, SmartContract: "0xtoken", Limit: 1}),
			queryHash: "b0182180e5a1263902fbcb1403e1e30365248e434ea79a32027ef7deb37e1c29",
			wantVars:  map[string]any{"network": "evidence-net", "dataset": "archive", "selectBlocks": nil, "smartContract": "0xtoken", "limit": 1},
		},
		{
			name:      "BQ3 transaction balances",
			op:        EVMTransactionBalances(EVMTransactionBalancesRequest{Source: source, Address: "0xaddress", Limit: 2}),
			queryHash: "e2ce4c6f0fa36598483163ad4b9b46cecf44ee9d744f35c2378e13ac3289e9dc",
			wantVars:  map[string]any{"network": "evidence-net", "dataset": "archive", "selectBlocks": nil, "address": "0xaddress", "limit": 2},
		},
		{
			name:      "BQ4 decoded events",
			op:        EVMEvents(EVMEventsRequest{Source: source, SmartContract: "0xtoken", Limit: 3}),
			queryHash: "bac9ab6ad6391b180b58ab0fe12cb477a71aaa75735d1027dbf8f51646144cc4",
			wantVars:  map[string]any{"network": "evidence-net", "dataset": "archive", "selectBlocks": nil, "smartContract": "0xtoken", "limit": 3},
		},
		{
			name:      "BQ5 Solana balance updates",
			op:        SolanaBalanceUpdates(4, "SolanaAccount"),
			queryHash: "3b1db552f9ddee8b2f4b3754c0ee9256ae6e6450316c7faf154cf4172d57083a",
			wantVars:  map[string]any{"limit": 4, "account": "SolanaAccount"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !reflect.DeepEqual(test.op.Variables, test.wantVars) {
				t.Fatalf("variables = %#v, want %#v", test.op.Variables, test.wantVars)
			}
			sum := sha256.Sum256([]byte(test.op.Query))
			if got := hex.EncodeToString(sum[:]); got != test.queryHash {
				t.Fatalf("exact query SHA-256 = %s", got)
			}

			operationJSON, err := json.Marshal(test.op)
			if err != nil {
				t.Fatal(err)
			}
			receipt := bitquery.NewReceipt(
				bitquery.ReceiptSourceHTTP,
				bitquery.ReceiptReceived,
				"https://fixture.test/graphql",
				operationJSON,
				[]byte("fixture-byte-sequence"),
				200,
				time.Date(2026, time.October, 3, 0, 0, 0, 0, time.UTC),
			)
			if got := receipt.Operation(); !bytes.Equal(got, operationJSON) {
				t.Fatalf("restored operation = %s, want %s", got, operationJSON)
			}
		})
	}
}

func TestEVMSourceLeavesDatasetAndBlockSelectionAbsent(t *testing.T) {
	op := EVMTransactionByHash(EVMTransactionByHashRequest{
		Source: EVMSource{Network: bitquery.NetworkEthereum},
		Hash:   "0xhash",
	})
	if _, ok := op.Variables["dataset"]; ok {
		t.Fatalf("dataset must be absent, got %#v", op.Variables["dataset"])
	}
	if _, ok := op.Variables["selectBlocks"]; ok {
		t.Fatalf("selectBlocks must be absent, got %#v", op.Variables["selectBlocks"])
	}
}
