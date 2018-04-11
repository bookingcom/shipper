package v1

import (
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ShipperNamespace = "shipper-system"

	PhaseLabel = "phase"

	ReleaseLabel                = "release"
	AppLabel                    = "shipper-app"
	ReleaseEnvironmentHashLabel = "shipper-release-hash"

	ReleaseRecordWaitingForObject = "WaitingForObject"
	ReleaseRecordObjectCreated    = "ReleaseCreated"

	ReleasePhaseWaitingForScheduling = "WaitingForScheduling"
	ReleasePhaseWaitingForStrategy   = "WaitingForStrategy"
	ReleasePhaseWaitingForCommand    = "WaitingForCommand"
	ReleasePhaseInstalled            = "Installed"
	ReleasePhaseSuperseded           = "Superseded"
	ReleasePhaseAborted              = "Aborted"

	InstallationStatusInstalled = "Installed"
	InstallationStatusFailed    = "Failed"

	ReleaseTemplateGenerationAnnotation = "shipper.booking.com/release.template.generation"
	ReleaseClustersAnnotation           = "shipper.booking.com/release.clusters"
	ReleaseReplicasAnnotation           = "shipper.booking.com/release.replicas"

	SecretChecksumAnnotation    = "shipper.booking.com/cluster-secret.checksum"
	SecretClusterNameAnnotation = "shipper.booking.com/cluster-secret.clusterName"

	LBLabel         = "shipper-lb"
	LBForProduction = "production"

	CapacityTargetSadPodLimit = 5
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Application describes a deployable application
type Application struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ApplicationSpec `json:"spec"`
	// Most recently observed status of the application
	Status ApplicationStatus `json:"status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ApplicationList is a list of Applications. Mostly only useful for
// admins: regular users interact with exactly one Application at once
type ApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Application `json:"items"`
}

type ApplicationSpec struct {
	Template ReleaseEnvironment `json:"template"`
}

type ApplicationStatus struct {
	History []*ReleaseRecord `json:"history"`
}

type ReleaseRecord struct {
	Name   string `json:"name"`
	Status string `json:"status"`
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

type ReleaseStrategy struct {
	Name string `json:"name"`
}

const (
	// ReleaseStrategyVanguard is a gradual deployment strategy with custom steps.
	ReleaseStrategyVanguard string = "vanguard"
)

type ChartValues map[string]interface{}

func (in *ChartValues) DeepCopyInto(out *ChartValues) {
	*out = ChartValues(
		deepCopyJSON(
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
	Phase        string                 `json:"phase"`
	AchievedStep uint                   `json:"achievedStep"`
	Strategy     *ReleaseStrategyStatus `json:"strategy,omitempty"`
}

type ReleaseEnvironment struct {
	// Chart spec: name, version, repoURL
	Chart Chart `json:"chart"`
	// the inlined "values.yaml" to apply to the chart when rendering it
	// XXX pointer here means it's null-able, do we want that?
	Values *ChartValues `json:"values"`

	// how v2 gets the traffic
	Strategy ReleaseStrategy `json:"strategy"`

	// set of sidecars to inject into the chart on rendering
	Sidecars []Sidecar `json:"sidecars"`

	// selectors for target clusters for the deployment
	// XXX what are the semantics when the field is empty/omitted?
	ClusterSelectors []ClusterSelector `json:"clusterSelectors"`
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
	Clusters []*ClusterInstallationStatus `json:"clusters,omitempty"`
}

type ClusterInstallationStatus struct {
	Name       string                         `json:"name"`
	Status     string                         `json:"status"`
	Message    string                         `json:"message,omitempty"`
	Conditions []ClusterInstallationCondition `json:"conditions,omitempty"`
}

type ClusterInstallationCondition struct {
	Type               ClusterConditionType   `json:"type"`
	Status             corev1.ConditionStatus `json:"status"`
	LastTransitionTime metav1.Time            `json:"lastTransitionTime,omitempty"`
	Reason             string                 `json:"reason,omitempty"`
	Message            string                 `json:"message,omitempty"`
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
	Name              string                     `json:"name"`
	AvailableReplicas int32                      `json:"availableReplicas"`
	AchievedPercent   int32                      `json:"achievedPercent"`
	SadPods           []PodStatus                `json:"sadPods,omitempty"`
	Conditions        []ClusterCapacityCondition `json:"conditions,omitempty"`
}

type ClusterConditionType string

const (
	ClusterConditionTypeOperational ClusterConditionType = "Operational"
	ClusterConditionTypeReady       ClusterConditionType = "Ready"
)

type ClusterCapacityCondition struct {
	Type               ClusterConditionType   `json:"type"`
	Status             corev1.ConditionStatus `json:"status"`
	LastTransitionTime metav1.Time            `json:"lastTransitionTime,omitempty"`
	Reason             string                 `json:"reason,omitempty"`
	Message            string                 `json:"message,omitempty"`
}

type PodStatus struct {
	Name           string                   `json:"name"`
	Containers     []corev1.ContainerStatus `json:"containers"`
	InitContainers []corev1.ContainerStatus `json:"initContainers"`
	Condition      corev1.PodCondition      `json:"condition"`
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
	Name            string                    `json:"name"`
	AchievedTraffic uint                      `json:"achievedTraffic"`
	Status          string                    `json:"status"`
	Conditions      []ClusterTrafficCondition `json:"conditions"`
}

type ClusterTrafficCondition struct {
	Type               ClusterConditionType   `json:"type"`
	Status             corev1.ConditionStatus `json:"status"`
	LastTransitionTime metav1.Time            `json:"lastTransitionTime,omitempty"`
	Reason             string                 `json:"reason,omitempty"`
	Message            string                 `json:"message,omitempty"`
}

type TrafficTargetSpec struct {
	Clusters []ClusterTrafficTarget `json:"clusters,omitempty"`
}

type ClusterTrafficTarget struct {
	Name string `json:"name"`
	// apimachinery intstr for percentages?
	TargetTraffic uint `json:"targetTraffic"`
}

type ReleaseStrategyStatus struct {
	State      ReleaseStrategyState       `json:"state,omitempty"`
	Conditions []ReleaseStrategyCondition `json:"conditions,omitempty"`
}

type ReleaseStrategyState struct {
	WaitingForInstallation StrategyState `json:"waitingForInstallation"`
	WaitingForCapacity     StrategyState `json:"waitingForCapacity"`
	WaitingForTraffic      StrategyState `json:"waitingForTraffic"`
	WaitingForCommand      StrategyState `json:"waitingForCommand"`
}

type ReleaseStrategyCondition struct {
	Type               StrategyConditionType  `json:"type"`
	Status             corev1.ConditionStatus `json:"status"`
	LastTransitionTime metav1.Time            `json:"lastTransitionTime,omitempty"`
	Reason             string                 `json:"reason,omitempty"`
	Message            string                 `json:"message,omitempty"`
	Step               int32                  `json:"step,omitempty"`
}

type StrategyConditionType string

const (
	StrategyConditionContenderAchievedInstallation StrategyConditionType = "ContenderAchievedInstallation"
	StrategyConditionContenderAchievedCapacity     StrategyConditionType = "ContenderAchievedCapacity"
	StrategyConditionContenderAchievedTraffic      StrategyConditionType = "ContenderAchievedTraffic"
	StrategyConditionIncumbentAchievedCapacity     StrategyConditionType = "IncumbentAchievedCapacity"
	StrategyConditionIncumbentAchievedTraffic      StrategyConditionType = "IncumbentAchievedTraffic"
)

type StrategyState string

const (
	StrategyStateUnknown StrategyState = "Unknown"
	StrategyStateTrue    StrategyState = "True"
	StrategyStateFalse   StrategyState = "False"
)

func (ss *StrategyState) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if s == "" {
		*ss = StrategyStateUnknown
	} else {
		*ss = StrategyState(s)
	}
	return nil
}
