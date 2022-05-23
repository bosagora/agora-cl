package altair

import (
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/agora"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/beacon-chain/state"
	"github.com/prysmaticlabs/prysm/config/params"
	types "github.com/prysmaticlabs/prysm/consensus-types/primitives"
)

// BaseReward takes state and validator index and calculate
// individual validator's base reward.
//
// Spec code:
//  def get_base_reward(state: BeaconState, index: ValidatorIndex) -> Gwei:
//    """
//    Return the base reward for the validator defined by ``index`` with respect to the current ``state``.
//
//    Note: An optimally performing validator can earn one base reward per epoch over a long time horizon.
//    This takes into account both per-epoch (e.g. attestation) and intermittent duties (e.g. block proposal
//    and sync committees).
//    """
//    increments = state.validators[index].effective_balance // EFFECTIVE_BALANCE_INCREMENT
//    return Gwei(increments * get_base_reward_per_increment(state))
func BaseReward(s state.ReadOnlyBeaconState, index types.ValidatorIndex) (uint64, error) {
	totalBalance, err := helpers.TotalActiveBalance(s)
	if err != nil {
		return 0, errors.Wrap(err, "could not calculate active balance")
	}
	return BaseRewardWithTotalBalance(s, index, totalBalance)
}

// BaseRewardWithTotalBalance calculates the base reward with the provided total balance.
func BaseRewardWithTotalBalance(s state.ReadOnlyBeaconState, index types.ValidatorIndex, totalBalance uint64) (uint64, error) {
	val, err := s.ValidatorAtIndexReadOnly(index)
	if err != nil {
		return 0, err
	}
	cfg := params.BeaconConfig()
	increments := val.EffectiveBalance() / cfg.EffectiveBalanceIncrement
	baseRewardPerInc, err := BaseRewardPerIncrement(s, totalBalance)
	if err != nil {
		return 0, err
	}
	return increments * baseRewardPerInc, nil
}

// BaseRewardPerIncrement of the beacon state
//
// modified for Agora white paper:
//  return the increment so that for perfect behavior of all validators
//  the total allocation would be rewarded to the active validators
func BaseRewardPerIncrement(s state.ReadOnlyBeaconState, activeBalance uint64) (uint64, error) {
	if activeBalance == 0 {
		return 0, errors.New("active balance can't be 0")
	}
	cfg := params.BeaconConfig()

	// Calculate the Agora allocated validator rewards for this Epoch based on the year
	timeSinceGenesis, err := s.Slot().SafeMul(cfg.SecondsPerSlot)
	if err != nil {
		return 0, errors.Errorf("Could not calculate seconds since Genesis for slot %d", s.Slot())
	}
	allocatedRewardsPerSecond := agora.AllocatedYearlyValidatorRewards(uint64(timeSinceGenesis)) / agora.YearOfSecs
	epochAllocatedAgoraRewards := cfg.GweiPerEth * cfg.SecondsPerSlot * uint64(cfg.SlotsPerEpoch) * allocatedRewardsPerSecond

	// return the base reward per increment so base reward can be calculated as effective balance multiplied by this
	return cfg.EffectiveBalanceIncrement * epochAllocatedAgoraRewards / activeBalance, nil
}
