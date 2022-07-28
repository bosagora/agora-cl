package agora

import "testing"

func TestAllocatedYearlyValidatorRewards(t *testing.T) {
	tests := []struct {
		name                string
		secondsSinceGenesis uint64
		want                uint64
	}{
		{"beginning of year 1", 1, 170_294_400},
		{"end of year 1", YearOfSecs - 1, 170_294_400},
		{"beginning of year 2", YearOfSecs + 1, 159_548_823},
		{"end of year 2", 2*YearOfSecs - 1, 159_548_823},
		{"year 3", 3*YearOfSecs - 1, 149_481_292},
		{"year 10", 10*YearOfSecs - 1, 94_719_523},
		{"year 50", 50*YearOfSecs - 1, 6_985_035},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AllocatedYearlyValidatorRewards(tt.secondsSinceGenesis, 1); got != tt.want {
				t.Errorf("AllocatedYearlyValidatorRewards() test for '%s' got %v but wanted %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestAllocatedValidatorRewardsPerEpoch(t *testing.T) {
	cfg := RewardConfig {
		SlotsPerEpoch: 32,
		SecondsPerSlot: 14,
		GweiPerBoa: 1000000000,
		EffectiveBalanceIncrement: 1000000000000,
	}

	tests := []struct {
		name                string
		secondsSinceGenesis uint64
		want                uint64
	}{
		{"beginning of year 1", 1, 2_419_200_000_000},
		{"end of year 1", YearOfSecs - 1, 2_419_200_000_000},
		{"beginning of year 2", YearOfSecs + 1, 2_266_548_480_000},
		{"beginning of year 3", 2*YearOfSecs + 1, 2_123_529_270_912},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AllocatedValidatorRewardsPerEpoch(tt.secondsSinceGenesis, cfg); got != tt.want {
				t.Errorf("AllocatedValidatorRewardsPerEpoch() test for '%s' got %v but wanted %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestValidatorRewardPerEpoch(t *testing.T) {
	cfg := RewardConfig {
		SlotsPerEpoch: 32,
		SecondsPerSlot: 14,
		GweiPerBoa: 1000000000,
		EffectiveBalanceIncrement: 1000000000000,
	}

	tests := []struct {
		name                string
		totalBalance		uint64
		effectiveBalance	uint64
		want                uint64
	}{
		{"all", cfg.EffectiveBalanceIncrement, cfg.EffectiveBalanceIncrement, 2_419_200_000_000},
		{"half", 2 * cfg.EffectiveBalanceIncrement, cfg.EffectiveBalanceIncrement, 1_209_600_000_000},
		{"one seventh", 7 * cfg.EffectiveBalanceIncrement, cfg.EffectiveBalanceIncrement, 345_600_000_000},
		{"one seventh, but has excess", 7 * cfg.EffectiveBalanceIncrement, cfg.EffectiveBalanceIncrement + 1, 345_600_000_000},
		{"below increment", 2 * cfg.EffectiveBalanceIncrement, cfg.EffectiveBalanceIncrement - 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidatorRewardPerEpoch(1, tt.totalBalance, tt.effectiveBalance, cfg); got != tt.want {
				t.Errorf("AllocatedValidatorRewardsPerEpoch() test for '%s' got %v but wanted %v", tt.name, got, tt.want)
			}
		})
	}
}
