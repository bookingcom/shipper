package replicas

import (
	"math"
)

// CalculateDesiredNumberOfReplicas extracts the optimal replica count for
// the given totalReplicaCount and desiredCapacityPercentage values.
//
// The returned value is a ceil'ed replica count calculated as a percentage
// of the given totalReplicaCount value. One of the considerations here is
// that the returned value will always return a replica count that is greater
// than the actual desired, and that is by design: we rather over than under
// capacity a deployment during a release process.
//
// Considering the following strategy: 0%, 25%, 50%, 75% and 100% of 3 total
// replicas; this method will yield, the respective number of desired replicas:
// 0, 1, 2, 3 and 3. Those values are based on where the desired percentage
// falls on the divisible slices of 3 replicas: 0%, 1%-33%, 34%-66% and
// 67%-100%.
func CalculateDesiredReplicaCount(totalReplicaCount, desiredCapacityPercentage int32) uint {
	desiredReplicaCount := math.Ceil(float64(totalReplicaCount) * float64(desiredCapacityPercentage) / 100)
	return uint(desiredReplicaCount)
}

// AchievedDesiredCapacity verifies whether the given currentReplicaCount
// and totalReplicaCount match the given desiredCapacityPercentage.
//
// Please note desiredPercentage might be a value between 0 and 100. In the
// case the informed desiredPercentage value is greater than 100, this
// function will panic; it is the caller's responsibility to check if the
// value falls in the 0-100 range.
func AchievedDesiredReplicaPercentage(totalReplicaCount, currentReplicaCount, desiredPercentage int32) bool {
	if desiredPercentage > 100 {
		panic("Programmer error: desiredPercentage should be a value between 0 and 100 inclusive")
	}

	return uint(currentReplicaCount) == CalculateDesiredReplicaCount(totalReplicaCount, desiredPercentage)
}
