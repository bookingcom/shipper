package release

import (
	"fmt"
	"reflect"
	"testing"

	intstrutil "k8s.io/apimachinery/pkg/util/intstr"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func TestGetMaxSurgePercent(t *testing.T) {
	var tests = []struct {
		title                string
		rollingUpdate        *shipper.RollingUpdate
		replicaCount         int32
		expectedSurgePercent int
		expectedError        error
	}{
		{
			title:                "default max surge with nil rolling update",
			rollingUpdate:        nil,
			replicaCount:         10,
			expectedSurgePercent: 100,
			expectedError:        nil,
		},
		{
			title:                "default max surge with empty rolling update",
			rollingUpdate:        &shipper.RollingUpdate{},
			replicaCount:         10,
			expectedSurgePercent: 100,
			expectedError:        nil,
		},
		{
			title: "max surge with maxSurge int value",
			rollingUpdate: &shipper.RollingUpdate{
				MaxSurge: intstrutil.FromInt(2),
			},
			replicaCount:         10,
			expectedSurgePercent: 20,
			expectedError:        nil,
		},
		{
			title: "max surge with maxSurge percent value",
			rollingUpdate: &shipper.RollingUpdate{
				MaxSurge: intstrutil.FromString("25%"),
			},
			replicaCount:         10,
			expectedSurgePercent: 30,
			expectedError:        nil,
		},
		{
			title: "max surge with maxSurge percent value round up",
			rollingUpdate: &shipper.RollingUpdate{
				MaxSurge: intstrutil.FromString("25%"),
			},
			replicaCount:         10,
			expectedSurgePercent: 30,
			expectedError:        nil,
		},
		{
			title: "default max surge with maxSurge negative value",
			rollingUpdate: &shipper.RollingUpdate{
				MaxSurge: intstrutil.FromInt(-2),
			},
			replicaCount:         10,
			expectedSurgePercent: 100,
			expectedError:        nil,
		},
		{
			title: "default max surge with maxSurge zero value",
			rollingUpdate: &shipper.RollingUpdate{
				MaxSurge: intstrutil.FromInt(0),
			},
			replicaCount:         10,
			expectedSurgePercent: 100,
			expectedError:        nil,
		},
		{
			title: "default max surge with maxSurge higher value then replicaCount",
			rollingUpdate: &shipper.RollingUpdate{
				MaxSurge: intstrutil.FromInt(80),
			},
			replicaCount:         10,
			expectedSurgePercent: 100,
			expectedError:        nil,
		},
		{
			title: "default max surge with invalid maxSurge value",
			rollingUpdate: &shipper.RollingUpdate{
				MaxSurge: intstrutil.FromString("hello"),
			},
			replicaCount:         10,
			expectedSurgePercent: 0,
			expectedError:        fmt.Errorf("invalid value for IntOrString: invalid value \"hello\": strconv.Atoi: parsing \"hello\": invalid syntax"),
		},
	}

	for _, test := range tests {
		percent, err := GetMaxSurgePercent(test.rollingUpdate, test.replicaCount)
		if !reflect.DeepEqual(err, test.expectedError) {
			t.Fatalf("expected error %v and got %v", test.expectedError, err)
		}
		if percent != test.expectedSurgePercent {
			t.Fatalf("testing %s: expected MaxSurg percent %d and got %d", test.title, test.expectedSurgePercent, percent)
		}
	}
}
