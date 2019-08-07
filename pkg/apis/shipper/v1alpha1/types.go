package v1alpha1

import (
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ShipperNamespace = "shipper-system"

	PhaseLabel = "phase"

	ReleaseLabel                = "shipper-release"
	AppLabel                    = "shipper-app"
	ReleaseEnvironmentHashLabel = "shipper-release-hash"
	PodTrafficStatusLabel       = "shipper-traffic-status"

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

	AppHighestObservedGenerationAnnotation = "shipper.booking.com/app.highestObservedGeneration"

	AppChartNameAnnotation            = "shipper.booking.com/app.chart.name"
	AppChartVersionResolvedAnnotation = "shipper.booking.com/app.chart.version.resolved"
	AppChartVersionRawAnnotation      = "shipper.booking.com/app.chart.version.raw"

	ReleaseGenerationAnnotation        = "shipper.booking.com/release.generation"
	ReleaseTemplateIterationAnnotation = "shipper.booking.com/release.template.iteration"
	ReleaseClustersAnnotation          = "shipper.booking.com/release.clusters"

	SecretChecksumAnnotation             = "shipper.booking.com/cluster-secret.checksum"
	SecretClusterNameAnnotation          = "shipper.booking.com/cluster-secret.clusterName"
	SecretClusterSkipTlsVerifyAnnotation = "shipper.booking.com/cluster-secret.insecure-tls-skip-verify"

	LBLabel         = "shipper-lb"
	LBForProduction = "production"

	Enabled  = "enabled"
	Disabled = "disabled"

	True  = "true"
	False = "false"

	HelmReleaseLabel    = "release"
	HelmWorkaroundLabel = "enable-helm-release-workaround"

	RBACDomainLabel       = "shipper-rbac-domain"
	RBACManagementDomain  = "management"
	RBACApplicationDomain = "application"
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
	RevisionHistoryLimit *int32             `json:"revisionHistoryLimit"`
	Template             ReleaseEnvironment `json:"template"`
}

type ApplicationStatus struct {
	Conditions []ApplicationCondition `json:"conditions,omitempty"`
	History    []string               `json:"history,omitempty"`
}

type ApplicationConditionType string

const (
	ApplicationConditionTypeValidHistory  ApplicationConditionType = "ValidHistory"
	ApplicationConditionTypeReleaseSynced ApplicationConditionType = "ReleaseSynced"
	ApplicationConditionTypeAborting      ApplicationConditionType = "Aborting"
	ApplicationConditionTypeRollingOut    ApplicationConditionType = "RollingOut"
)

type ApplicationCondition struct {
	Type               ApplicationConditionType `json:"type"`
	Status             corev1.ConditionStatus   `json:"status"`
	LastTransitionTime metav1.Time              `json:"lastTransitionTime,omitempty"`
	Reason             string                   `json:"reason,omitempty"`
	Message            string                   `json:"message,omitempty"`
}

type Chart struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	RepoURL string `json:"repoUrl"`
}

type ChartValues map[string]interface{}

func (in *ChartValues) DeepCopyInto(out *ChartValues) {
	*out = ChartValues(
		deepCopyJSON(
			map[string]interface{}(*in),
		),
	)
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
	Capabilities []string                 `json:"capabilities"`
	Region       string                   `json:"region"`
	APIMaster    string                   `json:"apiMaster"`
	Scheduler    ClusterSchedulerSettings `json:"scheduler"`
}

type ClusterSchedulerSettings struct {
	Unschedulable bool    `json:"unschedulable"`
	Weight        *int32  `json:"weight,omitempty"`
	Identity      *string `json:"identity,omitempty"`
}

// NOTE(btyler) when we introduce capacity based scheduling, the capacity can
// be collected by a cluster controller and stored in cluster.status
type ClusterStatus struct {
	InService bool `json:"inService"`
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
	TargetStep  int32              `json:"targetStep"`
	Environment ReleaseEnvironment `json:"environment"`
}

// this will likely grow into a struct with interesting fields
type ReleaseStatus struct {
	AchievedStep *AchievedStep          `json:"achievedStep,omitempty"`
	Strategy     *ReleaseStrategyStatus `json:"strategy,omitempty"`
	Conditions   []ReleaseCondition     `json:"conditions,omitempty"`
}

type AchievedStep struct {
	Step int32  `json:"step"`
	Name string `json:"name"`
}

type ReleaseConditionType string

const (
	ReleaseConditionTypeScheduled ReleaseConditionType = "Scheduled"
	ReleaseConditionTypeInstalled ReleaseConditionType = "Installed"
	ReleaseConditionTypeComplete  ReleaseConditionType = "Complete"
)

type ReleaseCondition struct {
	Type               ReleaseConditionType   `json:"type"`
	Status             corev1.ConditionStatus `json:"status"`
	LastTransitionTime metav1.Time            `json:"lastTransitionTime,omitempty"`
	Reason             string                 `json:"reason,omitempty"`
	Message            string                 `json:"message,omitempty"`
}

type ReleaseEnvironment struct {
	// Chart spec: name, version, repoURL
	Chart Chart `json:"chart"`
	// the inlined "values.yaml" to apply to the chart when rendering it
	// XXX pointer here means it's null-able, do we want that?
	Values *ChartValues `json:"values"`

	// requirements for target clusters for the deployment
	ClusterRequirements ClusterRequirements `json:"clusterRequirements"`

	Strategy *RolloutStrategy `json:"strategy,omitempty"`
}

type ClusterRequirements struct {
	// it is an error to not specify any regions
	Regions      []RegionRequirement `json:"regions"`
	Capabilities []string            `json:"capabilities,omitempty"`
}

type RegionRequirement struct {
	Name     string `json:"name"`
	Replicas *int32 `json:"replicas,omitempty"`
}

type RolloutStrategy struct {
	Steps []RolloutStrategyStep `json:"steps"`
}

type RolloutStrategyStep struct {
	Name     string                   `json:"name"`
	Capacity RolloutStrategyStepValue `json:"capacity"`
	Traffic  RolloutStrategyStepValue `json:"traffic"`
}

type RolloutStrategyStepValue struct {
	Incumbent int32 `json:"incumbent"`
	Contender int32 `json:"contender"`
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

type ClusterCapacityReportContainerBreakdownExample struct {
	Pod     string  `json:"pod"`
	Message *string `json:"message,omitempty"`
}

type ClusterCapacityReportContainerStateBreakdown struct {
	Count   uint32                                         `json:"count"`
	Example ClusterCapacityReportContainerBreakdownExample `json:"example"`
	Reason  string                                         `json:"reason,omitempty"`
	Type    string                                         `json:"type"`
}

type ClusterCapacityReportContainerBreakdown struct {
	Name   string                                         `json:"name"`
	States []ClusterCapacityReportContainerStateBreakdown `json:"states"`
}

type ClusterCapacityReportBreakdown struct {
	Containers []ClusterCapacityReportContainerBreakdown `json:"containers,omitempty"`
	Count      uint32                                    `json:"count"`
	Reason     string                                    `json:"reason,omitempty"`
	Status     string                                    `json:"status"`
	Type       string                                    `json:"type"`
}

type ClusterCapacityReportOwner struct {
	Name string `json:"name"`
}

type ClusterCapacityReport struct {
	Owner     ClusterCapacityReportOwner       `json:"owner"`
	Breakdown []ClusterCapacityReportBreakdown `json:"breakdown,omitempty"`
}

type ClusterCapacityStatus struct {
	Name              string                     `json:"name"`
	AvailableReplicas int32                      `json:"availableReplicas"`
	AchievedPercent   int32                      `json:"achievedPercent"`
	SadPods           []PodStatus                `json:"sadPods,omitempty"`
	Conditions        []ClusterCapacityCondition `json:"conditions,omitempty"`
	Reports           []ClusterCapacityReport    `json:"reports,omitempty"`
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
	Name              string `json:"name"`
	Percent           int32  `json:"percent"`
	TotalReplicaCount int32  `json:"totalReplicaCount"`
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
	AchievedTraffic uint32                    `json:"achievedTraffic"`
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
	Clusters []ClusterTrafficTarget `json:"clusters"`
}

type ClusterTrafficTarget struct {
	Name string `json:"name"`
	// apimachinery intstr for percentages?
	Weight uint32 `json:"weight"`
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
