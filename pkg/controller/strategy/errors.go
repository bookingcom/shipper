package strategy

import (
	"fmt"
)

type NotWorkingOnStrategyError error

func IsNotWorkingOnStrategy(err error) bool {
	_, ok := err.(NotWorkingOnStrategyError)
	return ok
}

func NewNotWorkingOnStrategyError(contenderReleaseKey string) error {
	return NotWorkingOnStrategyError(fmt.Errorf(
		"Found %s as a contender, but it is not currently working on any strategy", contenderReleaseKey))
}

type RetrievingInstallationTargetForReleaseError error

func NewRetrievingInstallationTargetForReleaseError(releaseKey string, err error) RetrievingInstallationTargetForReleaseError {
	return RetrievingInstallationTargetForReleaseError(fmt.Errorf(
		"Error when retrieving installation target for release %s: %s", releaseKey, err))
}

type RetrievingCapacityTargetForReleaseError error

func NewRetrievingCapacityTargetForReleaseError(releasekey string, err error) RetrievingCapacityTargetForReleaseError {
	return RetrievingCapacityTargetForReleaseError(fmt.Errorf(
		"Error when retrieving capacity target for release %s: %s", releasekey, err))
}

type RetrievingTrafficTargetForReleaseError error

func NewRetrievingTrafficTargetForReleaseError(releaseKey string, err error) RetrievingTrafficTargetForReleaseError {
	return RetrievingTrafficTargetForReleaseError(fmt.Errorf(
		"Error when retrieving traffic target for release %s: %s", releaseKey, err))
}
