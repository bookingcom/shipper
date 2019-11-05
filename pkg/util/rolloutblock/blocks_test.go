package rolloutblock

import (
	"testing"

	shippererrors "github.com/bookingcom/shipper/pkg/errors"
)

func TestValidateBlocks(t *testing.T) {
	singleDNSBlock := NewObjectNameList("rollout-blocks-global/dns-outage")
	singleDemoBlock := NewObjectNameList("frontend/demo-to-investors-in-progress")
	multiBlock := NewObjectNameList("rollout-blocks-global/dns-outage,frontend/demo-to-investors-in-progress")
	tests := []struct {
		Name       string
		Existing   ObjectNameList
		Overriding ObjectNameList
		Expected   error
	}{
		{
			"single effective block and non overrides",
			singleDNSBlock,
			NewObjectNameList(""),
			shippererrors.NewRolloutBlockError(singleDNSBlock.String()),
		},
		{
			"multiple effective block and one override",
			multiBlock,
			singleDemoBlock,
			shippererrors.NewRolloutBlockError(singleDNSBlock.String()),
		},
		{
			"single effective block and single effective override",
			singleDNSBlock,
			singleDNSBlock,
			nil,
		},
		{
			"non effective blocks and non effective overrides",
			NewObjectNameList(""),
			NewObjectNameList(""),
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			blockError := ValidateBlocks(tt.Existing, tt.Overriding)
			if blockError != tt.Expected {
				t.Errorf("Unexpected result returned by ValidateBlocks(): got: %s, want: %s", blockError, tt.Expected)
			}
		})
	}
}

func TestValidateAnnotations(t *testing.T) {
	singleDNSBlock := NewObjectNameList("rollout-blocks-global/dns-outage")
	singleDemoBlock := NewObjectNameList("frontend/demo-to-investors-in-progress")
	multiBlock := NewObjectNameList("rollout-blocks-global/dns-outage,frontend/demo-to-investors-in-progress")
	tests := []struct {
		Name       string
		Existing   ObjectNameList
		Overriding ObjectNameList
		Expected   error
	}{
		{
			"single effective block and different single override",
			singleDNSBlock,
			singleDemoBlock,
			shippererrors.NewInvalidRolloutBlockOverrideError(singleDemoBlock.String()),
		},
		{
			"non effective block and one override",
			NewObjectNameList(""),
			singleDemoBlock,
			shippererrors.NewInvalidRolloutBlockOverrideError(singleDemoBlock.String()),
		},
		{
			"single effective block and single effective override",
			singleDNSBlock,
			singleDNSBlock,
			nil,
		},
		{
			"non effective blocks and non effective overrides",
			NewObjectNameList(""),
			NewObjectNameList(""),
			nil,
		},
		{
			"multiple effective block and multiple effective override",
			multiBlock,
			multiBlock,
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			blockError := ValidateAnnotations(tt.Existing, tt.Overriding)
			if blockError != tt.Expected {
				t.Errorf("Unexpected result returned by ValidateBlocks(): got: %s, want: %s", blockError, tt.Expected)
			}
		})
	}
}
