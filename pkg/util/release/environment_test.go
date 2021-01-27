package release

import (
	"testing"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func TestIsReleaseSteppingBackwards(t *testing.T) {
	var tests = []struct {
		title        string
		achievedStep *shipper.AchievedStep
		targetStep   int32
		expected     bool
	}{
		{
			"nil achieved step",
			nil,
			0,
			false,
		},
		{
			"empty achieved step",
			&shipper.AchievedStep{},
			0,
			false,
		},
		{
			"release stepping forward",
			&shipper.AchievedStep{
				Step: 0,
				Name: "0",
			},
			2,
			false,
		},
		{
			"standing release (target step == achieved step)",
			&shipper.AchievedStep{
				Step: 0,
				Name: "0",
			},
			0,
			false,
		},
		{
			"release stepping backwards",
			&shipper.AchievedStep{
				Step: 1,
				Name: "1",
			},
			0,
			true,
		},
	}
	for _, test := range tests {
		actual := IsReleaseSteppingBackwards(test.achievedStep, test.targetStep)
		if actual != test.expected {
			t.Fatalf("testing %s: expected %t, actual %t", test.title, test.expected, actual)
		}
	}
}
