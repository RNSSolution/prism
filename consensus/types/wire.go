package types

import (
	amino "github.com/tendermint/go-amino"
	"github.com/ColorPlatform/prism/types"
)

var cdc = amino.NewCodec()

func init() {
	types.RegisterBlockAmino(cdc)
}
