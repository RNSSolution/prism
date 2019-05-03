package types

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/ColorPlatform/prism/crypto"
	cmn "github.com/ColorPlatform/prism/libs/common"
)

var (
	ErrVoteListUnexpectedStep            = errors.New("Unexpected step")
	ErrVoteListInvalidValidatorIndex     = errors.New("Invalid validator index")
	ErrVoteListInvalidValidatorAddress   = errors.New("Invalid validator address")
	ErrVoteListInvalidSignature          = errors.New("Invalid signature")
	ErrVoteListInvalidBlockHash          = errors.New("Invalid block hash")
	ErrVoteListNonDeterministicSignature = errors.New("Non-deterministic signature")
	ErrVoteListNil                       = errors.New("Nil vote")
)

/*
type ErrVoteConflictingVotes struct {
	*DuplicateVoteEvidence
}

func (err *ErrVoteConflictingVotes) Error() string {
	return fmt.Sprintf("Conflicting votes from validator %v", err.PubKey.Address())
}

func NewConflictingVoteError(val *Validator, voteA, voteB *Vote) *ErrVoteConflictingVotes {
	return &ErrVoteConflictingVotes{
		&DuplicateVoteEvidence{
			PubKey: val.PubKey,
			VoteA:  voteA,
			VoteB:  voteB,
		},
	}
}
*/

// VoteList represents a list of prevote, precommit, or commit votes from validators belonging to a specific set.
// It is signed by the leader of the corresponding league.
type VoteList struct {
	Type             SignedMsgType `json:"type"`
	Height           int64         `json:"height"`
	Round            int           `json:"round"`
	BlockID          BlockID       `json:"block_id"` // zero if vote is nil.
	Timestamp        time.Time     `json:"timestamp"`
	Votes            []Vote        `json:"votes"`
	ValidatorAddress Address       `json:"validator_address"`
	ValidatorIndex   int           `json:"validator_index"`
	Signature        []byte        `json:"signature"`
}

func (vl *VoteList) SignBytes(chainID string) []byte {
	bz, err := cdc.MarshalBinaryLengthPrefixed(CanonicalizeVoteList(chainID, vl))
	if err != nil {
		panic(err)
	}
	return bz
}

func (vl *VoteList) Copy() *VoteList {
	voteListCopy := *vl
	return &voteListCopy
}

func (vl *VoteList) String() string {
	if vl == nil {
		return "nil-VoteList"
	}
	var typeString string
	switch vl.Type {
	case LeaguePrevoteListType:
		typeString = "LeaguePrevoteListType"
	case LeadersPrecommitListType:
		typeString = "LeadersPrecommitListType"
	default:
		cmn.PanicSanity("Unknown vote list type")
	}

	return fmt.Sprintf("VoteList{%v:%X %v/%02d/%v(%v) %X %X @ %s: [%v]}",
		vl.ValidatorIndex,
		cmn.Fingerprint(vl.ValidatorAddress),
		vl.Height,
		vl.Round,
		vl.Type,
		typeString,
		cmn.Fingerprint(vl.BlockID.Hash),
		cmn.Fingerprint(vl.Signature),
		CanonicalTime(vl.Timestamp),
		vl.Votes,
	)
}

func (vl *VoteList) Verify(chainID string, pubKey crypto.PubKey) error {
	if !bytes.Equal(pubKey.Address(), vl.ValidatorAddress) {
		return ErrVoteListInvalidValidatorAddress
	}

	if !pubKey.VerifyBytes(vl.SignBytes(chainID), vl.Signature) {
		return ErrVoteListInvalidSignature
	}
	return nil
}

// ValidateBasic performs basic validation.
func (vl *VoteList) ValidateBasic() error {
	if !IsVoteListTypeValid(vl.Type) {
		return errors.New("Invalid Type")
	}
	if vl.Height < 0 {
		return errors.New("Negative Height")
	}
	if vl.Round < 0 {
		return errors.New("Negative Round")
	}

	// NOTE: Timestamp validation is subtle and handled elsewhere.

	if err := vl.BlockID.ValidateBasic(); err != nil {
		return fmt.Errorf("Wrong BlockID: %v", err)
	}
	// BlockID.ValidateBasic would not err if we for instance have an empty hash but a
	// non-empty PartsSetHeader:
	if !vl.BlockID.IsZero() && !vl.BlockID.IsComplete() {
		return fmt.Errorf("BlockID must be either empty or complete, got: %v", vl.BlockID)
	}
	if len(vl.ValidatorAddress) != crypto.AddressSize {
		return fmt.Errorf("Expected ValidatorAddress size to be %d bytes, got %d bytes",
			crypto.AddressSize,
			len(vl.ValidatorAddress),
		)
	}
	if vl.ValidatorIndex < 0 {
		return errors.New("Negative ValidatorIndex")
	}
	if len(vl.Signature) == 0 {
		return errors.New("Signature is missing")
	}
	if len(vl.Signature) > MaxSignatureSize {
		return fmt.Errorf("Signature is too big (max: %d)", MaxSignatureSize)
	}

	for i, vote := range(vl.Votes) {
		if vote.Type != (vl.Type - LeagueVoteListOffset) {
			return fmt.Errorf("Wrong vote type in Votes[%d], got: %v, expected %v", i, vl.Type - LeagueVoteListOffset, vote.Type)
		}
	}
	return nil
}
