package dto

import "github.com/bze-alphateam/bze-scan/backend/internal/poolprice"

// PoolPrice is the price state of the pool behind an LP token: its reserves,
// price, the node's recent average and the drift between them (see
// poolprice.Pool for each field). Null for other denoms and when the pool
// cannot be read.
type PoolPrice = poolprice.Pool
