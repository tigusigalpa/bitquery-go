package v2

import (
	"github.com/tigusigalpa/bitquery-go"
)

// Thin, documented V2 document factories: EVM + Solana common domains.
// These are conveniences — Execute() with a raw Operation stays the
// primary contract. All dynamic values go through variables.
//
// https://docs.bitquery.io/docs/graphql/query/
// https://docs.bitquery.io/docs/blockchain/Solana/

// --- EVM ---

// EVMDexTrades returns latest EVM DEX trades.
// https://docs.bitquery.io/docs/cubes/evm-dexpool/
func EVMDexTrades(network bitquery.Network, limit int, mempool bool) bitquery.Operation {
	return bitquery.Operation{
		Query: `query ($network: evm_network, $limit: Int!, $mempool: Boolean) {
  EVM(network: $network, mempool: $mempool) {
    DEXTrades(limit: {count: $limit}) {
      Block { Time Number }
      Transaction { Hash }
      Trade {
        Buy { Amount Currency { Symbol SmartContract } Buyer }
        Sell { Amount Currency { Symbol SmartContract } Buyer }
        Dex { ProtocolName ProtocolFamily SmartContract }
      }
    }
  }
}`,
		Variables: map[string]any{
			"network": string(network),
			"limit":   limit,
			"mempool": mempool,
		},
	}
}

// EVMTransfers returns EVM transfers, optionally filtered by currency.
func EVMTransfers(network bitquery.Network, limit int, currency string) bitquery.Operation {
	return bitquery.Operation{
		Query: `query ($network: evm_network, $limit: Int!, $currency: String) {
  EVM(network: $network) {
    Transfers(limit: {count: $limit}, where: {Transfer: {Currency: {SmartContract: {is: $currency}}}}) {
      Block { Time Number }
      Transaction { Hash }
      Transfer {
        Amount
        Currency { Symbol SmartContract }
        Sender
        Receiver
      }
    }
  }
}`,
		Variables: map[string]any{
			"network":  string(network),
			"limit":    limit,
			"currency": currency,
		},
	}
}

// EVMTransactions returns EVM transactions, optionally filtered by sender.
func EVMTransactions(network bitquery.Network, limit int, from string) bitquery.Operation {
	return bitquery.Operation{
		Query: `query ($network: evm_network, $limit: Int!, $from: String) {
  EVM(network: $network) {
    Transactions(limit: {count: $limit}, where: {Transaction: {From: {is: $from}}}) {
      Block { Time Number }
      Transaction {
        Hash
        From
        To
        Value
        Gas
        GasPrice
      }
    }
  }
}`,
		Variables: map[string]any{
			"network": string(network),
			"limit":   limit,
			"from":    from,
		},
	}
}

// --- Solana ---

// SolanaDexTrades returns latest Solana DEX trades.
// https://docs.bitquery.io/docs/blockchain/Solana/solana-dextrades/
func SolanaDexTrades(limit int) bitquery.Operation {
	return bitquery.Operation{
		Query: `query ($limit: Int!) {
  Solana {
    DEXTrades(limit: {count: $limit}) {
      Block { Slot Time }
      Transaction { Signature }
      Trade {
        Buy { Amount Currency { Symbol MintAddress } Account { Address } }
        Sell { Amount Currency { Symbol MintAddress } Account { Address } }
        Dex { ProtocolName ProtocolFamily ProgramAddress }
      }
    }
  }
}`,
		Variables: map[string]any{"limit": limit},
	}
}

// SolanaTransfers returns Solana transfers, optionally filtered by mint.
// https://docs.bitquery.io/docs/blockchain/Solana/solana-transfers/
func SolanaTransfers(limit int, mint string) bitquery.Operation {
	return bitquery.Operation{
		Query: `query ($limit: Int!, $mint: String) {
  Solana {
    Transfers(limit: {count: $limit}, where: {Transfer: {Currency: {MintAddress: {is: $mint}}}}) {
      Block { Slot Time }
      Transaction { Signature }
      Transfer {
        Amount
        Currency { Symbol MintAddress }
        Sender { Address }
        Receiver { Address }
      }
    }
  }
}`,
		Variables: map[string]any{"limit": limit, "mint": mint},
	}
}

// SolanaBalanceUpdates returns Solana balance updates for an account.
// https://docs.bitquery.io/docs/blockchain/Solana/solana-balance-updates/
func SolanaBalanceUpdates(limit int, account string) bitquery.Operation {
	return bitquery.Operation{
		Query: `query ($limit: Int!, $account: String) {
  Solana {
    BalanceUpdates(limit: {count: $limit}, where: {BalanceUpdate: {Account: {Address: {is: $account}}}}) {
      Block { Slot Time }
      Transaction { Signature }
      BalanceUpdate {
        Account { Address }
        Currency { Symbol MintAddress }
        PreBalance
        PostBalance
      }
    }
  }
}`,
		Variables: map[string]any{"limit": limit, "account": account},
	}
}

// SolanaInstructions returns Solana instructions, optionally by program.
// https://docs.bitquery.io/docs/blockchain/Solana/solana-instructions/
func SolanaInstructions(limit int, program string) bitquery.Operation {
	return bitquery.Operation{
		Query: `query ($limit: Int!, $program: String) {
  Solana {
    Instructions(limit: {count: $limit}, where: {Instruction: {Program: {Address: {is: $program}}}}) {
      Block { Slot Time }
      Transaction { Signature }
      Instruction {
        Program { Address Name MethodName }
        Accounts { Address }
      }
    }
  }
}`,
		Variables: map[string]any{"limit": limit, "program": program},
	}
}

// SolanaTransactions returns Solana transactions, optionally by signer.
// https://docs.bitquery.io/docs/blockchain/Solana/solana-transactions/
func SolanaTransactions(limit int, signer string) bitquery.Operation {
	return bitquery.Operation{
		Query: `query ($limit: Int!, $signer: String) {
  Solana {
    Transactions(limit: {count: $limit}, where: {Transaction: {Signer: {is: $signer}}}) {
      Block { Slot Time }
      Transaction {
        Signature
        Signer
        FeePayer
        Fee
        Result { Success }
      }
    }
  }
}`,
		Variables: map[string]any{"limit": limit, "signer": signer},
	}
}
