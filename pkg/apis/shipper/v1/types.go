package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ShipmentOrder describes a request to deploy an application
type ShipmentOrder struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Specification of the desired behavior of the order.
	Spec ShipmentOrderSpec `json:"spec"`
	// Most recently observed status of the order
	Status ShipmentOrderStatus `json:"status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ShipmentOrderList is a list of ShipmentOrders. Mostly only useful for
// admins: regular users interact with exactly one ShipmentOrder at once
type ShipmentOrderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ShipmentOrder `json:"items"`
}

type ShipmentOrderStatus string

type ShipmentOrderSpec struct {
	// selectors for target clusters for the deployment
	ClusterSelectors []ClusterSelector `json:"clusterSelectors"`

	// Chart spec: name and version
	Chart Chart `json:"chart"`

	// how v2 gets the traffic
	Strategy ReleaseStrategy `json:"strategy"`

	// the inlined "values.yaml" to apply to the chart when rendering it
	Values string `json:"values"`
}

type ClusterSelector struct {
	Regions      []string `json:"regions"`
	Capabilities []string `json:"capabilities"`
}

type Chart struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ReleaseStrategy string
type ChartValues map[string]interface{}

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Strategy defines a sequence of steps to safely deliver a change to production
type Strategy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec StrategySpec `json:"spec"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type StrategyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Strategy `json:"items"`
}

type StrategySpec struct {
	Steps []StrategyStep `json:"steps"`
}

type StrategyStep struct {
	IncumbentCapacity string `json:"incumbent_capacity"`
	IncumbentTraffic  string `json:"incumbent_traffic"`

	ContenderCapacity string `json:"contender_capacity"`
	ContenderTraffic  string `json:"contender_traffic"`
}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// An TargetCluster is a cluster we're deploying to.
type TargetCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TargetClusterSpec `json:"spec"`

	// Most recently observed status of the order
	/// +optional
	Status TargetClusterStatus `json:"status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type TargetClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []TargetCluster `json:"items"`
}

type TargetClusterSpec struct {
	Capabilities []string `json:"capabilities"`
	Region       string   `json:"region"`

	//Capacity TargetClusterCapacity
}

type TargetClusterStatus struct {
	InService bool `json:"in_service"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// A Release is the  defines the goal state for # of pods for incumbent and
// contender versions. This is used by the StrategyController to change the
// state of the cluster to satisfy a single step of a Strategy.
type Release struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ReleaseSpec   `json:"spec"`
	Status ReleaseStatus `json:"status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ReleaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Release `json:"items"`
}

type ReleaseSpec struct {
	// better indicated with labels?
	TargetStep int `json:"targetstep"`
}

// this will likely grow into a struct with interesting fields
type ReleaseStatus string

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// A CapacityTarget defines the goal state for # of pods for incumbent and
// contender versions. This is used by the StrategyController to change the
// state of the cluster to satisfy a single step of a Strategy.
type CapacityTarget struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CapacityTargetSpec   `json:"spec"`
	Status CapacityTargetStatus `json:"status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type CapacityTargetList struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Items []CapacityTarget `json:"items"`
}

type CapacityTargetStatus string

type CapacityTargetSpec struct {
	// the capacity and traffic controllers need context to pick the right
	// things to target for traffic. These labels need to end up on the
	// pods, since that's what service mesh impls will mostly care about.
	Selectors []string                `json:"selectors"`
	Clusters  []ClusterCapacityTarget `json:"clusters"`
}

type ClusterCapacityTarget struct {
	Name              string `json:"name"`
	IncumbentReplicas uint   `json:"incumbent_replicas"`
	ContenderReplicas uint   `json:"contender_replicas"`
	Status            string `json:"status"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// A TrafficTarget defines the goal state for traffic split between incumbent
// and contender versions. This is used by the StrategyController to change the
// state of the service mesh to satisfy a single step of a Strategy.
type TrafficTarget struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TrafficTargetSpec `json:"spec"`

	Status TrafficTargetStatus `json:"status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type TrafficTargetList struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Items []TrafficTarget `json:"items"`
}

type TrafficTargetStatus string

type TrafficTargetSpec struct {
	// the capacity and traffic controllers need context to pick the right things to modify
	Selectors []string               `json:"selectors"`
	Clusters  []ClusterTrafficTarget `json:"clusters"`
}

type ClusterTrafficTarget struct {
	Name string `json:"name"`
	// apimachinery intstr for percentages?
	IncumbentTraffic uint `json:"incumbent_traffic"`
	ContenderTraffic uint `json:"contender_traffic"`

	Status string `json:"status"`
}
