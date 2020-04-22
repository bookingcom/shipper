package release

import (
	"encoding/json"

	"k8s.io/apimachinery/pkg/runtime/schema"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

type StrategyPatch interface {
	PatchSpec() (string, schema.GroupVersionKind, []byte)
	Alters(interface{}) bool
	IsEmpty() bool
}

type CapacityTargetSpecPatch struct {
	Name    string
	NewSpec *shipper.CapacityTargetSpec
}

var _ StrategyPatch = (*CapacityTargetSpecPatch)(nil)

func (p *CapacityTargetSpecPatch) PatchSpec() (string, schema.GroupVersionKind, []byte) {
	patch := make(map[string]interface{})
	patch["spec"] = p.NewSpec
	b, _ := json.Marshal(patch)
	return p.Name, shipper.SchemeGroupVersion.WithKind("CapacityTarget"), b
}

func (p *CapacityTargetSpecPatch) Alters(o interface{}) bool {
	// CapacityTargetSpecPatch is an altering one by it's nature: it's only
	// being created if a capacity target adjustment is required. Therefore
	// we're saving a few peanuts and moving on with always-apply strategy.
	return !p.IsEmpty()
}

func (p *CapacityTargetSpecPatch) IsEmpty() bool {
	return p == nil || p.NewSpec == nil
}

type TrafficTargetSpecPatch struct {
	Name    string
	NewSpec *shipper.TrafficTargetSpec
}

var _ StrategyPatch = (*TrafficTargetSpecPatch)(nil)

func (p *TrafficTargetSpecPatch) PatchSpec() (string, schema.GroupVersionKind, []byte) {
	patch := make(map[string]interface{})
	patch["spec"] = p.NewSpec
	b, _ := json.Marshal(patch)
	return p.Name, shipper.SchemeGroupVersion.WithKind("TrafficTarget"), b
}

func (p *TrafficTargetSpecPatch) Alters(o interface{}) bool {
	// TrafficTargetSpecPatch is an altering one by it's nature: it's only
	// being created if a capacity target adjustment is required. Therefore
	// we're saving a few peanuts and moving on with always-apply strategy.
	return !p.IsEmpty()
}

func (p *TrafficTargetSpecPatch) IsEmpty() bool {
	return p == nil || p.NewSpec == nil
}
