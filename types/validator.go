package types

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/ColorPlatform/prism/crypto"
	cmn "github.com/ColorPlatform/prism/libs/common"
)

// Volatile state for each Validator
// NOTE: The ProposerPriority is not included in Validator.Hash();
// make sure to update that method if changes are made here
type Validator struct {
	League      int           `json:"league"`
	NodeId      int           `json:"node_id"`
	Address     Address       `json:"address"`
	PubKey      crypto.PubKey `json:"pub_key"`
	VotingPower int64         `json:"voting_power"`

	ProposerPriority int64    `json:"proposer_priority"`
}

func NewValidator(pubKey crypto.PubKey, votingPower int64, league int, nodeId int) *Validator {
	return &Validator{
		League:           league,
		NodeId:           nodeId,
		Address:          pubKey.Address(),
		PubKey:           pubKey,
		VotingPower:      votingPower,
		ProposerPriority: 0,
	}
}

// Creates a new copy of the validator so we can mutate ProposerPriority.
// Panics if the validator is nil.
func (v *Validator) Copy() *Validator {
	vCopy := *v
	return &vCopy
}

// Returns the one with higher ProposerPriority.
func (v *Validator) CompareProposerPriority(other *Validator) *Validator {
	if v == nil {
		return other
	}
	if v.ProposerPriority > other.ProposerPriority {
		return v
	} else if v.ProposerPriority < other.ProposerPriority {
		return other
	} else {
		result := bytes.Compare(v.Address, other.Address)
		if result < 0 {
			return v
		} else if result > 0 {
			return other
		} else {
			cmn.PanicSanity("Cannot compare identical validators")
			return nil
		}
	}
}

func (v *Validator) String() string {
	if v == nil {
		return "nil-Validator"
	}
	return fmt.Sprintf("Validator{%v %v VP:%v A:%v L:(%v@%v)}",
		v.Address,
		v.PubKey,
		v.VotingPower,
		v.ProposerPriority,
		v.NodeId,
		v.League,
	)
}

// ValidatorListString returns a prettified validator list for logging purposes.
func ValidatorListString(vals []*Validator) string {
	chunks := make([]string, len(vals))
	for i, val := range vals {
		chunks[i] = fmt.Sprintf("%s:%d", val.Address, val.VotingPower)
	}

	return strings.Join(chunks, ",")
}

// Bytes computes the unique encoding of a validator with a given voting power.
// These are the bytes that gets hashed in consensus. It excludes address
// as its redundant with the pubkey. This also excludes ProposerPriority
// which changes every round.
func (v *Validator) Bytes() []byte {
	return cdcEncode(struct {
		PubKey      crypto.PubKey
		VotingPower int64
	}{
		v.PubKey,
		v.VotingPower,
	})
}

//----------------------------------------
// RandValidator

// RandValidator returns a randomized validator, useful for testing.
// UNSTABLE
func RandValidator(randPower bool, minPower int64) (*Validator, PrivValidator) {
	privVal := NewMockPV()
	votePower := minPower
	if randPower {
		votePower += int64(cmn.RandUint32())
	}
	pubKey := privVal.GetPubKey()
	// TODO: add new validator to the current league
	val := NewValidator(pubKey, votePower, InvalidLeague, InvalidNodeId)
	return val, privVal
}
