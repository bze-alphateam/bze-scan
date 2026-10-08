package dto

import "encoding/json"

// RawTx is the body of GET /api/v1/raw/tx/{hash}: the transaction as the node
// encodes it, sliced from the block and its results.
type RawTx struct {
	Height int64 `json:"height"`
	Index  int64 `json:"index"`
	// Tx is the base64 raw transaction, /block data.txs[index].
	Tx string `json:"tx"`
	// TxResult is /block_results txs_results[index], verbatim.
	TxResult json.RawMessage `json:"tx_result"`
}
