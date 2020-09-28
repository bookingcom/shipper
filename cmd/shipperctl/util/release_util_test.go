package util

import (
	"reflect"
	"strings"
	"testing"
)

func TestFilterSelectedClusters(t *testing.T) {
	tests := []struct {
		Name                     string
		ScheduledClusters        []string
		DecommissionedClusters   []string
		ExpectedFilteredClusters []string
	}{
		{
			Name:                     "first decommissioned cluster",
			ScheduledClusters:        []string{"cluster-A", "cluster-B"},
			DecommissionedClusters:   []string{"cluster-A"},
			ExpectedFilteredClusters: []string{"cluster-B"},
		},
		{
			Name:                     "second decommissioned cluster",
			ScheduledClusters:        []string{"cluster-A", "cluster-B"},
			DecommissionedClusters:   []string{"cluster-B"},
			ExpectedFilteredClusters: []string{"cluster-A"},
		},
		{
			Name:                     "all decommissioned cluster",
			ScheduledClusters:        []string{"cluster-A", "cluster-B"},
			DecommissionedClusters:   []string{"cluster-A", "cluster-B"},
			ExpectedFilteredClusters: nil,
		},
		{
			Name:                     "all decommissioned cluster reversed order",
			ScheduledClusters:        []string{"cluster-A", "cluster-B"},
			DecommissionedClusters:   []string{"cluster-B", "cluster-A"},
			ExpectedFilteredClusters: nil,
		},
		{
			Name:                     "nil decommissioned cluster",
			ScheduledClusters:        []string{"cluster-A", "cluster-B"},
			DecommissionedClusters:   nil,
			ExpectedFilteredClusters: []string{"cluster-A", "cluster-B"},
		},
		{
			Name:                     "empty decommissioned cluster",
			ScheduledClusters:        []string{"cluster-A", "cluster-B"},
			DecommissionedClusters:   []string{},
			ExpectedFilteredClusters: []string{"cluster-A", "cluster-B"},
		},
		{
			Name:                     "different decommissioned cluster",
			ScheduledClusters:        []string{"cluster-A", "cluster-B"},
			DecommissionedClusters:   []string{"cluster-C", "cluster-D"},
			ExpectedFilteredClusters: []string{"cluster-A", "cluster-B"},
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			filteredClusters := FilterSelectedClusters(test.ScheduledClusters, test.DecommissionedClusters)
			if !reflect.DeepEqual(test.ExpectedFilteredClusters, filteredClusters) {
				t.Fatalf(
					"expected filtered clusters %q got %q",
					strings.Join(test.ExpectedFilteredClusters, ","),
					strings.Join(filteredClusters, ","))
			}
		})
	}
}
