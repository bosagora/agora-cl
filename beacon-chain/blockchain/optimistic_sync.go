package blockchain

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	types "github.com/prysmaticlabs/eth2-types"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/blocks"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/time"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/transition"
	"github.com/prysmaticlabs/prysm/beacon-chain/db/kv"
	v1 "github.com/prysmaticlabs/prysm/beacon-chain/powchain/engine-api-client/v1"
	"github.com/prysmaticlabs/prysm/beacon-chain/state"
	fieldparams "github.com/prysmaticlabs/prysm/config/fieldparams"
	"github.com/prysmaticlabs/prysm/config/params"
	"github.com/prysmaticlabs/prysm/encoding/bytesutil"
	enginev1 "github.com/prysmaticlabs/prysm/proto/engine/v1"
	ethpb "github.com/prysmaticlabs/prysm/proto/prysm/v1alpha1"
	"github.com/prysmaticlabs/prysm/proto/prysm/v1alpha1/block"
	"github.com/prysmaticlabs/prysm/time/slots"
	"github.com/sirupsen/logrus"
	"go.opencensus.io/trace"
)

// notifyForkchoiceUpdate signals execution engine the fork choice updates. Execution engine should:
// 1. Re-organizes the execution payload chain and corresponding state to make head_block_hash the head.
// 2. Applies finality to the execution state: it irreversibly persists the chain of all execution payloads and corresponding state, up to and including finalized_block_hash.
func (s *Service) notifyForkchoiceUpdate(ctx context.Context, headBlk block.BeaconBlock, headRoot [32]byte, finalizedRoot [32]byte) (*enginev1.PayloadIDBytes, error) {
	ctx, span := trace.StartSpan(ctx, "blockChain.notifyForkchoiceUpdate")
	defer span.End()

	if headBlk == nil || headBlk.IsNil() || headBlk.Body().IsNil() {
		return nil, errors.New("nil head block")
	}
	// Must not call fork choice updated until the transition conditions are met on the Pow network.
	isExecutionBlk, err := blocks.IsExecutionBlock(headBlk.Body())
	if err != nil {
		return nil, errors.Wrap(err, "could not determine if block is execution block")
	}
	if !isExecutionBlk {
		return nil, nil
	}
	headPayload, err := headBlk.Body().ExecutionPayload()
	if err != nil {
		return nil, errors.Wrap(err, "could not get execution payload")
	}
	finalizedBlock, err := s.cfg.BeaconDB.Block(ctx, s.ensureRootNotZeros(finalizedRoot))
	if err != nil {
		return nil, errors.Wrap(err, "could not get finalized block")
	}
	var finalizedHash []byte
	if blocks.IsPreBellatrixVersion(finalizedBlock.Block().Version()) {
		finalizedHash = params.BeaconConfig().ZeroHash[:]
	} else {
		payload, err := finalizedBlock.Block().Body().ExecutionPayload()
		if err != nil {
			return nil, errors.Wrap(err, "could not get finalized block execution payload")
		}
		finalizedHash = payload.BlockHash
	}

	fcs := &enginev1.ForkchoiceState{
		HeadBlockHash:      headPayload.BlockHash,
		SafeBlockHash:      headPayload.BlockHash,
		FinalizedBlockHash: finalizedHash,
	}

	nextSlot := s.CurrentSlot() + 1
	hasAttr, attr, vid, err := s.getPayloadAttribute(ctx, s.headState(ctx), nextSlot)
	if err != nil {
		return nil, errors.Wrap(err, "could not get payload attribute")
	}

	payloadID, _, err := s.cfg.ExecutionEngineCaller.ForkchoiceUpdated(ctx, fcs, attr)
	if err != nil {
		switch err {
		case v1.ErrAcceptedSyncingPayloadStatus:
			log.WithFields(logrus.Fields{
				"headSlot":      headBlk.Slot(),
				"headHash":      fmt.Sprintf("%#x", bytesutil.Trunc(headPayload.BlockHash)),
				"finalizedHash": fmt.Sprintf("%#x", bytesutil.Trunc(finalizedHash)),
			}).Info("Called fork choice updated with optimistic block")
			return payloadID, nil
		default:
			return nil, errors.Wrap(err, "could not notify forkchoice update from execution engine")
		}
	}
	if err := s.cfg.ForkChoiceStore.SetOptimisticToValid(ctx, headRoot); err != nil {
		return nil, errors.Wrap(err, "could not set block to valid")
	}
	if hasAttr {
		s.cfg.ProposerSlotIndexCache.SetProposerAndPayloadIDs(nextSlot, vid, bytesutil.BytesToUint64BigEndian(payloadID[:]))
	}
	return payloadID, nil
}

// notifyForkchoiceUpdate signals execution engine on a new payload
func (s *Service) notifyNewPayload(ctx context.Context, preStateVersion, postStateVersion int,
	preStateHeader, postStateHeader *ethpb.ExecutionPayloadHeader, blk block.SignedBeaconBlock, root [32]byte) error {
	ctx, span := trace.StartSpan(ctx, "blockChain.notifyNewPayload")
	defer span.End()

	// Execution payload is only supported in Bellatrix and beyond. Pre
	// merge blocks are never optimistic
	if blocks.IsPreBellatrixVersion(postStateVersion) {
		return s.cfg.ForkChoiceStore.SetOptimisticToValid(ctx, root)
	}
	if err := helpers.BeaconBlockIsNil(blk); err != nil {
		return err
	}
	body := blk.Block().Body()
	enabled, err := blocks.IsExecutionEnabledUsingHeader(postStateHeader, body)
	if err != nil {
		return errors.Wrap(err, "could not determine if execution is enabled")
	}
	if !enabled {
		return s.cfg.ForkChoiceStore.SetOptimisticToValid(ctx, root)
	}
	payload, err := body.ExecutionPayload()
	if err != nil {
		return errors.Wrap(err, "could not get execution payload")
	}
	_, err = s.cfg.ExecutionEngineCaller.NewPayload(ctx, payload)
	if err != nil {
		switch err {
		case v1.ErrAcceptedSyncingPayloadStatus:
			log.WithFields(logrus.Fields{
				"slot":      blk.Block().Slot(),
				"blockHash": fmt.Sprintf("%#x", bytesutil.Trunc(payload.BlockHash)),
			}).Info("Called new payload with optimistic block")
			return nil
		default:
			return errors.Wrap(err, "could not validate execution payload from execution engine")
		}
	}

	if err := s.cfg.ForkChoiceStore.SetOptimisticToValid(ctx, root); err != nil {
		return errors.Wrap(err, "could not set optimistic status")
	}

	// During the transition event, the transition block should be verified for sanity.
	if blocks.IsPreBellatrixVersion(preStateVersion) {
		// Handle case where pre-state is Altair but block contains payload.
		// To reach here, the block must have contained a valid payload.
		return s.validateMergeBlock(ctx, blk)
	}
	atTransition, err := blocks.IsMergeTransitionBlockUsingPayloadHeader(preStateHeader, body)
	if err != nil {
		return errors.Wrap(err, "could not check if merge block is terminal")
	}
	if !atTransition {
		return nil
	}
	return s.validateMergeBlock(ctx, blk)
}

// optimisticCandidateBlock returns true if this block can be optimistically synced.
//
// Spec pseudocode definition:
// def is_optimistic_candidate_block(opt_store: OptimisticStore, current_slot: Slot, block: BeaconBlock) -> bool:
//    if is_execution_block(opt_store.blocks[block.parent_root]):
//        return True
//
//    justified_root = opt_store.block_states[opt_store.head_block_root].current_justified_checkpoint.root
//    if is_execution_block(opt_store.blocks[justified_root]):
//        return True
//
//    if block.slot + SAFE_SLOTS_TO_IMPORT_OPTIMISTICALLY <= current_slot:
//        return True
//
//    return False
func (s *Service) optimisticCandidateBlock(ctx context.Context, blk block.BeaconBlock) (bool, error) {
	if blk.Slot()+params.BeaconConfig().SafeSlotsToImportOptimistically <= s.CurrentSlot() {
		return true, nil
	}

	parent, err := s.cfg.BeaconDB.Block(ctx, bytesutil.ToBytes32(blk.ParentRoot()))
	if err != nil {
		return false, err
	}
	if parent == nil {
		return false, errNilParentInDB
	}

	parentIsExecutionBlock, err := blocks.IsExecutionBlock(parent.Block().Body())
	if err != nil {
		return false, err
	}
	if parentIsExecutionBlock {
		return true, nil
	}

	j := s.store.JustifiedCheckpt()
	if j == nil {
		return false, errNilJustifiedInStore
	}
	jBlock, err := s.cfg.BeaconDB.Block(ctx, bytesutil.ToBytes32(j.Root))
	if err != nil {
		return false, err
	}
	return blocks.IsExecutionBlock(jBlock.Block().Body())
}

func (s *Service) getPayloadAttribute(ctx context.Context, st state.BeaconState, slot types.Slot) (bool, *enginev1.PayloadAttributes, types.ValidatorIndex, error) {
	vId, _, ok := s.cfg.ProposerSlotIndexCache.GetProposerPayloadIDs(slot)
	if !ok {
		return false, nil, 0, nil
	}
	st = st.Copy()
	st, err := transition.ProcessSlotsIfPossible(ctx, st, slot)
	if err != nil {
		return false, nil, 0, err
	}
	random, err := helpers.RandaoMix(st, time.CurrentEpoch(st))
	if err != nil {
		return false, nil, 0, nil
	}
	feeRecipient := params.BeaconConfig().DefaultFeeRecipient
	recipient, err := s.cfg.BeaconDB.FeeRecipientByValidatorID(ctx, vId)
	switch err == nil {
	case true:
		feeRecipient = recipient
	case errors.As(err, kv.ErrNotFoundFeeRecipient):
		if feeRecipient.String() == fieldparams.EthBurnAddressHex {
			logrus.WithFields(logrus.Fields{
				"validatorIndex": vId,
				"burnAddress":    fieldparams.EthBurnAddressHex,
			}).Error("Fee recipient not set. Using burn address")
		}
	default:
		return false, nil, 0, errors.Wrap(err, "could not get fee recipient in db")
	}
	t, err := slots.ToTime(uint64(s.genesisTime.Unix()), slot)
	if err != nil {
		return false, nil, 0, err
	}
	attr := &enginev1.PayloadAttributes{
		Timestamp:             uint64(t.Unix()),
		PrevRandao:            random,
		SuggestedFeeRecipient: feeRecipient.Bytes(),
	}
	return true, attr, vId, nil
}
