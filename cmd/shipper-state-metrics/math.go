package main

import (
	"errors"
	"math"
	"sort"
)

// Copyright (c) 2014-2015 Montana Flynn (https://anonfunction.com)
// Licensed under the MIT License (check NOTICE for full license)
// Obtained from https://github.com/montanaflynn/stats, minor modifications
// were applied
func Percentile(input []float64, percent float64) (percentile float64, err error) {
	// Find the length of items in the slice
	il := len(input)

	// Return an error for empty slices
	if il == 0 {
		return 0, errors.New("empty input")
	}

	// Return error for less than 0 or greater than 100 percentages
	if percent < 0 || percent > 100 {
		return 0, errors.New("percent out of bounds")
	}

	// Sort the data
	sort.Float64s(input)

	// Return the last item
	if percent == 100.0 {
		return input[il-1], nil
	}

	// Find ordinal ranking
	or := int(math.Ceil(float64(il) * percent / 100))

	// Return the item that is in the place of the ordinal rank
	if or == 0 {
		return input[0], nil
	}
	return input[or-1], nil

}

func MakeSummary(input []float64, quantiles []float64) (map[float64]float64, error) {
	summary := make(map[float64]float64)

	for _, p := range quantiles {
		percentile, err := Percentile(input, p*100)
		summary[p] = percentile
		if err != nil {
			return nil, err
		}
	}

	return summary, nil
}

func Sum(input []float64) float64 {
	sum := 0.0

	for _, v := range input {
		sum = sum + v
	}

	return sum
}
