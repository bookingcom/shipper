package chart

import (
	"fmt"
	"path/filepath"
	"testing"

	"k8s.io/helm/pkg/chartutil"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		chartname string
		isvalid   bool
	}{
		{"reviews-api-0.0.1", true},
		{"reviews-api-broken-k8s-objects", false},
		{"reviews-api-multi-service-no-lb", false},
		{"reviews-api-multi-service-with-lb", true},
		{"reviews-api-single-service-no-lb", true},
		{"reviews-api-single-service-with-lb", true},
	}

	for _, testcase := range tests {
		t.Run(testcase.chartname, func(t *testing.T) {
			filename := fmt.Sprintf("%s.tgz", testcase.chartname)
			filepath := filepath.Join("testdata", "chart-samples", filename)
			chart, err := chartutil.Load(filepath)
			if err != nil {
				t.Fatalf("Failed to load chart: %s", err)
			}
			err = Validate(chart)
			if err != nil && testcase.isvalid {
				t.Errorf("Chart %q is expected to be valid, unexpected validation error returned: %s", testcase.chartname, err)
			}
			if err == nil && !testcase.isvalid {
				t.Errorf("Chart %q is expected to be invalid, no validation error returned", testcase.chartname)
			}
		})
	}
}
