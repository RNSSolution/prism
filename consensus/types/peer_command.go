package types

type PeerCommand byte

type PeerCommandChannel chan PeerCommand

const (
	HasNewVote        PeerCommand = 0x01
	HasNewPrecommit   PeerCommand = 0x02
	HasNewBlockHeader PeerCommand = 0x03
	HasNewBlockPart   PeerCommand = 0x04
)