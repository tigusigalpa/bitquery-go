package v1

import (
	"github.com/tigusigalpa/bitquery-go"
)

// Thin, documented V1 document factories for common domains. These are
// conveniences — Execute() with a raw Operation stays the primary
// contract. All dynamic values go through variables.
//
// https://docs.bitquery.io/v1/docs/Schema/ethereum/overview

// Blocks returns recent blocks for a V1 network.
func Blocks(network bitquery.Network, limit int, dateFrom, dateTill string) bitquery.Operation {
	if dateFrom == "" && dateTill == "" {
		return bitquery.Operation{
			Query: `query ($network: EthereumNetwork!, $limit: Int!) {
  ethereum(network: $network) {
    blocks(options: {limit: $limit, desc: "height"}) {
      height
      timestamp { time }
      blockHash
      transactionCount
    }
  }
}`,
			Variables: map[string]any{
				"network": string(network),
				"limit":   limit,
			},
		}
	}
	return bitquery.Operation{
		Query: `query ($network: EthereumNetwork!, $limit: Int!, $from: ISO8601DateTime, $till: ISO8601DateTime) {
  ethereum(network: $network) {
    blocks(options: {limit: $limit, desc: "height"}, date: {since: $from, till: $till}) {
      height
      timestamp { time }
      blockHash
      transactionCount
    }
  }
}`,
		Variables: map[string]any{
			"network": string(network),
			"limit":   limit,
			"from":    dateFrom,
			"till":    dateTill,
		},
	}
}

// Transactions returns V1 transactions, optionally filtered by hash.
func Transactions(network bitquery.Network, limit int, txHash string) bitquery.Operation {
	return bitquery.Operation{
		Query: `query ($network: EthereumNetwork!, $limit: Int!, $hash: String) {
  ethereum(network: $network) {
    transactions(options: {limit: $limit, desc: "block.height"}, txHash: {is: $hash}) {
      hash
      block { height timestamp { time } }
      sender { address }
      to { address }
      value
      gasValue
      success
    }
  }
}`,
		Variables: map[string]any{
			"network": string(network),
			"limit":   limit,
			"hash":    txHash,
		},
	}
}

// Transfers returns V1 transfers, optionally filtered by address.
func Transfers(network bitquery.Network, limit int, address string) bitquery.Operation {
	return bitquery.Operation{
		Query: `query ($network: EthereumNetwork!, $limit: Int!, $address: String) {
  ethereum(network: $network) {
    transfers(options: {limit: $limit, desc: "block.height"}, any: [{sender: {is: $address}}, {receiver: {is: $address}}]) {
      block { height timestamp { time } }
      transaction { hash }
      sender { address }
      receiver { address }
      amount
      currency { symbol address }
    }
  }
}`,
		Variables: map[string]any{
			"network": string(network),
			"limit":   limit,
			"address": address,
		},
	}
}

// DexTrades returns V1 DEX trades, optionally filtered by exchange.
func DexTrades(network bitquery.Network, limit int, exchangeName string) bitquery.Operation {
	return bitquery.Operation{
		Query: `query ($network: EthereumNetwork!, $limit: Int!, $exchange: String) {
  ethereum(network: $network) {
    dexTrades(options: {limit: $limit, desc: "block.height"}, exchangeName: {is: $exchange}) {
      block { height timestamp { time } }
      transaction { hash }
      exchange { name }
      buyCurrency { symbol address }
      buyAmount
      sellCurrency { symbol address }
      sellAmount
    }
  }
}`,
		Variables: map[string]any{
			"network":  string(network),
			"limit":    limit,
			"exchange": exchangeName,
		},
	}
}

// SmartContractCalls returns V1 smart-contract calls.
func SmartContractCalls(network bitquery.Network, limit int, contract, dateFrom, dateTill string) bitquery.Operation {
	return bitquery.Operation{
		Query: `query ($network: EthereumNetwork!, $limit: Int!, $contract: String, $from: ISO8601DateTime, $till: ISO8601DateTime) {
  ethereum(network: $network) {
    smartContractCalls(options: {limit: $limit, desc: "block.height"}, smartContractAddress: {is: $contract}, date: {since: $from, till: $till}) {
      block { height timestamp { time } }
      transaction { hash }
      smartContract { address { address } }
      smartContractMethod { name signature }
      caller { address }
    }
  }
}`,
		Variables: map[string]any{
			"network":  string(network),
			"limit":    limit,
			"contract": contract,
			"from":     dateFrom,
			"till":     dateTill,
		},
	}
}
