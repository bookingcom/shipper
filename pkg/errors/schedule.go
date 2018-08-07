package errors

import (
	"fmt"
	"strings"
)

type NoRegionsSpecified struct{}

func IsNoRegionsSpecified(err error) bool {
	_, ok := err.(NoRegionsSpecified)
	return ok
}

func (e NoRegionsSpecified) Name() string {
	return "NoRegionsSpecified"
}

func (e NoRegionsSpecified) Error() string {
	return "No regions specified in clusterRequirements. Must list at least one region"
}

func (e NoRegionsSpecified) ShouldRetry() bool {
	return false
}

func NewNoRegionsSpecified() NoRegionsSpecified {
	return NoRegionsSpecified{}
}

type NotEnoughClustersInRegion struct {
	region    string
	required  int
	available int
}

func IsNotEnoughClustersInRegion(err error) bool {
	_, ok := err.(NotEnoughClustersInRegion)
	return ok
}

func (e NotEnoughClustersInRegion) Name() string {
	return "NotEnoughClustersInRegion"
}

func (e NotEnoughClustersInRegion) Error() string {
	return fmt.Sprintf("Not enough clusters in region %q. Required: %d / Available: %d", e.region, e.required, e.available)
}

func (e NotEnoughClustersInRegion) ShouldRetry() bool {
	return false
}

func NewNotEnoughClustersInRegion(region string, required, available int) NotEnoughClustersInRegion {
	return NotEnoughClustersInRegion{
		region:    region,
		required:  required,
		available: available,
	}
}

type NotEnoughCapableClustersInRegion struct {
	region       string
	capabilities []string
	required     int
	available    int
}

func IsNotEnoughCapableClustersInRegion(err error) bool {
	_, ok := err.(NotEnoughCapableClustersInRegion)
	return ok
}

func (e NotEnoughCapableClustersInRegion) Name() string {
	return "NotEnoughCapableClustersInRegion"
}

func (e NotEnoughCapableClustersInRegion) Error() string {
	capabilitiesString := strings.Join(e.capabilities, ",")
	return fmt.Sprintf(
		"Not enough clusters in region %q with required capabilities %q. Required: %d / Available: %d",
		e.region, capabilitiesString, e.required, e.available,
	)
}

func (e NotEnoughCapableClustersInRegion) ShouldRetry() bool {
	return false
}

func NewNotEnoughCapableClustersInRegion(region string, capabilities []string, required, available int) error {
	return NotEnoughCapableClustersInRegion{
		region:       region,
		capabilities: capabilities,
		required:     required,
		available:    available,
	}
}

type InvalidReleaseOwnerRefs struct {
	count int
}

func IsInvalidReleaseOwnerRefs(err error) bool {
	_, ok := err.(InvalidReleaseOwnerRefs)
	return ok
}

func (e InvalidReleaseOwnerRefs) Name() string {
	return "InvalidReleaseOwnerRefs"
}

func (e InvalidReleaseOwnerRefs) Error() string {
	return fmt.Sprintf(
		"Releases should only ever have 1 owner, but this one has %d", e.count,
	)
}

func (e InvalidReleaseOwnerRefs) ShouldRetry() bool {
	return false
}

func NewInvalidReleaseOwnerRefs(count int) InvalidReleaseOwnerRefs {
	return InvalidReleaseOwnerRefs{
		count: count,
	}
}

type DuplicateCapabilityRequirement struct {
	capability string
}

func IsDuplicateCapabilityRequirement(err error) bool {
	_, ok := err.(DuplicateCapabilityRequirement)
	return ok
}

func (e DuplicateCapabilityRequirement) Name() string {
	return "DuplicateCapabilityRequirement"
}

func (e DuplicateCapabilityRequirement) Error() string {
	return fmt.Sprintf(
		"Capability %q listed more than once in clusterRequirements",
		e.capability,
	)
}

func (e DuplicateCapabilityRequirement) ShouldRetry() bool {
	return false
}

func NewDuplicateCapabilityRequirement(capability string) DuplicateCapabilityRequirement {
	return DuplicateCapabilityRequirement{
		capability: capability,
	}
}
