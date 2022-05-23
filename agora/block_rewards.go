package agora

import ()

// Global constants (start with a capital letter)
const (
	YearOfSecs uint64 = 365 * 24 * 60 * 60
)

// Package scope constants
const (
	firstYearValRewards uint64 = 27 * (YearOfSecs / 5)
)

// Agora rewards as defined in the white paper
//
// Allocated Validator rewards are 27 coins per 5 seconds for first year
//   then reduced by 6.31% every year
func AllocatedYearlyValidatorRewards(secondsSinceGenesis uint64) uint64 {
	yearsSinceGenesis := secondsSinceGenesis / YearOfSecs
	var yearlyReward uint64 = firstYearValRewards
	for y := yearsSinceGenesis; y > 0; y-- {
		yearlyReward = (yearlyReward * 9_369) / 10_000
	}
	return yearlyReward
}
