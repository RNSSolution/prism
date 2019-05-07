package types

//-----------------------------------------------------------------------------
// RoundStepType enum type

// RoundStepType for Prism
const (
	RoundStepPrismNewHeight     = RoundStepType(0x01) // Wait til CommitTime + timeoutCommit
	RoundStepPrismNewRound      = RoundStepType(0x02) // Setup new round and go to RoundStepPrismPropose
	RoundStepPrismPropose       = RoundStepType(0x03) // Did propose, gossip proposal
	RoundStepInLeaguePrevote        = RoundStepType(0x04) // As league member prevoted, gossips prevotes
	RoundStepInLeaguePrevoteWait    = RoundStepType(0x05) // As League member received any +2/3 prevotes, starts timeout
	RoundStepInterLeaguePrevote     = RoundStepType(0x06) // As league leader communicates with leaders of other leagues
	RoundStepInterLeaguePrevoteWait = RoundStepType(0x07) // As league leader received +2/3 of votes, starts timeout

	RoundStepInterLeaguePrecommit     = RoundStepType(0x08) // As league leader did precommit, gossip precommits
	RoundStepInterLeaguePrecommitWait = RoundStepType(0x09) // As league leader received any +2/3 precommits, start timeout
	RoundStepLeagueCommit             = RoundStepType(0x0a) // Entered commit state machine
	// NOTE: RoundStepPrismNewHeight acts as RoundStepCommitWait.

	// NOTE: Update IsValid method if you change this!
)

// IsValid returns true if the step is valid, false if unknown/undefined.
func (rs RoundStepType) isValidPrism() bool {
	return uint8(rs) >= 0x01 && uint8(rs) <= 0x0A
}

// String returns a string
func (rs RoundStepType) stringPrism() string {
	switch rs {
	case RoundStepPrismNewHeight:
		return "RoundStepPrismNewHeight"
	case RoundStepPrismNewRound:
		return "RoundStepPrismNewRound"
	case RoundStepPrismPropose:
		return "RoundStepPrismPropose"
	case RoundStepInLeaguePrevote:
		return "RoundStepInLeaguePrevote"
	case RoundStepInLeaguePrevoteWait:
		return "RoundStepInLeaguePrevoteWait"
	case RoundStepInterLeaguePrevote:
		return "RoundStepInterLeaguePrevote"
	case RoundStepInterLeaguePrevoteWait:
		return "RoundStepInterLeaguePrevoteWait"
	case RoundStepInterLeaguePrecommit:
		return "RoundStepInterLeaguePrecommit"
	case RoundStepInterLeaguePrecommitWait:
		return "RoundStepInterLeaguePrecommitWait"
	case RoundStepLeagueCommit:
		return "RoundStepLeagueCommit"
	default:
		return "RoundStepUnknown" // Cannot panic.
	}
}


