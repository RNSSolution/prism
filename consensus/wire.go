package consensus

import (
	amino "github.com/tendermint/go-amino"
	"github.com/ColorPlatform/prism/types"
)

var cdc = amino.NewCodec()

func init() {
	RegisterConsensusMessages(cdc)
	RegisterWALMessages(cdc)
	types.RegisterBlockAmino(cdc)
}
