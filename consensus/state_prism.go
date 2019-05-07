package consensus

import (
	"bytes"
	"fmt"
	"reflect"
	"runtime/debug"

	cmn "github.com/ColorPlatform/prism/libs/common"
	"github.com/ColorPlatform/prism/libs/fail"
	tmtime "github.com/ColorPlatform/prism/types/time"

	cfg "github.com/ColorPlatform/prism/config"
	cstypes "github.com/ColorPlatform/prism/consensus/types"
	tmevents "github.com/ColorPlatform/prism/libs/events"
	"github.com/ColorPlatform/prism/p2p"
	sm "github.com/ColorPlatform/prism/state"
	"github.com/ColorPlatform/prism/types"
	"github.com/ColorPlatform/prism/globals"
)

// NewConsensusState returns a new ConsensusState.
func NewConsensusStatePrism(
	config *cfg.ConsensusConfig,
	state sm.State,
	blockExec *sm.BlockExecutor,
	blockStore sm.BlockStore,
	txNotifier txNotifier,
	evpool evidencePool,
	options ...StateOption,
) *ConsensusState {
	cs := &ConsensusState{
		config:           config,
		blockExec:        blockExec,
		blockStore:       blockStore,
		txNotifier:       txNotifier,
		peerMsgQueue:     make(chan msgInfo, msgQueueSize),
		internalMsgQueue: make(chan msgInfo, msgQueueSize),
		timeoutTicker:    NewTimeoutTicker(),
		statsMsgQueue:    make(chan msgInfo, msgQueueSize),
		done:             make(chan struct{}),
		doWALCatchup:     true,
		wal:              nilWAL{},
		evpool:           evpool,
		evsw:             tmevents.NewEventSwitch(),
		metrics:          NopMetrics(),
	}
	// set function defaults (may be overwritten before calling Start)
	cs.decideProposal = cs.prismDecideProposal
	cs.doPrevote = cs.prismDoPrevote
	cs.setProposal = cs.prismSetProposal

	cs.updateToState(state)

	// Don't call scheduleRound0 yet.
	// We do that upon Start().
	cs.reconstructLastCommit(state)
	cs.BaseService = *cmn.NewBaseService(nil, "ConsensusState", cs)
	for _, option := range options {
		option(cs)
	}
	return cs
}

//----------------------------------------
// Public interface

// OnStart implements cmn.Service.
// It loads the latest state via the WAL, and starts the timeout and receive routines.
func (cs *ConsensusState) OnStartPrism() error {
	if err := cs.evsw.Start(); err != nil {
		return err
	}

	// we may set the WAL in testing before calling Start,
	// so only OpenWAL if its still the nilWAL
	if _, ok := cs.wal.(nilWAL); ok {
		walFile := cs.config.WalFile()
		wal, err := cs.OpenWAL(walFile)
		if err != nil {
			cs.Logger.Error("Error loading ConsensusState wal", "err", err.Error())
			return err
		}
		cs.wal = wal
	}

	// we need the timeoutRoutine for replay so
	// we don't block on the tick chan.
	// NOTE: we will get a build up of garbage go routines
	// firing on the tockChan until the receiveRoutine is started
	// to deal with them (by that point, at most one will be valid)
	if err := cs.timeoutTicker.Start(); err != nil {
		return err
	}

	// we may have lost some votes if the process crashed
	// reload from consensus log to catchup
	if cs.doWALCatchup {
		if err := cs.catchupReplay(cs.Height); err != nil {
			// don't try to recover from data corruption error
			if IsDataCorruptionError(err) {
				cs.Logger.Error("Encountered corrupt WAL file", "err", err.Error())
				cs.Logger.Error("Please repair the WAL file before restarting")
				fmt.Println(`You can attempt to repair the WAL as follows:

----
WALFILE=~/.tendermint/data/cs.wal/wal
cp $WALFILE ${WALFILE}.bak # backup the file
go run scripts/wal2json/main.go $WALFILE > wal.json # this will panic, but can be ignored
rm $WALFILE # remove the corrupt file
go run scripts/json2wal/main.go wal.json $WALFILE # rebuild the file without corruption
----`)

				return err
			}

			cs.Logger.Error("Error on catchup replay. Proceeding to start ConsensusState anyway", "err", err.Error())
			// NOTE: if we ever do return an error here,
			// make sure to stop the timeoutTicker
		}
	}

	// now start the receiveRoutine
	go cs.receiveRoutinePrism(0)

	// schedule the first round!
	// use GetRoundState so we don't race the receiveRoutine for access
	cs.scheduleRound0Prism(cs.GetRoundState())

	return nil
}

//------------------------------------------------------------
// internal functions for managing the state

// enterNewRound(height, 0) at cs.StartTime.
func (cs *ConsensusState) scheduleRound0Prism(rs *cstypes.RoundState) {
	//cs.Logger.Info("scheduleRound0", "now", tmtime.Now(), "startTime", cs.StartTime)
	sleepDuration := rs.StartTime.Sub(tmtime.Now())
	cs.scheduleTimeout(sleepDuration, rs.Height, 0, cstypes.RoundStepPrismNewHeight)
}

// Reconstruct LastCommit from SeenCommit, which we saved along with the block,
// (which happens even before saving the state)
func (cs *ConsensusState) reconstructLastCommitPrism(state sm.State) {
	if state.LastBlockHeight == 0 {
		return
	}
	seenCommit := cs.blockStore.LoadSeenCommit(state.LastBlockHeight)
	// TODO: fix types.PrecommitType
	lastPrecommits := types.NewVoteSet(state.ChainID, state.LastBlockHeight, seenCommit.Round(), types.PrecommitType, state.LastValidators)
	for _, precommit := range seenCommit.Precommits {
		if precommit == nil {
			continue
		}
		added, err := lastPrecommits.AddVote(seenCommit.ToVote(precommit))
		if !added || err != nil {
			cmn.PanicCrisis(fmt.Sprintf("Failed to reconstruct LastCommit: %v", err))
		}
	}
	if !lastPrecommits.HasTwoThirdsMajority() {
		cmn.PanicSanity("Failed to reconstruct LastCommit: Does not have +2/3 maj")
	}
	cs.LastCommit = lastPrecommits
}

// Updates ConsensusState and increments height to match that of state.
// The round becomes 0 and cs.Step becomes cstypes.RoundStepNewHeight.
func (cs *ConsensusState) updateToStatePrism(state sm.State) {
	if cs.CommitRound > -1 && 0 < cs.Height && cs.Height != state.LastBlockHeight {
		cmn.PanicSanity(fmt.Sprintf("updateToState() expected state height of %v but found %v",
			cs.Height, state.LastBlockHeight))
	}
	if !cs.state.IsEmpty() && cs.state.LastBlockHeight+1 != cs.Height {
		// This might happen when someone else is mutating cs.state.
		// Someone forgot to pass in state.Copy() somewhere?!
		cmn.PanicSanity(fmt.Sprintf("Inconsistent cs.state.LastBlockHeight+1 %v vs cs.Height %v",
			cs.state.LastBlockHeight+1, cs.Height))
	}

	// If state isn't further out than cs.state, just ignore.
	// This happens when SwitchToConsensus() is called in the reactor.
	// We don't want to reset e.g. the Votes, but we still want to
	// signal the new round step, because other services (eg. txNotifier)
	// depend on having an up-to-date peer state!
	if !cs.state.IsEmpty() && (state.LastBlockHeight <= cs.state.LastBlockHeight) {
		cs.Logger.Info("Ignoring updateToState()", "newHeight", state.LastBlockHeight+1, "oldHeight", cs.state.LastBlockHeight+1)
		cs.newStep()
		return
	}

	// Reset fields based on state.
	validators := state.Validators

	// TODO: fix lastPrecommits
	lastPrecommits := (*types.VoteSet)(nil)
	if cs.CommitRound > -1 && cs.Votes != nil {
		if !cs.Votes.Precommits(cs.CommitRound).HasTwoThirdsMajority() {
			cmn.PanicSanity("updateToState(state) called but last Precommit round didn't have +2/3")
		}
		lastPrecommits = cs.Votes.Precommits(cs.CommitRound)
	}

	// Next desired block height
	height := state.LastBlockHeight + 1

	// RoundState fields
	cs.updateHeight(height)
	cs.updateRoundStep(0, cstypes.RoundStepPrismNewHeight)
	if cs.CommitTime.IsZero() {
		// "Now" makes it easier to sync up dev nodes.
		// We add timeoutCommit to allow transactions
		// to be gathered for the first block.
		// And alternative solution that relies on clocks:
		//  cs.StartTime = state.LastBlockTime.Add(timeoutCommit)
		cs.StartTime = cs.config.Commit(tmtime.Now())
	} else {
		cs.StartTime = cs.config.Commit(cs.CommitTime)
	}

	cs.Validators = validators
	cs.Proposal = nil
	cs.ProposalBlock = nil
	cs.ProposalBlockParts = nil
	cs.LockedRound = -1
	cs.LockedBlock = nil
	cs.LockedBlockParts = nil
	cs.ValidRound = -1
	cs.ValidBlock = nil
	cs.ValidBlockParts = nil
	cs.Votes = cstypes.NewHeightVoteSet(state.ChainID, height, validators)
	cs.CommitRound = -1
	cs.LastCommit = lastPrecommits
	cs.LastValidators = state.LastValidators
	cs.TriggeredTimeoutPrecommit = false

	cs.state = state

	// Finally, broadcast RoundState
	cs.newStepPrism()
}

func (cs *ConsensusState) newStepPrism() {
	rs := cs.RoundStateEvent()
	cs.wal.Write(rs)
	cs.nSteps++
	// newStep is called by updateToState in NewConsensusState before the eventBus is set!
	if cs.eventBus != nil {
		cs.eventBus.PublishEventNewRoundStep(rs)
		cs.evsw.FireEvent(types.EventNewRoundStep, &cs.RoundState)
	}
}

//-----------------------------------------
// the main go routines

// receiveRoutine handles messages which may cause state transitions.
// it's argument (n) is the number of messages to process before exiting - use 0 to run forever
// It keeps the RoundState and is the only thing that updates it.
// Updates (state transitions) happen on timeouts, complete proposals, and 2/3 majorities.
// ConsensusState must be locked before any internal state is updated.
func (cs *ConsensusState) receiveRoutinePrism(maxSteps int) {
	onExit := func(cs *ConsensusState) {
		// NOTE: the internalMsgQueue may have signed messages from our
		// priv_val that haven't hit the WAL, but its ok because
		// priv_val tracks LastSig

		// close wal now that we're done writing to it
		cs.wal.Stop()
		cs.wal.Wait()

		close(cs.done)
	}

	defer func() {
		if r := recover(); r != nil {
			cs.Logger.Error("CONSENSUS FAILURE!!!", "err", r, "stack", string(debug.Stack()))
			// stop gracefully
			//
			// NOTE: We most probably shouldn't be running any further when there is
			// some unexpected panic. Some unknown error happened, and so we don't
			// know if that will result in the validator signing an invalid thing. It
			// might be worthwhile to explore a mechanism for manual resuming via
			// some console or secure RPC system, but for now, halting the chain upon
			// unexpected consensus bugs sounds like the better option.
			onExit(cs)
		}
	}()

	for {
		if maxSteps > 0 {
			if cs.nSteps >= maxSteps {
				cs.Logger.Info("reached max steps. exiting receive routine")
				cs.nSteps = 0
				return
			}
		}
		rs := cs.RoundState
		var mi msgInfo

		select {
		case <-cs.txNotifier.TxsAvailable():
			cs.handleTxsAvailablePrism()
		case mi = <-cs.peerMsgQueue:
			cs.wal.Write(mi)
			// handles proposals, block parts, votes
			// may generate internal events (votes, complete proposals, 2/3 majorities)
			cs.handleMsg(mi)
		case mi = <-cs.internalMsgQueue:
			cs.wal.WriteSync(mi) // NOTE: fsync

			if _, ok := mi.Msg.(*VoteMessage); ok {
				// we actually want to simulate failing during
				// the previous WriteSync, but this isn't easy to do.
				// Equivalent would be to fail here and manually remove
				// some bytes from the end of the wal.
				fail.Fail() // XXX
			}

			// handles proposals, block parts, votes
			cs.handleMsgPrism(mi)
		case ti := <-cs.timeoutTicker.Chan(): // tockChan:
			cs.wal.Write(ti)
			// if the timeout is relevant to the rs
			// go to the next step
			cs.handleTimeoutPrism(ti, rs)
		case <-cs.Quit():
			onExit(cs)
			return
		}
	}
}

// state transitions on complete-proposal, 2/3-any, 2/3-one
func (cs *ConsensusState) handleMsgPrism(mi msgInfo) {
	cs.mtx.Lock()
	defer cs.mtx.Unlock()

	var (
		added bool
		err   error
	)
	msg, peerID := mi.Msg, mi.PeerID
	switch msg := msg.(type) {
	case *ProposalMessage:
		// will not cause transition.
		// once proposal is set, we can receive block parts
		err = cs.setProposal(msg.Proposal)
	case *BlockPartMessage:
		// if the proposal is complete, we'll enterPrevote or tryFinalizeCommit
		added, err = cs.addProposalBlockPart(msg, peerID)
		if added {
			cs.statsMsgQueue <- mi
		}

		if err != nil && msg.Round != cs.Round {
			cs.Logger.Debug("Received block part from wrong round", "height", cs.Height, "csRound", cs.Round, "blockRound", msg.Round)
			err = nil
		}
	case *VoteMessage:
		// attempt to add the vote and dupeout the validator if its a duplicate signature
		// if the vote gives us a 2/3-any or 2/3-one, we transition
		added, err = cs.tryAddVote(msg.Vote, peerID)
		if added {
			cs.statsMsgQueue <- mi
		}

		if err == ErrAddingVote {
			// TODO: punish peer
			// We probably don't want to stop the peer here. The vote does not
			// necessarily comes from a malicious peer but can be just broadcasted by
			// a typical peer.
			// https://github.com/ColorPlatform/prism/issues/1281
		}

		// NOTE: the vote is broadcast to peers by the reactor listening
		// for vote events

		// TODO: If rs.Height == vote.Height && rs.Round < vote.Round,
		// the peer is sending us CatchupCommit precommits.
		// We could make note of this and help filter in broadcastHasVoteMessage().
	default:
		cs.Logger.Error("Unknown msg type", "type", reflect.TypeOf(msg))
		return
	}

	if err != nil {
		// Causes TestReactorValidatorSetChanges to timeout
		// https://github.com/ColorPlatform/prism/issues/3406
		// cs.Logger.Error("Error with msg", "height", cs.Height, "round", cs.Round,
		// 	"peer", peerID, "err", err, "msg", msg)
	}
}

func (cs *ConsensusState) isLeader() bool {
	return cs.Validators.IsLeagueLeader(cs.privValidator)
}

func (cs *ConsensusState) handleTimeoutPrism(ti timeoutInfo, rs cstypes.RoundState) {
	cs.Logger.Debug("Received tock", "timeout", ti.Duration, "height", ti.Height, "round", ti.Round, "step", ti.Step)

	// timeouts must be for current height, round, step
	if ti.Height != rs.Height || ti.Round < rs.Round || (ti.Round == rs.Round && ti.Step < rs.Step) {
		cs.Logger.Debug("Ignoring tock because we're ahead", "height", rs.Height, "round", rs.Round, "step", rs.Step)
		return
	}

	// the timeout will now cause a state transition
	cs.mtx.Lock()
	defer cs.mtx.Unlock()

	switch ti.Step {
	case cstypes.RoundStepPrismNewHeight:
		// NewRound event fired from enterNewRound.
		// XXX: should we fire timeout here (for timeout commit)?
		cs.enterNewRoundPrism(ti.Height, 0)
	case cstypes.RoundStepNewRound:
		cs.enterProposePrism(ti.Height, 0)
	case cstypes.RoundStepPrismPropose:
		cs.eventBus.PublishEventTimeoutPropose(cs.RoundStateEvent())
		cs.enterInLeaguePrevote(ti.Height, ti.Round)
	case cstypes.RoundStepInLeaguePrevoteWait:
		cs.eventBus.PublishEventTimeoutWait(cs.RoundStateEvent())
		if globals.UseLeagues() && cs.isLeader() {
			cs.enterInterLeaguePreVote(ti.Height, ti.Round)
		} else if globals.UseLeagues() {
			cs.enterLeagueCommit(ti.Height, ti.Round)
		} else {
			cs.enterPrecommit(ti.Height, ti.Round)
		}
	case cstypes.RoundStepInterLeaguePrecommitWait:
		cs.eventBus.PublishEventTimeoutWait(cs.RoundStateEvent())
		cs.enterInterLeaguePrecommit(ti.Height, ti.Round)
		cs.enterNewRound(ti.Height, ti.Round+1)
	case cstypes.RoundStepLeagueCommit:
		cs.eventBus.PublishEventTimeoutWait(cs.RoundStateEvent())
		cs.enterLeagueCommit(ti.Height, ti.Round)
		cs.enterNewRound(ti.Height, ti.Round+1)
	default:
		panic(fmt.Sprintf("Invalid timeout step: %v", ti.Step))
	}

}

func (cs *ConsensusState) handleTxsAvailablePrism() {
	cs.mtx.Lock()
	defer cs.mtx.Unlock()
	// we only need to do this for round 0
	cs.enterNewRoundPrism(cs.Height, 0)
	cs.enterProposePrism(cs.Height, 0)
}

//-----------------------------------------------------------------------------
// State functions
// Used internally by handleTimeout and handleMsg to make state transitions

// Enter: `timeoutNewHeight` by startTime (commitTime+timeoutCommit),
// 	or, if SkipTimeout==true, after receiving all precommits from (height,round-1)
// Enter: `timeoutPrecommits` after any +2/3 precommits from (height,round-1)
// Enter: +2/3 precommits for nil at (height,round-1)
// Enter: +2/3 prevotes any or +2/3 precommits for block or any from (height, round)
// NOTE: cs.StartTime was already set for height.
func (cs *ConsensusState) enterNewRoundPrism(height int64, round int) {
	logger := cs.Logger.With("height", height, "round", round)

	if cs.Height != height || round < cs.Round || (cs.Round == round && cs.Step != cstypes.RoundStepPrismNewHeight) {
		logger.Debug(fmt.Sprintf("enterNewRoundPrism(%v/%v): Invalid args. Current step: %v/%v/%v", height, round, cs.Height, cs.Round, cs.Step))
		return
	}

	if now := tmtime.Now(); cs.StartTime.After(now) {
		logger.Info("Need to set a buffer and log message here for sanity.", "startTime", cs.StartTime, "now", now)
	}

	logger.Info(fmt.Sprintf("enterNewRoundPrism(%v/%v). Current: %v/%v/%v", height, round, cs.Height, cs.Round, cs.Step))

	// Increment validators if necessary
	validators := cs.Validators
	if cs.Round < round {
		validators = validators.Copy()
		validators.IncrementProposerPriority(round - cs.Round)
	}

	// Setup new round
	// we don't fire newStep for this step,
	// but we fire an event, so update the round step first
	cs.updateRoundStep(round, cstypes.RoundStepPrismNewRound)
	cs.Validators = validators
	if round == 0 {
		// We've already reset these upon new height,
		// and meanwhile we might have received a proposal
		// for round 0.
	} else {
		logger.Info("Resetting Proposal info")
		cs.Proposal = nil
		cs.ProposalBlock = nil
		cs.ProposalBlockParts = nil
	}
	cs.Votes.SetRound(round + 1) // also track next round (round+1) to allow round-skipping
	cs.TriggeredTimeoutPrecommit = false

	cs.eventBus.PublishEventNewRound(cs.NewRoundEvent())
	cs.metrics.Rounds.Set(float64(round))

	// Wait for txs to be available in the mempool
	// before we enterPropose in round 0. If the last block changed the app hash,
	// we may need an empty "proof" block, and enterPropose immediately.
	waitForTxs := cs.config.WaitForTxs() && round == 0 && !cs.needProofBlock(height)
	if waitForTxs {
		if cs.config.CreateEmptyBlocksInterval > 0 {
			cs.scheduleTimeout(cs.config.CreateEmptyBlocksInterval, height, round,
				cstypes.RoundStepPrismNewRound)
		}
	} else {
		cs.enterProposePrism(height, round)
	}
}

// Enter (CreateEmptyBlocks): from enterNewRound(height,round)
// Enter (CreateEmptyBlocks, CreateEmptyBlocksInterval > 0 ): after enterNewRound(height,round), after timeout of CreateEmptyBlocksInterval
// Enter (!CreateEmptyBlocks) : after enterNewRound(height,round), once txs are in the mempool
func (cs *ConsensusState) enterProposePrism(height int64, round int) {
	logger := cs.Logger.With("height", height, "round", round)

	if cs.Height != height || round < cs.Round || (cs.Round == round && cstypes.RoundStepPropose <= cs.Step) {
		logger.Debug(fmt.Sprintf("enterProposePrism(%v/%v): Invalid args. Current step: %v/%v/%v", height, round, cs.Height, cs.Round, cs.Step))
		return
	}
	logger.Info(fmt.Sprintf("enterProposePrism(%v/%v). Current: %v/%v/%v", height, round, cs.Height, cs.Round, cs.Step))

	defer func() {
		// Done enterPropose:
		cs.updateRoundStep(round, cstypes.RoundStepPrismPropose)
		cs.newStep()

		// If we have the whole proposal + POL, then goto Prevote now.
		// else, we'll enterPrevote when the rest of the proposal is received (in AddProposalBlockPart),
		// or else after timeoutPropose
		if cs.isProposalComplete() {
			cs.enterInLeaguePrevote(height, cs.Round)
		}
	}()

	// If we don't get the proposal and all block parts quick enough, enterPrevote
	cs.scheduleTimeout(cs.config.Propose(round), height, round, cstypes.RoundStepPrismPropose)

	// Nothing more to do if we're not a validator
	if cs.privValidator == nil {
		logger.Debug("This node is not a validator")
		return
	}

	// if not a validator, we're done
	address := cs.privValidator.GetPubKey().Address()
	if !cs.Validators.HasAddress(address) {
		logger.Debug("This node is not a validator", "addr", address, "vals", cs.Validators)
		return
	}
	logger.Debug("This node is a validator")

	if cs.isProposer(address) {
		logger.Info("enterPropose: Our turn to propose", "proposer", cs.Validators.GetProposer().Address, "privValidator", cs.privValidator)
		cs.decideProposal(height, round)
	} else {
		logger.Info("enterPropose: Not our turn to propose", "proposer", cs.Validators.GetProposer().Address, "privValidator", cs.privValidator)
	}
}

func (cs *ConsensusState) prismDecideProposal(height int64, round int) {
	var block *types.Block
	var blockParts *types.PartSet

	// Decide on block
	if cs.ValidBlock != nil {
		// If there is valid block, choose that.
		block, blockParts = cs.ValidBlock, cs.ValidBlockParts
	} else {
		// Create a new proposal block from state/txs from the mempool.
		block, blockParts = cs.createProposalBlock()
		if block == nil { // on error
			return
		}
	}

	// Flush the WAL. Otherwise, we may not recompute the same proposal to sign, and the privValidator will refuse to sign anything.
	cs.wal.FlushAndSync()

	// Make proposal
	propBlockId := types.BlockID{Hash: block.Hash(), PartsHeader: blockParts.Header()}
	proposal := types.NewProposal(height, round, cs.ValidRound, propBlockId)
	if err := cs.privValidator.SignProposal(cs.state.ChainID, proposal); err == nil {

		// send proposal and block parts on internal msg queue
		cs.sendInternalMessage(msgInfo{&ProposalMessage{proposal}, ""})
		for i := 0; i < blockParts.Total(); i++ {
			part := blockParts.GetPart(i)
			cs.sendInternalMessage(msgInfo{&BlockPartMessage{cs.Height, cs.Round, part}, ""})
		}
		cs.Logger.Info("Signed proposal", "height", height, "round", round, "proposal", proposal)
		cs.Logger.Debug(fmt.Sprintf("Signed proposal block: %v", block))
	} else {
		if !cs.replayMode {
			cs.Logger.Error("enterProposePrism: Error signing proposal", "height", height, "round", round, "err", err)
		}
	}
}

// Enter: `timeoutPropose` after entering Propose.
// Enter: proposal block and POL is ready.
// Prevote for LockedBlock if we're locked, or ProposalBlock if valid.
// Otherwise vote nil.
func (cs *ConsensusState) enterInLeaguePrevote(height int64, round int) {
	if cs.Height != height || round < cs.Round || (cs.Round == round && cstypes.RoundStepInLeaguePrevote <= cs.Step) {
		cs.Logger.Debug(fmt.Sprintf("enterPrevote(%v/%v): Invalid args. Current step: %v/%v/%v", height, round, cs.Height, cs.Round, cs.Step))
		return
	}

	defer func() {
		// Done enterPrevote:
		cs.updateRoundStep(round, cstypes.RoundStepInLeaguePrevote)
		cs.newStep()
	}()

	cs.Logger.Info(fmt.Sprintf("enterInLeaguePrevote(%v/%v). Current: %v/%v/%v", height, round, cs.Height, cs.Round, cs.Step))

	// Sign and broadcast vote as necessary
	cs.doPrevote(height, round)

	// Once `addVote` hits any +2/3 prevotes, we will go to PrevoteWait
	// (so we have more time to try and collect +2/3 prevotes for a single block)
}

func (cs *ConsensusState) prismDoPrevote(height int64, round int) {
	logger := cs.Logger.With("height", height, "round", round)

	// If a block is locked, prevote that.
	if cs.LockedBlock != nil {
		logger.Info("enterPrevote: Block was locked")
		cs.signAddVote(types.PrevoteType, cs.LockedBlock.Hash(), cs.LockedBlockParts.Header())
		return
	}

	// If ProposalBlock is nil, prevote nil.
	if cs.ProposalBlock == nil {
		logger.Info("enterPrevote: ProposalBlock is nil")
		cs.signAddVote(types.PrevoteType, nil, types.PartSetHeader{})
		return
	}

	// Validate proposal block
	err := cs.blockExec.ValidateBlock(cs.state, cs.ProposalBlock)
	if err != nil {
		// ProposalBlock is invalid, prevote nil.
		logger.Error("enterPrevote: ProposalBlock is invalid", "err", err)
		cs.signAddVote(types.PrevoteType, nil, types.PartSetHeader{})
		return
	}

	// Prevote cs.ProposalBlock
	// NOTE: the proposal signature is validated when it is received,
	// and the proposal block parts are validated as they are received (against the merkle hash in the proposal)
	logger.Info("enterPrevote: ProposalBlock is valid")
	cs.signAddVote(types.PrevoteType, cs.ProposalBlock.Hash(), cs.ProposalBlockParts.Header())
}

// Enter: any +2/3 prevotes at next round.
func (cs *ConsensusState) enterInLeaguePrevoteWait(height int64, round int) {
	logger := cs.Logger.With("height", height, "round", round)

	if cs.Height != height || round < cs.Round || (cs.Round == round && cstypes.RoundStepInLeaguePrevoteWait <= cs.Step) {
		logger.Debug(fmt.Sprintf("enterInLeaguePrevoteWait(%v/%v): Invalid args. Current step: %v/%v/%v", height, round, cs.Height, cs.Round, cs.Step))
		return
	}
	if !cs.Votes.Prevotes(round).ThresholdPassed() {
		cmn.PanicSanity(fmt.Sprintf("enterInLeaguePrevoteWait(%v/%v), but Prevotes does not have any +2/3 votes", height, round))
	}
	logger.Info(fmt.Sprintf("enterInLeaguePrevoteWait(%v/%v). Current: %v/%v/%v", height, round, cs.Height, cs.Round, cs.Step))

	defer func() {
		// Done enterInLeaguePrevoteWait:
		cs.updateRoundStep(round, cstypes.RoundStepInLeaguePrevoteWait)
		cs.newStep()
	}()

	// Wait for some more prevotes; enterPrecommit
	cs.scheduleTimeout(cs.config.Prevote(round), height, round, cstypes.RoundStepInLeaguePrevoteWait)
}

func (cs *ConsensusState) enterInterLeaguePreVote(height int64, round int) {
	if cs.Height != height || round < cs.Round || (cs.Round == round && cstypes.RoundStepInLeaguePrevote <= cs.Step) || !cs.isLeader() {
		cs.Logger.Debug(fmt.Sprintf("enterPrevote(%v/%v): Invalid args. Current step: %v/%v/%v", height, round, cs.Height, cs.Round, cs.Step))
		return
	}

	defer func() {
		// Done enterPrevote:
		cs.updateRoundStep(round, cstypes.RoundStepInLeaguePrevote)
		cs.newStep()
	}()

	cs.Logger.Info(fmt.Sprintf("enterInLeaguePrevote(%v/%v). Current: %v/%v/%v", height, round, cs.Height, cs.Round, cs.Step))
	// cs.doInterLeaguePrevote()
	
	leagues := globals.GetLeagues()
	self, err := leagues.GetPeer(cs.privValidator.GetPubKey().Address())
	if err != nil {
		panic("Failed to find self")
	}
	league := self.League

	if league < 0 || league >= leagues.Leagues {
		panic(fmt.Sprintf("Unexpected league ID: %v", league))
	}
	leaguePeers := leagues.GetPeers(league)

	voteSet := cs.Votes.Prevotes(int(height))
	var votes []*types.Vote
	for _,peer := range(leaguePeers) {
		if vote := voteSet.GetByAddress(peer.PubKey.Address()); vote != nil {
			votes = append(votes, vote)
		}
	} 
	cs.Logger.Info(fmt.Sprintf("enterInterLeaguePrevote: vote list is ready: %v", votes))
	cs.signAddVoteList(types.LeaguePrevoteListType, cs.ProposalBlock.Hash(), cs.ProposalBlockParts.Header(), votes)
}

// Enter: `timeoutPrevote` after any +2/3 prevotes.
// Enter: `timeoutPrecommit` after any +2/3 precommits.
// Enter: +2/3 precomits for block or nil.
// Lock & precommit the ProposalBlock if we have enough prevotes for it (a POL in this round)
// else, unlock an existing lock and precommit nil if +2/3 of prevotes were nil,
// else, precommit nil otherwise.
func (cs *ConsensusState) enterInterLeaguePrecommit(height int64, round int) {
	logger := cs.Logger.With("height", height, "round", round)

	if cs.Height != height || round < cs.Round || (cs.Round == round && cstypes.RoundStepInterLeaguePrecommit <= cs.Step) {
		logger.Debug(fmt.Sprintf("enterPrecommit(%v/%v): Invalid args. Current step: %v/%v/%v", height, round, cs.Height, cs.Round, cs.Step))
		return
	}

	logger.Info(fmt.Sprintf("enterPrecommit(%v/%v). Current: %v/%v/%v", height, round, cs.Height, cs.Round, cs.Step))

	defer func() {
		// Done enterPrecommit:
		cs.updateRoundStep(round, cstypes.RoundStepInterLeaguePrecommit)
		cs.newStep()
	}()

	// check for a polka
	blockID, ok := cs.Votes.Prevotes(round).TwoThirdsMajority()

	// If we don't have a polka, we must precommit nil.
	if !ok {
		if cs.LockedBlock != nil {
			logger.Info("enterPrecommit: No +2/3 prevotes during enterPrecommit while we're locked. Precommitting nil")
		} else {
			logger.Info("enterPrecommit: No +2/3 prevotes during enterPrecommit. Precommitting nil.")
		}
		cs.signAddVote(types.PrecommitType, nil, types.PartSetHeader{})
		return
	}

	// At this point +2/3 prevoted for a particular block or nil.
	cs.eventBus.PublishEventPolka(cs.RoundStateEvent())

	// the latest POLRound should be this round.
	polRound, _ := cs.Votes.POLInfo()
	if polRound < round {
		cmn.PanicSanity(fmt.Sprintf("This POLRound should be %v but got %v", round, polRound))
	}

	// +2/3 prevoted nil. Unlock and precommit nil.
	if len(blockID.Hash) == 0 {
		if cs.LockedBlock == nil {
			logger.Info("enterPrecommit: +2/3 prevoted for nil.")
		} else {
			logger.Info("enterPrecommit: +2/3 prevoted for nil. Unlocking")
			cs.LockedRound = -1
			cs.LockedBlock = nil
			cs.LockedBlockParts = nil
			cs.eventBus.PublishEventUnlock(cs.RoundStateEvent())
		}
		cs.signAddVote(types.PrecommitType, nil, types.PartSetHeader{})
		return
	}

	// At this point, +2/3 prevoted for a particular block.

	// If we're already locked on that block, precommit it, and update the LockedRound
	if cs.LockedBlock.HashesTo(blockID.Hash) {
		logger.Info("enterPrecommit: +2/3 prevoted locked block. Relocking")
		cs.LockedRound = round
		cs.eventBus.PublishEventRelock(cs.RoundStateEvent())
		cs.signAddVote(types.PrecommitType, blockID.Hash, blockID.PartsHeader)
		return
	}

	// If +2/3 prevoted for proposal block, stage and precommit it
	if cs.ProposalBlock.HashesTo(blockID.Hash) {
		logger.Info("enterPrecommit: +2/3 prevoted proposal block. Locking", "hash", blockID.Hash)
		// Validate the block.
		if err := cs.blockExec.ValidateBlock(cs.state, cs.ProposalBlock); err != nil {
			cmn.PanicConsensus(fmt.Sprintf("enterPrecommit: +2/3 prevoted for an invalid block: %v", err))
		}
		cs.LockedRound = round
		cs.LockedBlock = cs.ProposalBlock
		cs.LockedBlockParts = cs.ProposalBlockParts
		cs.eventBus.PublishEventLock(cs.RoundStateEvent())
		cs.signAddVote(types.PrecommitType, blockID.Hash, blockID.PartsHeader)
		return
	}

	// There was a polka in this round for a block we don't have.
	// Fetch that block, unlock, and precommit nil.
	// The +2/3 prevotes for this round is the POL for our unlock.
	// TODO: In the future save the POL prevotes for justification.
	cs.LockedRound = -1
	cs.LockedBlock = nil
	cs.LockedBlockParts = nil
	if !cs.ProposalBlockParts.HasHeader(blockID.PartsHeader) {
		cs.ProposalBlock = nil
		cs.ProposalBlockParts = types.NewPartSetFromHeader(blockID.PartsHeader)
	}
	cs.eventBus.PublishEventUnlock(cs.RoundStateEvent())
	cs.signAddVote(types.PrecommitType, nil, types.PartSetHeader{})
}

// Enter: any +2/3 precommits for next round.
func (cs *ConsensusState) enterInLeaguePrecommitWait(height int64, round int) {
	logger := cs.Logger.With("height", height, "round", round)

	if cs.Height != height || round < cs.Round || (cs.Round == round && cs.TriggeredTimeoutPrecommit) {
		logger.Debug(
			fmt.Sprintf(
				"enterPrecommitWait(%v/%v): Invalid args. "+
					"Current state is Height/Round: %v/%v/, TriggeredTimeoutPrecommit:%v",
				height, round, cs.Height, cs.Round, cs.TriggeredTimeoutPrecommit))
		return
	}
	if !cs.Votes.Precommits(round).ThresholdPassed() {
		cmn.PanicSanity(fmt.Sprintf("enterPrecommitWait(%v/%v), but Precommits does not have any +2/3 votes", height, round))
	}
	logger.Info(fmt.Sprintf("enterPrecommitWait(%v/%v). Current: %v/%v/%v", height, round, cs.Height, cs.Round, cs.Step))

	defer func() {
		// Done enterPrecommitWait:
		cs.TriggeredTimeoutPrecommit = true
		cs.newStep()
	}()

	// Wait for some more precommits; enterNewRound
	cs.scheduleTimeout(cs.config.Precommit(round), height, round, cstypes.RoundStepInterLeaguePrecommitWait)

}

// Enter: +2/3 precommits for block
func (cs *ConsensusState) enterLeagueCommit(height int64, commitRound int) {
	logger := cs.Logger.With("height", height, "commitRound", commitRound)

	if cs.Height != height || cstypes.RoundStepLeagueCommit <= cs.Step {
		logger.Debug(fmt.Sprintf("enterCommit(%v/%v): Invalid args. Current step: %v/%v/%v", height, commitRound, cs.Height, cs.Round, cs.Step))
		return
	}
	logger.Info(fmt.Sprintf("enterCommit(%v/%v). Current: %v/%v/%v", height, commitRound, cs.Height, cs.Round, cs.Step))

	defer func() {
		// Done enterCommit:
		// keep cs.Round the same, commitRound points to the right Precommits set.
		cs.updateRoundStep(cs.Round, cstypes.RoundStepLeagueCommit)
		cs.CommitRound = commitRound
		cs.CommitTime = tmtime.Now()
		cs.newStep()

		// Maybe finalize immediately.
		cs.tryFinalizeCommit(height)
	}()

	blockID, ok := cs.Votes.Precommits(commitRound).TwoThirdsMajority()
	if !ok {
		cmn.PanicSanity("RunActionCommit() expects +2/3 precommits")
	}

	// The Locked* fields no longer matter.
	// Move them over to ProposalBlock if they match the commit hash,
	// otherwise they'll be cleared in updateToState.
	if cs.LockedBlock.HashesTo(blockID.Hash) {
		logger.Info("Commit is for locked block. Set ProposalBlock=LockedBlock", "blockHash", blockID.Hash)
		cs.ProposalBlock = cs.LockedBlock
		cs.ProposalBlockParts = cs.LockedBlockParts
	}

	// If we don't have the block being committed, set up to get it.
	if !cs.ProposalBlock.HashesTo(blockID.Hash) {
		if !cs.ProposalBlockParts.HasHeader(blockID.PartsHeader) {
			logger.Info("Commit is for a block we don't know about. Set ProposalBlock=nil", "proposal", cs.ProposalBlock.Hash(), "commit", blockID.Hash)
			// We're getting the wrong block.
			// Set up ProposalBlockParts and keep waiting.
			cs.ProposalBlock = nil
			cs.ProposalBlockParts = types.NewPartSetFromHeader(blockID.PartsHeader)
			cs.eventBus.PublishEventValidBlock(cs.RoundStateEvent())
			cs.evsw.FireEvent(types.EventValidBlock, &cs.RoundState)
		} else {
			// We just need to keep waiting.
		}
	}
}

// If we have the block AND +2/3 commits for it, finalize.
func (cs *ConsensusState) tryFinalizeCommitPrism(height int64) {
	logger := cs.Logger.With("height", height)

	if cs.Height != height {
		cmn.PanicSanity(fmt.Sprintf("tryFinalizeCommit() cs.Height: %v vs height: %v", cs.Height, height))
	}

	blockID, ok := cs.Votes.Precommits(cs.CommitRound).TwoThirdsMajority()
	if !ok || len(blockID.Hash) == 0 {
		logger.Error("Attempt to finalize failed. There was no +2/3 majority, or +2/3 was for <nil>.")
		return
	}
	if !cs.ProposalBlock.HashesTo(blockID.Hash) {
		// TODO: this happens every time if we're not a validator (ugly logs)
		// TODO: ^^ wait, why does it matter that we're a validator?
		logger.Info("Attempt to finalize failed. We don't have the commit block.", "proposal-block", cs.ProposalBlock.Hash(), "commit-block", blockID.Hash)
		return
	}

	//	go
	cs.finalizeCommitPrism(height)
}

// Increment height and goto cstypes.RoundStepNewHeight
func (cs *ConsensusState) finalizeCommitPrism(height int64) {
	if cs.Height != height || cs.Step != cstypes.RoundStepLeagueCommit {
		cs.Logger.Debug(fmt.Sprintf("finalizeCommit(%v): Invalid args. Current step: %v/%v/%v", height, cs.Height, cs.Round, cs.Step))
		return
	}

	blockID, ok := cs.Votes.Precommits(cs.CommitRound).TwoThirdsMajority()
	block, blockParts := cs.ProposalBlock, cs.ProposalBlockParts

	if !ok {
		cmn.PanicSanity(fmt.Sprintf("Cannot finalizeCommit, commit does not have two thirds majority"))
	}
	if !blockParts.HasHeader(blockID.PartsHeader) {
		cmn.PanicSanity(fmt.Sprintf("Expected ProposalBlockParts header to be commit header"))
	}
	if !block.HashesTo(blockID.Hash) {
		cmn.PanicSanity(fmt.Sprintf("Cannot finalizeCommit, ProposalBlock does not hash to commit hash"))
	}
	if err := cs.blockExec.ValidateBlock(cs.state, block); err != nil {
		cmn.PanicConsensus(fmt.Sprintf("+2/3 committed an invalid block: %v", err))
	}

	cs.Logger.Info(fmt.Sprintf("Finalizing commit of block with %d txs", block.NumTxs),
		"height", block.Height, "hash", block.Hash(), "root", block.AppHash)
	cs.Logger.Info(fmt.Sprintf("%v", block))

	fail.Fail() // XXX

	// Save to blockStore.
	if cs.blockStore.Height() < block.Height {
		// NOTE: the seenCommit is local justification to commit this block,
		// but may differ from the LastCommit included in the next block
		precommits := cs.Votes.Precommits(cs.CommitRound)
		seenCommit := precommits.MakeCommit()
		cs.blockStore.SaveBlock(block, blockParts, seenCommit)
	} else {
		// Happens during replay if we already saved the block but didn't commit
		cs.Logger.Info("Calling finalizeCommit on already stored block", "height", block.Height)
	}

	fail.Fail() // XXX

	// Write EndHeightMessage{} for this height, implying that the blockstore
	// has saved the block.
	//
	// If we crash before writing this EndHeightMessage{}, we will recover by
	// running ApplyBlock during the ABCI handshake when we restart.  If we
	// didn't save the block to the blockstore before writing
	// EndHeightMessage{}, we'd have to change WAL replay -- currently it
	// complains about replaying for heights where an #ENDHEIGHT entry already
	// exists.
	//
	// Either way, the ConsensusState should not be resumed until we
	// successfully call ApplyBlock (ie. later here, or in Handshake after
	// restart).
	cs.wal.WriteSync(EndHeightMessage{height}) // NOTE: fsync

	fail.Fail() // XXX

	// Create a copy of the state for staging and an event cache for txs.
	stateCopy := cs.state.Copy()

	// Execute and commit the block, update and save the state, and update the mempool.
	// NOTE The block.AppHash wont reflect these txs until the next block.
	var err error
	stateCopy, err = cs.blockExec.ApplyBlock(stateCopy, types.BlockID{Hash: block.Hash(), PartsHeader: blockParts.Header()}, block)
	if err != nil {
		cs.Logger.Error("Error on ApplyBlock. Did the application crash? Please restart tendermint", "err", err)
		err := cmn.Kill()
		if err != nil {
			cs.Logger.Error("Failed to kill this process - please do so manually", "err", err)
		}
		return
	}

	fail.Fail() // XXX

	// must be called before we update state
	cs.recordMetrics(height, block)

	// NewHeightStep!
	cs.updateToState(stateCopy)

	fail.Fail() // XXX

	// cs.StartTime is already set.
	// Schedule Round0 to start soon.
	cs.scheduleRound0(&cs.RoundState)

	// By here,
	// * cs.Height has been increment to height+1
	// * cs.Step is now cstypes.RoundStepNewHeight
	// * cs.StartTime is set to when we will start round0.
}


//-----------------------------------------------------------------------------

func (cs *ConsensusState) prismSetProposal(proposal *types.Proposal) error {
	// Already have one
	// TODO: possibly catch double proposals
	if cs.Proposal != nil {
		return nil
	}

	// Does not apply
	if proposal.Height != cs.Height || proposal.Round != cs.Round {
		return nil
	}

	// Verify POLRound, which must be -1 or in range [0, proposal.Round).
	if proposal.POLRound < -1 ||
		(proposal.POLRound >= 0 && proposal.POLRound >= proposal.Round) {
		return ErrInvalidProposalPOLRound
	}

	// Verify signature
	if !cs.Validators.GetProposer().PubKey.VerifyBytes(proposal.SignBytes(cs.state.ChainID), proposal.Signature) {
		return ErrInvalidProposalSignature
	}

	cs.Proposal = proposal
	// We don't update cs.ProposalBlockParts if it is already set.
	// This happens if we're already in cstypes.RoundStepCommit or if there is a valid block in the current round.
	// TODO: We can check if Proposal is for a different block as this is a sign of misbehavior!
	if cs.ProposalBlockParts == nil {
		cs.ProposalBlockParts = types.NewPartSetFromHeader(proposal.BlockID.PartsHeader)
	}
	cs.Logger.Info("Received proposal", "proposal", proposal)
	return nil
}

// NOTE: block is not necessarily valid.
// Asynchronously triggers either enterPrevote (before we timeout of propose) or tryFinalizeCommit, once we have the full block.
func (cs *ConsensusState) addProposalBlockPartPrism(msg *BlockPartMessage, peerID p2p.ID) (added bool, err error) {
	height, round, part := msg.Height, msg.Round, msg.Part

	// Blocks might be reused, so round mismatch is OK
	if cs.Height != height {
		cs.Logger.Debug("Received block part from wrong height", "height", height, "round", round)
		return false, nil
	}

	// We're not expecting a block part.
	if cs.ProposalBlockParts == nil {
		// NOTE: this can happen when we've gone to a higher round and
		// then receive parts from the previous round - not necessarily a bad peer.
		cs.Logger.Info("Received a block part when we're not expecting any",
			"height", height, "round", round, "index", part.Index, "peer", peerID)
		return false, nil
	}

	added, err = cs.ProposalBlockParts.AddPart(part)
	if err != nil {
		return added, err
	}
	if added && cs.ProposalBlockParts.IsComplete() {
		// Added and completed!
		_, err = cdc.UnmarshalBinaryLengthPrefixedReader(
			cs.ProposalBlockParts.GetReader(),
			&cs.ProposalBlock,
			int64(cs.state.ConsensusParams.Block.MaxBytes),
		)
		if err != nil {
			return added, err
		}
		// NOTE: it's possible to receive complete proposal blocks for future rounds without having the proposal
		cs.Logger.Info("Received complete proposal block", "height", cs.ProposalBlock.Height, "hash", cs.ProposalBlock.Hash())
		cs.eventBus.PublishEventCompleteProposal(cs.CompleteProposalEvent())

		// Update Valid* if we can.
		prevotes := cs.Votes.Prevotes(cs.Round)
		blockID, hasTwoThirds := prevotes.TwoThirdsMajority()
		if hasTwoThirds && !blockID.IsZero() && (cs.ValidRound < cs.Round) {
			if cs.ProposalBlock.HashesTo(blockID.Hash) {
				cs.Logger.Info("Updating valid block to new proposal block",
					"valid-round", cs.Round, "valid-block-hash", cs.ProposalBlock.Hash())
				cs.ValidRound = cs.Round
				cs.ValidBlock = cs.ProposalBlock
				cs.ValidBlockParts = cs.ProposalBlockParts
			}
			// TODO: In case there is +2/3 majority in Prevotes set for some
			// block and cs.ProposalBlock contains different block, either
			// proposer is faulty or voting power of faulty processes is more
			// than 1/3. We should trigger in the future accountability
			// procedure at this point.
		}

		if cs.Step <= cstypes.RoundStepPropose && cs.isProposalComplete() {
			// Move onto the next step
			cs.enterInLeaguePrevote(height, cs.Round)
			/* 
			if hasTwoThirds { // this is optimisation as this will be triggered when prevote is added
				cs.enterPrecommit(height, cs.Round)
			}
			*/
		} else if cs.Step == cstypes.RoundStepLeagueCommit {
			// If we're waiting on the proposal block...
			cs.tryFinalizeCommit(height)
		}
		return added, nil
	}
	return added, nil
}

// Attempt to add the vote. if its a duplicate signature, dupeout the validator
func (cs *ConsensusState) tryAddVotePrism(vote *types.Vote, peerID p2p.ID) (bool, error) {
	added, err := cs.addVotePrism(vote, peerID)
	if err != nil {
		// If the vote height is off, we'll just ignore it,
		// But if it's a conflicting sig, add it to the cs.evpool.
		// If it's otherwise invalid, punish peer.
		if err == ErrVoteHeightMismatch {
			return added, err
		} else if voteErr, ok := err.(*types.ErrVoteConflictingVotes); ok {
			addr := cs.privValidator.GetPubKey().Address()
			if bytes.Equal(vote.ValidatorAddress, addr) {
				cs.Logger.Error("Found conflicting vote from ourselves. Did you unsafe_reset a validator?", "height", vote.Height, "round", vote.Round, "type", vote.Type)
				return added, err
			}
			cs.evpool.AddEvidence(voteErr.DuplicateVoteEvidence)
			return added, err
		} else {
			// Probably an invalid signature / Bad peer.
			// Seems this can also err sometimes with "Unexpected step" - perhaps not from a bad peer ?
			cs.Logger.Error("Error attempting to add vote", "err", err)
			return added, ErrAddingVote
		}
	}
	return added, nil
}

//-----------------------------------------------------------------------------

func (cs *ConsensusState) addVotePrism(vote *types.Vote, peerID p2p.ID) (added bool, err error) {
	cs.Logger.Debug("addVote", "voteHeight", vote.Height, "voteType", vote.Type, "valIndex", vote.ValidatorIndex, "csHeight", cs.Height)

	// A precommit for the previous height?
	// These come in while we wait timeoutCommit
	if vote.Height+1 == cs.Height {
		if !(cs.Step == cstypes.RoundStepNewHeight && vote.Type == types.PrecommitType) {
			// TODO: give the reason ..
			// fmt.Errorf("tryAddVote: Wrong height, not a LastCommit straggler commit.")
			return added, ErrVoteHeightMismatch
		}
		added, err = cs.LastCommit.AddVote(vote)
		if !added {
			return added, err
		}

		cs.Logger.Info(fmt.Sprintf("Added to lastPrecommits: %v", cs.LastCommit.StringShort()))
		cs.eventBus.PublishEventVote(types.EventDataVote{Vote: vote})
		cs.evsw.FireEvent(types.EventVote, vote)

		// if we can skip timeoutCommit and have all the votes now,
		if cs.config.SkipTimeoutCommit && cs.LastCommit.HasAll() {
			// go straight to new round (skip timeout commit)
			// cs.scheduleTimeout(time.Duration(0), cs.Height, 0, cstypes.RoundStepNewHeight)
			cs.enterNewRoundPrism(cs.Height, 0)
		}

		return
	}

	// Height mismatch is ignored.
	// Not necessarily a bad peer, but not favourable behaviour.
	if vote.Height != cs.Height {
		err = ErrVoteHeightMismatch
		cs.Logger.Info("Vote ignored and not added", "voteHeight", vote.Height, "csHeight", cs.Height, "peerID", peerID)
		return
	}

	height := cs.Height
	added, err = cs.Votes.AddVote(vote, peerID)
	if !added {
		// Either duplicate, or error upon cs.Votes.AddByIndex()
		return
	}

	cs.eventBus.PublishEventVote(types.EventDataVote{Vote: vote})
	cs.evsw.FireEvent(types.EventVote, vote)

	switch vote.Type {
	case types.PrevoteType:
		prevotes := cs.Votes.Prevotes(vote.Round)
		cs.Logger.Info("Added to prevote", "vote", vote, "prevotes", prevotes.StringShort())

		// If +2/3 prevotes for a block or nil for *any* round:
		if blockID, ok := prevotes.TwoThirdsMajority(); ok {

			// There was a polka!
			// If we're locked but this is a recent polka, unlock.
			// If it matches our ProposalBlock, update the ValidBlock

			// Unlock if `cs.LockedRound < vote.Round <= cs.Round`
			// NOTE: If vote.Round > cs.Round, we'll deal with it when we get to vote.Round
			if (cs.LockedBlock != nil) &&
				(cs.LockedRound < vote.Round) &&
				(vote.Round <= cs.Round) &&
				!cs.LockedBlock.HashesTo(blockID.Hash) {

				cs.Logger.Info("Unlocking because of POL.", "lockedRound", cs.LockedRound, "POLRound", vote.Round)
				cs.LockedRound = -1
				cs.LockedBlock = nil
				cs.LockedBlockParts = nil
				cs.eventBus.PublishEventUnlock(cs.RoundStateEvent())
			}

			// Update Valid* if we can.
			// NOTE: our proposal block may be nil or not what received a polka..
			if len(blockID.Hash) != 0 && (cs.ValidRound < vote.Round) && (vote.Round == cs.Round) {

				if cs.ProposalBlock.HashesTo(blockID.Hash) {
					cs.Logger.Info(
						"Updating ValidBlock because of POL.", "validRound", cs.ValidRound, "POLRound", vote.Round)
					cs.ValidRound = vote.Round
					cs.ValidBlock = cs.ProposalBlock
					cs.ValidBlockParts = cs.ProposalBlockParts
				} else {
					cs.Logger.Info(
						"Valid block we don't know about. Set ProposalBlock=nil",
						"proposal", cs.ProposalBlock.Hash(), "blockId", blockID.Hash)
					// We're getting the wrong block.
					cs.ProposalBlock = nil
				}
				if !cs.ProposalBlockParts.HasHeader(blockID.PartsHeader) {
					cs.ProposalBlockParts = types.NewPartSetFromHeader(blockID.PartsHeader)
				}
				cs.evsw.FireEvent(types.EventValidBlock, &cs.RoundState)
				cs.eventBus.PublishEventValidBlock(cs.RoundStateEvent())
			}
		}

		// If +2/3 prevotes for *anything* for future round:
		if cs.Round < vote.Round && prevotes.ThresholdPassed() {
			// Round-skip if there is any 2/3+ of votes ahead of us
			cs.enterNewRoundPrism(height, vote.Round)
		} else if cs.Round == vote.Round && cstypes.RoundStepInLeaguePrevote <= cs.Step { // current round
			blockID, ok := prevotes.TwoThirdsMajority()
			if ok && (cs.isProposalComplete() || len(blockID.Hash) == 0) {
				cs.enterInterLeaguePrecommit(height, vote.Round)
			} else if prevotes.ThresholdPassed() {
				cs.enterInLeaguePrevoteWait(height, vote.Round)
			}
		} else if cs.Proposal != nil && 0 <= cs.Proposal.POLRound && cs.Proposal.POLRound == vote.Round {
			// If the proposal is now complete, enter prevote of cs.Round.
			if cs.isProposalComplete() {
				cs.enterInLeaguePrevote(height, cs.Round)
			}
		}

	case types.PrecommitType:
		precommits := cs.Votes.Precommits(vote.Round)
		cs.Logger.Info("Added to precommit", "vote", vote, "precommits", precommits.StringShort())

		blockID, ok := precommits.TwoThirdsMajority()
		if ok {
			// Executed as TwoThirdsMajority could be from a higher round
			cs.enterNewRoundPrism(height, vote.Round)
			cs.enterInterLeaguePrecommit(height, vote.Round)
			if len(blockID.Hash) != 0 {
				cs.enterLeagueCommit(height, vote.Round)
				if cs.config.SkipTimeoutCommit && precommits.HasAll() {
					cs.enterNewRoundPrism(cs.Height, 0)
				}
			} else {
				cs.enterPrecommitWait(height, vote.Round)
			}
		} else if cs.Round <= vote.Round && precommits.ThresholdPassed() {
			cs.enterNewRoundPrism(height, vote.Round)
			cs.enterPrecommitWait(height, vote.Round)
		}

	default:
		panic(fmt.Sprintf("Unexpected vote type %X", vote.Type)) // go-wire should prevent this.
	}

	return
}

func (cs *ConsensusState) signVoteList(type_ types.SignedMsgType, hash []byte, header types.PartSetHeader, votes []*types.Vote) (*types.VoteList, error) {
	// Flush the WAL. Otherwise, we may not recompute the same vote to sign, and the privValidator will refuse to sign anything.
	cs.wal.FlushAndSync()

	addr := cs.privValidator.GetPubKey().Address()
	valIndex, _ := cs.Validators.GetByAddress(addr)

	votesCopy := make([]types.Vote, len(votes))
	for i, vote := range(votes) {
		votesCopy[i] = *vote
	}

	voteList := &types.VoteList{
		ValidatorAddress: addr,
		ValidatorIndex:   valIndex,
		Height:           cs.Height,
		Round:            cs.Round,
		Timestamp:        cs.voteTime(),
		Type:             type_,
		BlockID:          types.BlockID{Hash: hash, PartsHeader: header},
		Votes:            votesCopy,
	}
	err := cs.privValidator.SignVoteList(cs.state.ChainID, voteList)
	return voteList, err
}

// sign the vote list and publish on internalMsgQueue
func (cs *ConsensusState) signAddVoteList(type_ types.SignedMsgType, hash []byte, header types.PartSetHeader, votes []*types.Vote) *types.VoteList {
	// if we don't have a key or we're not in the validator set, do nothing
	if cs.privValidator == nil || !cs.Validators.HasAddress(cs.privValidator.GetPubKey().Address()) {
		return nil
	}
	voteList, err := cs.signVoteList(type_, hash, header, votes)
	if err == nil {
		// cs.sendInternalMessage(msgInfo{&VoteListMessage{voteList}, ""})
		cs.Logger.Info("Signed and pushed vote", "height", cs.Height, "round", cs.Round, "voteList", voteList, "err", err)
		return voteList
	}
	//if !cs.replayMode {
	cs.Logger.Error("Error signing vote list", "height", cs.Height, "round", cs.Round, "voteList", voteList, "err", err)
	//}
	return nil
}

