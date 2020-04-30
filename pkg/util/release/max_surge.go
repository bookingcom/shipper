package release

import (
	"math"

	intstrutil "k8s.io/apimachinery/pkg/util/intstr"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func GetMaxSurgePercent(rollingUpdate *shipper.RollingUpdate, replicaCount int32) (int, error) {
	maxSurgeDefaultValue := intstrutil.FromString("100%")
	maxSurgeValue := intstrutil.FromString("100%")
	if rollingUpdate != nil {
		maxSurgeValue = *intstrutil.ValueOrDefault(&rollingUpdate.MaxSurge, maxSurgeValue)
	}

	surge, err := intstrutil.GetValueFromIntOrPercent(&maxSurgeValue, int(replicaCount), true)
	if err != nil {
		return 0, err
	}
	if surge <= 0 {
		// 0 or less are invalid values, using default
		surge, err = intstrutil.GetValueFromIntOrPercent(&maxSurgeDefaultValue, int(replicaCount), true)
		if err != nil {
			return 0, err
		}
	}
	surgePercent := math.Ceil((float64(surge) / float64(replicaCount)) * 100)
	// surgePercent must be between 1 - 100
	surgePercent = math.Min(surgePercent, 100)
	return int(surgePercent), nil
}
