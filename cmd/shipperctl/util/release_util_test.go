package util

import (
	"reflect"
	"strings"
	"testing"
)

func TestGetFilteredScheduledClusters(t *testing.T) {
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

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			filteredClusters := FilterSelectedClusters(tt.ScheduledClusters, tt.DecommissionedClusters)
			if !reflect.DeepEqual(tt.ExpectedFilteredClusters, filteredClusters) {
				t.Fatalf(
					"expected filtered clusters %q got %q",
					strings.Join(tt.ExpectedFilteredClusters, ","),
					strings.Join(filteredClusters, ","))
			}
		})
	}
}
