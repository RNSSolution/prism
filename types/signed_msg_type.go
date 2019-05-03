package types

// SignedMsgType is a type of signed message in the consensus.
type SignedMsgType byte

const (
	// Votes
	PrevoteType   SignedMsgType = 0x01
	PrecommitType SignedMsgType = 0x02

	// Leagues voting
	LeagueVoteListOffset      SignedMsgType = 0x80
	// A set of votes frm a specific league
	LeaguePrevoteListType    SignedMsgType = 0x81
	// A set of votes from league leaders
	LeadersPrecommitListType SignedMsgType = 0x82

	// Proposals
	ProposalType SignedMsgType = 0x20
)

// IsVoteTypeValid returns true if t is a valid vote type.
func IsVoteTypeValid(t SignedMsgType) bool {
	switch t {
	case PrevoteType, PrecommitType:
		return true
	default:
		return false
	}
}

// IsVoteListTypeValid returns true if t is a valid vote list type.
func IsVoteListTypeValid(t SignedMsgType) bool {
	switch t {
	case LeaguePrevoteListType, LeadersPrecommitListType:
		return true
	default:
		return false
	}
}
