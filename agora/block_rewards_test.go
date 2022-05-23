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
			if got := AllocatedYearlyValidatorRewards(tt.secondsSinceGenesis); got != tt.want {
				t.Errorf("AllocatedYearlyValidatorRewards() test for '%s' got %v but wanted %v", tt.name, got, tt.want)
			}
		})
	}
}
