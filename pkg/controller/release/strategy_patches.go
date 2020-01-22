package release

import (
	"encoding/json"

	"k8s.io/apimachinery/pkg/api/equality"
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
	// CapacityTargetSpecPatch is an altering one by it's nature: it's only
	// being created if a capacity target adjustment is required. Therefore
	// we're saving a few peanuts and moving on with always-apply strategy.
	return !p.IsEmpty()
}

func (p *TrafficTargetSpecPatch) IsEmpty() bool {
	return p == nil || p.NewSpec == nil
}

type ReleaseStrategyStatusPatch struct {
	Name              string
	NewStrategyStatus *shipper.ReleaseStrategyStatus
}

var _ StrategyPatch = (*ReleaseStrategyStatusPatch)(nil)

func (p *ReleaseStrategyStatusPatch) PatchSpec() (string, schema.GroupVersionKind, []byte) {
	patch := map[string]interface{}{
		"status": map[string]interface{}{
			"strategy": p.NewStrategyStatus,
		},
	}
	b, _ := json.Marshal(patch)
	return p.Name, shipper.SchemeGroupVersion.WithKind("Release"), b
}

func (p *ReleaseStrategyStatusPatch) Alters(o interface{}) bool {
	rel, ok := o.(*shipper.Release)
	if !ok || p.IsEmpty() {
		return false
	}
	return !equality.Semantic.DeepEqual(rel.Status.Strategy, p.NewStrategyStatus)
}

func (p *ReleaseStrategyStatusPatch) IsEmpty() bool {
	return p == nil || p.NewStrategyStatus == nil
}
