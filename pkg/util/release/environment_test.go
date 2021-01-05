package release

import (
	"testing"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func TestIsReleaseProgressing(t *testing.T) {
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
			true,
		},
		{
			"empty achieved step",
			&shipper.AchievedStep{},
			0,
			true,
		},
		{
			"progressing release",
			&shipper.AchievedStep{
				Step: 0,
				Name: "0",
			},
			2,
			true,
		},
		{
			"progressing release (target step == achieved step)",
			&shipper.AchievedStep{
				Step: 0,
				Name: "0",
			},
			0,
			true,
		},
		{
			"not progressing release",
			&shipper.AchievedStep{
				Step: 1,
				Name: "1",
			},
			0,
			false,
		},
	}
	for _, test := range tests {
		actual := IsReleaseProgressing(test.achievedStep, test.targetStep)
		if actual != test.expected {
			t.Fatalf("testing %s: expected %t, actual %t", test.title, test.expected, actual)
		}
	}
}

