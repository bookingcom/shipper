package state

func MakeHistogram(input []float64, buckets []float64) map[float64]uint64 {
	histogram := make(map[float64]uint64)

	for _, value := range input {
		for _, bucket := range buckets {
			// NOTE(jgreff): yes, this leaves out values in the
			// +Inf bucket. Prometheus doesn't put them in any
			// buckets, but infers it from the total count of
			// observations, minus the observations in all the
			// other buckets.
			if value < bucket {
				histogram[bucket]++
				break
			}
		}
	}

	return histogram
}

func Sum(input []float64) float64 {
	sum := 0.0

	for _, v := range input {
		sum = sum + v
	}

	return sum
}
