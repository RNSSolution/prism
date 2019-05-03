package blockchain

import (
	amino "github.com/tendermint/go-amino"
	"github.com/ColorPlatform/prism/types"
)

var cdc = amino.NewCodec()

func init() {
	RegisterBlockchainMessages(cdc)
	types.RegisterBlockAmino(cdc)
}
