package conn

import (
	amino "github.com/tendermint/go-amino"
	cryptoAmino "github.com/ColorPlatform/prism/crypto/encoding/amino"
)

var cdc *amino.Codec = amino.NewCodec()

func init() {
	cryptoAmino.RegisterAmino(cdc)
	RegisterPacket(cdc)
}
