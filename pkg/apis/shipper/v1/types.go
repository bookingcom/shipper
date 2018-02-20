package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	ReleaseLabel   = "release"
	ReleaseLinkAnn = "releaseLink"

	ReleaseIncumbentAnn = "incumbent"
	ReleaseContenderAnn = "contender"

	ReleasePhaseWaitingForScheduling = "WaitingForScheduling"
	ReleasePhaseWaitingForStrategy   = "WaitingForStrategy"
	ReleasePhaseWaitingForCommand    = "WaitingForCommand"
	ReleasePhaseInstalled            = "Installed"
	ReleasePhaseSuperseded           = "Superseded"

	InstallationStatusInstalled = "Installed"
	InstallationStatusFailed    = "Failed"
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

// ShipmentOrderPhase is what's happening with a ShipmentOrder.
type ShipmentOrderPhase string

const (
	// ShipmentOrderPhasePending indicates ShipmentOrders that are yet to be picked
	// by the ShipmentOrder controller.
	ShipmentOrderPhasePending ShipmentOrderPhase = "Pending"
	// ShipmentOrderPhaseShipping indicates ShipmentOrders that are being processed
	// by the ShipmentOrder controller.
	ShipmentOrderPhaseShipping ShipmentOrderPhase = "Shipping"
	// ShipmentOrderPhaseShipped indicates ShipmentOrders that have been already
	// processed by the ShipmentOrder controller.
	ShipmentOrderPhaseShipped ShipmentOrderPhase = "Shipped"
)

type ShipmentOrderStatus struct {
	Phase ShipmentOrderPhase `json:"phase"`
}

type ShipmentOrderSpec struct {
	// selectors for target clusters for the deployment
	// XXX what are the semantics when the field is empty/omitted?
	ClusterSelectors []ClusterSelector `json:"clusterSelectors"`

	// Chart spec: name and version
	Chart Chart `json:"chart"`

	// how v2 gets the traffic
	Strategy ReleaseStrategy `json:"strategy"`

	// the inlined "values.yaml" to apply to the chart when rendering it
	// XXX pointer here means it's null-able, do we want that?
	Values *ChartValues `json:"values"`
}

type ClusterSelector struct {
	Regions      []string `json:"regions"`
	Capabilities []string `json:"capabilities"`
}

type Chart struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	RepoURL string `json:"repoUrl"`
}

// ReleaseStrategy is how deployments are executed.
type ReleaseStrategy string

const (
	// ReleaseStrategyVanguard is a gradual deployment strategy with custom steps.
	ReleaseStrategyVanguard ReleaseStrategy = "vanguard"
)

type ChartValues map[string]interface{}

func (in *ChartValues) DeepCopyInto(out *ChartValues) {
	*out = ChartValues(
		runtime.DeepCopyJSON(
			map[string]interface{}(*in),
		),
	)
}

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
	IncumbentCapacity string `json:"incumbentCapacity"`
	IncumbentTraffic  string `json:"incumbentTraffic"`

	ContenderCapacity string `json:"contenderCapacity"`
	ContenderTraffic  string `json:"contenderTraffic"`
}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// An Cluster is a cluster we're deploying to.
type Cluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ClusterSpec `json:"spec"`

	// Most recently observed status of the order
	/// +optional
	Status ClusterStatus `json:"status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Cluster `json:"items"`
}

type ClusterSpec struct {
	Capabilities  []string `json:"capabilities"`
	Region        string   `json:"region"`
	APIMaster     string   `json:"apiMaster"`
	Unschedulable bool     `json:"unschedulable"`

	//Capacity ClusterCapacity
}

type ClusterStatus struct {
	InService bool `json:"inService"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// A Release is the  defines the goal state for # of pods for incumbent and
// contender versions. This is used by the StrategyController to change the
// state of the cluster to satisfy a single step of a Strategy.
type Release struct {
	metav1.TypeMeta `json:",inline"`
	ReleaseMeta     `json:"metadata,omitempty"`

	Spec   ReleaseSpec   `json:"spec"`
	Status ReleaseStatus `json:"status"`
}

type ReleaseMeta struct {
	metav1.ObjectMeta `json:",inline"`
	Environment       ReleaseEnvironment `json:"environment"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ReleaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Release `json:"items"`
}

type ReleaseSpec struct {
	// better indicated with labels?
	TargetStep int `json:"targetStep"`
}

// this will likely grow into a struct with interesting fields
type ReleaseStatus struct {
	Phase        string `json:"phase"`
	AchievedStep uint   `json:"achievedStep"`
}

type ReleaseEnvironment struct {
	Clusters      []string          `json:"clusters"`
	Chart         EmbeddedChart     `json:"chart"`
	ShipmentOrder ShipmentOrderSpec `json:"shipmentOrder"`
	Sidecars      []Sidecar         `json:"sidecars"`
	Replicas      int32             `json:"replicas"`
}

type EmbeddedChart struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Tarball string `json:"tarball"`
}

type EmbeddedShipmentOrder struct {
	ClusterSelectors []ClusterSelector `json:"clusterSelectors"`
	Strategy         string            `json:"strategy"`
	Values           *ChartValues      `json:"values"`
}

type Sidecar struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// An InstallationTarget defines the goal state for # of pods for incumbent and
// contender versions. This is used by the StrategyController to change the
// state of the cluster to satisfy a single step of a Strategy.
type InstallationTarget struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InstallationTargetSpec   `json:"spec"`
	Status InstallationTargetStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type InstallationTargetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []InstallationTarget `json:"items"`
}

type InstallationTargetStatus struct {
	Clusters []ClusterInstallationStatus `json:"clusters,omitempty"`
}

type ClusterInstallationStatus struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`

	// Conditions []Condition
}

type InstallationTargetSpec struct {
	Clusters []string `json:"clusters"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// A CapacityTarget defines the goal state for # of pods for incumbent and
// contender versions. This is used by the StrategyController to change the
// state of the cluster to satisfy a single step of a Strategy.
type CapacityTarget struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CapacityTargetSpec   `json:"spec"`
	Status CapacityTargetStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type CapacityTargetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []CapacityTarget `json:"items"`
}

type CapacityTargetStatus struct {
	Clusters []ClusterCapacityStatus `json:"clusters,omitempty"`
}

type ClusterCapacityStatus struct {
	Name              string      `json:"name"`
	AvailableReplicas int32       `json:"availableReplicas"`
	AchievedPercent   int32       `json:"achievedPercent"`
	SadPods           []PodStatus `json:"sadPods"`
}

type PodStatus struct {
	Name       string                   `json:"name"`
	Containers []corev1.ContainerStatus `json:"containers"`
	Condition  corev1.PodCondition      `json:"condition"`
}

// the capacity and traffic controllers need context to pick the right
// things to target for traffic. These labels need to end up on the
// pods, since that's what service mesh impls will mostly care about.
//	Selectors []string                `json:"selectors"`

type CapacityTargetSpec struct {
	Clusters []ClusterCapacityTarget `json:"clusters"`
}

type ClusterCapacityTarget struct {
	Name    string `json:"name"`
	Percent int32  `json:"percent"`
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

	Status TrafficTargetStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type TrafficTargetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []TrafficTarget `json:"items"`
}

type TrafficTargetStatus struct {
	Clusters []*ClusterTrafficStatus `json:"clusters,omitempty"`
}

type ClusterTrafficStatus struct {
	Name            string `json:"name"`
	AchievedTraffic uint   `json:"achievedTraffic"`
	Status          string `json:"status"`
}

type TrafficTargetSpec struct {
	Clusters []ClusterTrafficTarget `json:"clusters,omitempty"`
}

type ClusterTrafficTarget struct {
	Name string `json:"name"`
	// apimachinery intstr for percentages?
	TargetTraffic uint `json:"targetTraffic"`
}
