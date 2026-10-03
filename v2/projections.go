package v2

import "github.com/tigusigalpa/bitquery-go"

// EVMSource makes the top-level EVM dataset choices explicit for a pinned
// projection. Dataset and SelectBlocks use bitquery.Optional so callers can
// distinguish an omitted variable from an explicit GraphQL null. The SDK does
// not select a dataset or block branch on the caller's behalf.
//
// The values are intentionally open strings because Bitquery's schema evolves.
// Use values verified for the selected cube, such as "realtime", "archive",
// "combined", or a supported select_blocks enum value.
type EVMSource struct {
	Network      bitquery.Network
	Dataset      bitquery.Optional[string]
	SelectBlocks bitquery.Optional[string]
}

func (s EVMSource) variables() map[string]any {
	variables := map[string]any{"network": string(s.Network)}
	s.Dataset.SetVariable(variables, "dataset")
	s.SelectBlocks.SetVariable(variables, "selectBlocks")
	return variables
}

// EVMTransactionByHashRequest selects one EVM transaction by its native hash.
// Source controls dataset and block selection explicitly.
type EVMTransactionByHashRequest struct {
	Source EVMSource
	Hash   string
}

// EVMTransactionByHash returns the BQ1 transaction projection. It is a query
// factory only: it makes no request and does not decode provider responses.
func EVMTransactionByHash(request EVMTransactionByHashRequest) bitquery.Operation {
	variables := request.Source.variables()
	variables["hash"] = request.Hash
	return bitquery.Operation{
		OperationName: "BQ1Transaction",
		Query: `query BQ1Transaction($network: evm_network, $dataset: dataset_arg_enum, $selectBlocks: blocks_query_arg_enum, $hash: String!) {
  EVM(network: $network, dataset: $dataset, select_blocks: $selectBlocks) {
    Transactions(where: {Transaction: {Hash: {is: $hash}}}) {
      Block { Time Number }
      Transaction { From To Hash Value }
    }
  }
}`,
		Variables: variables,
	}
}

// EVMDEXTradesRequest selects a bounded EVM DEX-trade projection. Limit is
// required so the helper never introduces an implicit coverage window.
type EVMDEXTradesRequest struct {
	Source EVMSource
	Limit  int
}

// EVMDEXTrades returns the BQ2 EVM DEX-trade projection. It does not assume
// that a result order, cursor, or completeness guarantee exists.
func EVMDEXTrades(request EVMDEXTradesRequest) bitquery.Operation {
	variables := request.Source.variables()
	variables["limit"] = request.Limit
	return bitquery.Operation{
		OperationName: "BQ2DEXTrades",
		Query: `query BQ2DEXTrades($network: evm_network, $dataset: dataset_arg_enum, $selectBlocks: blocks_query_arg_enum, $limit: Int!) {
  EVM(network: $network, dataset: $dataset, select_blocks: $selectBlocks) {
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
		Variables: variables,
	}
}

// EVMBalancesRequest selects balances for one address. Address is required;
// callers choose the EVM dataset and block selection through Source.
type EVMBalancesRequest struct {
	Source  EVMSource
	Address string
}

// EVMBalances returns the BQ3 balances projection. Amount is selected with
// the documented non-zero selector; raw response bytes remain the source of
// truth for decimal representation and nullability.
func EVMBalances(request EVMBalancesRequest) bitquery.Operation {
	variables := request.Source.variables()
	variables["address"] = request.Address
	return bitquery.Operation{
		OperationName: "BQ3Balances",
		Query: `query BQ3Balances($network: evm_network, $dataset: dataset_arg_enum, $selectBlocks: blocks_query_arg_enum, $address: String!) {
  EVM(network: $network, dataset: $dataset, select_blocks: $selectBlocks) {
    Balances(where: {Balance: {Address: {is: $address}}}) {
      Currency { Symbol SmartContract }
      Balance { Amount(selectWhere: {gt: "0"}) AmountInUSD Address }
    }
  }
}`,
		Variables: variables,
	}
}

// EVMHoldersRequest selects bounded token-holder rows for one token contract.
type EVMHoldersRequest struct {
	Source        EVMSource
	SmartContract string
	Limit         int
}

// EVMHolders returns the BQ3 token-holder projection. It keeps the token
// contract, row limit, and EVM source selection as caller-owned inputs.
func EVMHolders(request EVMHoldersRequest) bitquery.Operation {
	variables := request.Source.variables()
	variables["smartContract"] = request.SmartContract
	variables["limit"] = request.Limit
	return bitquery.Operation{
		OperationName: "BQ3Holders",
		Query: `query BQ3Holders($network: evm_network, $dataset: dataset_arg_enum, $selectBlocks: blocks_query_arg_enum, $smartContract: String!, $limit: Int!) {
  EVM(network: $network, dataset: $dataset, select_blocks: $selectBlocks) {
    Holders(
      limit: {count: $limit}
      orderBy: {descending: Balance_Amount}
      where: {Currency: {SmartContract: {is: $smartContract}}}
    ) {
      Holder { Address }
      Balance { Amount(selectWhere: {gt: "0"}) AmountInUSD UpdateCount FirstChangeTime LastChangeTime }
      Currency { Symbol SmartContract }
    }
  }
}`,
		Variables: variables,
	}
}

// EVMTransactionBalancesRequest selects bounded transaction balance changes
// for one address.
type EVMTransactionBalancesRequest struct {
	Source  EVMSource
	Address string
	Limit   int
}

// EVMTransactionBalances returns the BQ3 balance-change projection. It does
// not promise archive availability, ordering stability, or finality for any
// network; those remain properties of the selected provider dataset.
func EVMTransactionBalances(request EVMTransactionBalancesRequest) bitquery.Operation {
	variables := request.Source.variables()
	variables["address"] = request.Address
	variables["limit"] = request.Limit
	return bitquery.Operation{
		OperationName: "BQ3TransactionBalances",
		Query: `query BQ3TransactionBalances($network: evm_network, $dataset: dataset_arg_enum, $selectBlocks: blocks_query_arg_enum, $address: String!, $limit: Int!) {
  EVM(network: $network, dataset: $dataset, select_blocks: $selectBlocks) {
    TransactionBalances(
      limit: {count: $limit}
      orderBy: {descending: Block_Time}
      where: {TokenBalance: {Address: {is: $address}}}
    ) {
      Block { Number Time }
      Transaction { Hash }
      TokenBalance {
        Address
        PreBalance
        PostBalance
        BalanceChangeReasonCode
        Currency { Symbol SmartContract Native }
      }
    }
  }
}`,
		Variables: variables,
	}
}

// EVMEventsRequest selects bounded decoded EVM event rows from one smart
// contract. The helper intentionally exposes no trigger or status parameter:
// neither is part of this pinned Events operation.
type EVMEventsRequest struct {
	Source        EVMSource
	SmartContract string
	Limit         int
}

// EVMEvents returns the BQ4 decoded-event projection. The selected union
// fragments mirror only the documented scalar forms and are not a claim that
// they cover every ABI value or nullability combination.
func EVMEvents(request EVMEventsRequest) bitquery.Operation {
	variables := request.Source.variables()
	variables["smartContract"] = request.SmartContract
	variables["limit"] = request.Limit
	return bitquery.Operation{
		OperationName: "BQ4DecodedEvents",
		Query: `query BQ4DecodedEvents($network: evm_network, $dataset: dataset_arg_enum, $selectBlocks: blocks_query_arg_enum, $smartContract: String!, $limit: Int!) {
  EVM(network: $network, dataset: $dataset, select_blocks: $selectBlocks) {
    Events(
      limit: {count: $limit}
      orderBy: {descending: Block_Time}
      where: {Log: {SmartContract: {is: $smartContract}}}
    ) {
      Block { Number Time }
      Transaction { Hash }
      Log { SmartContract Signature { Name } }
      Arguments {
        Name
        Value {
          ... on EVM_ABI_Integer_Value_Arg { integer }
          ... on EVM_ABI_String_Value_Arg { string }
          ... on EVM_ABI_Address_Value_Arg { address }
          ... on EVM_ABI_BigInt_Value_Arg { bigInteger }
          ... on EVM_ABI_Bytes_Value_Arg { hex }
          ... on EVM_ABI_Boolean_Value_Arg { bool }
        }
      }
    }
  }
}`,
		Variables: variables,
	}
}
