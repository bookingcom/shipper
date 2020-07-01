package v1alpha1

import (
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ShipperNamespace            = "shipper-system"
	GlobalRolloutBlockNamespace = "rollout-blocks-global"

	ShipperManagementServiceAccount  = "shipper-mgmt-cluster"
	ShipperApplicationServiceAccount = "shipper-app-cluster"

	ReleaseLabel                 = "shipper-release"
	AppLabel                     = "shipper-app"
	ReleaseEnvironmentHashLabel  = "shipper-release-hash"
	PodTrafficStatusLabel        = "shipper-traffic-status"
	InstallationTargetOwnerLabel = "shipper-owned-by"
	MigrationLabel               = "shipper.booking.com/target.object.migration.0.9.completed" // 57 characters. careful, keep less then 63 characters.

	AppHighestObservedGenerationAnnotation = "shipper.booking.com/app.highestObservedGeneration"

	AppChartNameAnnotation            = "shipper.booking.com/app.chart.name"
	AppChartVersionResolvedAnnotation = "shipper.booking.com/app.chart.version.resolved"
	AppChartVersionRawAnnotation      = "shipper.booking.com/app.chart.version.raw"

	ReleaseGenerationAnnotation        = "shipper.booking.com/release.generation"
	ReleaseTemplateIterationAnnotation = "shipper.booking.com/release.template.iteration"
	ReleaseClustersAnnotation          = "shipper.booking.com/release.clusters"

	SecretClusterSkipTlsVerifyAnnotation = "shipper.booking.com/cluster-secret.insecure-tls-skip-verify"

	RolloutBlocksOverrideAnnotation = "shipper.booking.com/rollout-block.override"

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
	ApplicationConditionTypeBlocked       ApplicationConditionType = "Blocked"
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

// A Cluster is a cluster we're deploying to.
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

// A Release  defines the goal state for # of pods for incumbent and
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
	ReleaseConditionTypeClustersChosen   ReleaseConditionType = "ClustersChosen"
	ReleaseConditionTypeStrategyExecuted ReleaseConditionType = "StrategyExecuted"
	ReleaseConditionTypeComplete         ReleaseConditionType = "Complete"
	ReleaseConditionTypeBlocked          ReleaseConditionType = "Blocked"
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
	Values ChartValues `json:"values"`

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

type TargetConditionType string

const (
	TargetConditionTypeOperational TargetConditionType = "Operational"
	TargetConditionTypeReady       TargetConditionType = "Ready"
)

type TargetCondition struct {
	Type               TargetConditionType    `json:"type"`
	Status             corev1.ConditionStatus `json:"status"`
	LastTransitionTime metav1.Time            `json:"lastTransitionTime,omitempty"`
	Reason             string                 `json:"reason,omitempty"`
	Message            string                 `json:"message,omitempty"`
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
	Conditions []TargetCondition `json:"conditions,omitempty"`

	// Deprecated
	Clusters []*ClusterInstallationStatus `json:"clusters,omitempty"`
}

// Deprecated
type ClusterInstallationStatus struct {
	Name       string                         `json:"name"`
	Conditions []ClusterInstallationCondition `json:"conditions,omitempty"`
}

// Deprecated
type ClusterInstallationCondition struct {
	Type               ClusterConditionType   `json:"type"`
	Status             corev1.ConditionStatus `json:"status"`
	LastTransitionTime metav1.Time            `json:"lastTransitionTime,omitempty"`
	Reason             string                 `json:"reason,omitempty"`
	Message            string                 `json:"message,omitempty"`
}

type InstallationTargetSpec struct {
	CanOverride bool        `json:"canOverride"`
	Chart       Chart       `json:"chart"`
	Values      ChartValues `json:"values,omitempty"`

	// Deprecated
	Clusters []string `json:"clusters,omitempty"`
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

type CapacityTargetSpec struct {
	Percent           int32 `json:"percent"`
	TotalReplicaCount int32 `json:"totalReplicaCount"`

	// Deprecated
	Clusters []ClusterCapacityTarget `json:"clusters,omitempty"`
}

// Deprecated
type ClusterCapacityTarget struct {
	Name              string `json:"name"`
	Percent           int32  `json:"percent"`
	TotalReplicaCount int32  `json:"totalReplicaCount"`
}

type CapacityTargetStatus struct {
	ObservedGeneration int64             `json:"observedGeneration,omitempty"`
	AvailableReplicas  int32             `json:"availableReplicas"`
	AchievedPercent    int32             `json:"achievedPercent"`
	SadPods            []PodStatus       `json:"sadPods,omitempty"`
	Conditions         []TargetCondition `json:"conditions,omitempty"`

	// Deprecated
	Clusters []ClusterCapacityStatus `json:"clusters,omitempty"`
}

// Deprecated
type ClusterCapacityStatus struct {
	Name              string                     `json:"name"`
	AvailableReplicas int32                      `json:"availableReplicas"`
	AchievedPercent   int32                      `json:"achievedPercent"`
	SadPods           []PodStatus                `json:"sadPods,omitempty"`
	Conditions        []ClusterCapacityCondition `json:"conditions,omitempty"`
}

// Deprecated
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
	ObservedGeneration int64             `json:"observedGeneration,omitempty"`
	AchievedTraffic    uint32            `json:"achievedTraffic"`
	Conditions         []TargetCondition `json:"conditions"`

	// Deprecated
	Clusters []*ClusterTrafficStatus `json:"clusters,omitempty"`
}

// Deprecated
type ClusterTrafficStatus struct {
	Name            string                    `json:"name"`
	AchievedTraffic uint32                    `json:"achievedTraffic"`
	Conditions      []ClusterTrafficCondition `json:"conditions"`
}

// Deprecated
type ClusterConditionType string

// Deprecated
type ClusterTrafficCondition struct {
	Type               ClusterConditionType   `json:"type"`
	Status             corev1.ConditionStatus `json:"status"`
	LastTransitionTime metav1.Time            `json:"lastTransitionTime,omitempty"`
	Reason             string                 `json:"reason,omitempty"`
	Message            string                 `json:"message,omitempty"`
}

type TrafficTargetSpec struct {
	// apimachinery intstr for percentages?
	Weight uint32 `json:"weight"`

	// Deprecated
	Clusters []ClusterTrafficTarget `json:"clusters,omitempty"`
}

// Deprecated
type ClusterTrafficTarget struct {
	Name string `json:"name"`
	// apimachinery intstr for percentages?
	Weight uint32 `json:"weight"`
}

type ReleaseStrategyStatus struct {
	State    ReleaseStrategyState    `json:"state,omitempty"`
	Clusters []ClusterStrategyStatus `json:"clusters,omitempty"`

	// Deprecated
	Conditions []ReleaseStrategyCondition `json:"conditions,omitempty"`
}

type ClusterStrategyStatus struct {
	Name       string                     `json:"name"`
	Conditions []ReleaseStrategyCondition `json:"conditions"`
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
	Reason             string                 `json:"reason"`
	Message            string                 `json:"message"`
	Step               int32                  `json:"step"`
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

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// A RolloutBlock defines the a state where rollouts are blocked, locally or globally.
// This is used by the ApplicationController to disable rollouts.
type RolloutBlock struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RolloutBlockSpec   `json:"spec"`
	Status RolloutBlockStatus `json:"status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type RolloutBlockList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []RolloutBlock `json:"items"`
}

type RolloutBlockStatus struct {
	Overrides RolloutBlockOverrides `json:"overrides"`
}

type RolloutBlockOverrides struct {
	Application string `json:"application"`
	Release     string `json:"release"`
}

type RolloutBlockSpec struct {
	Message string             `json:"message"`
	Author  RolloutBlockAuthor `json:"author"`
}

type RolloutBlockAuthor struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

const (
	RolloutBlockReason = "RolloutsBlocked"
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
