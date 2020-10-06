package builder

import (
	"testing"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

func TestEmptyReport(t *testing.T) {

	ownerName := "owner"

	actual := NewReport(ownerName).Build()
	expected := &shipper.ClusterCapacityReport{
		Owner: shipper.ClusterCapacityReportOwner{Name: ownerName},
	}

	text, err := shippertesting.YamlDiff(expected, actual)
	if err != nil {
		t.Errorf("an error occurred: %s", err)
	}
	if len(text) > 0 {
		t.Errorf("expected is different from actual:\n%s", text)
	}
}

func TestReportOneContainerOnePodOneCondition(t *testing.T) {
	ownerName := "owner"
	actual := NewReport(ownerName).
		AddPodConditionBreakdownBuilder(
			NewPodConditionBreakdown(1, "Ready", "True", "").
				AddOrIncrementContainerState("app", "pod-a", "Ready", "", "")).
		Build()
	expected := &shipper.ClusterCapacityReport{
		Owner: shipper.ClusterCapacityReportOwner{Name: ownerName},
		Breakdown: []shipper.ClusterCapacityReportBreakdown{
			{
				Type:   "Ready",
				Status: "True",
				Count:  1,
				Containers: []shipper.ClusterCapacityReportContainerBreakdown{
					{
						Name: "app", States: []shipper.ClusterCapacityReportContainerStateBreakdown{
							{
								Count:  1,
								Type:   "Ready",
								Reason: "",
								Example: shipper.ClusterCapacityReportContainerBreakdownExample{
									Pod: "pod-a",
								},
							},
						},
					},
				},
			},
		},
	}

	text, err := shippertesting.YamlDiff(expected, actual)
	if err != nil {
		t.Errorf("an error occurred: %s", err)
	}
	if len(text) > 0 {
		t.Errorf("expected is different from actual:\n%s", text)
	}
}

func TestReportOneContainerOnePodOneConditionTerminatedWithExitCodeContainer(t *testing.T) {
	ownerName := "owner"
	actual := NewReport(ownerName).
		AddPodConditionBreakdownBuilder(
			NewPodConditionBreakdown(1, "Ready", "True", "").
				AddOrIncrementContainerState("app", "pod-a", "Ready", "", "Terminated with exit code 1")).
		Build()

	m := "Terminated with exit code 1"
	mPtr := &m

	expected := &shipper.ClusterCapacityReport{
		Owner: shipper.ClusterCapacityReportOwner{Name: ownerName},
		Breakdown: []shipper.ClusterCapacityReportBreakdown{
			{
				Type:   "Ready",
				Status: "True",
				Count:  1,
				Containers: []shipper.ClusterCapacityReportContainerBreakdown{
					{
						Name: "app", States: []shipper.ClusterCapacityReportContainerStateBreakdown{
							{
								Count:  1,
								Type:   "Ready",
								Reason: "",
								Example: shipper.ClusterCapacityReportContainerBreakdownExample{
									Pod:     "pod-a",
									Message: mPtr,
								},
							},
						},
					},
				},
			},
		},
	}

	text, err := shippertesting.YamlDiff(expected, actual)
	if err != nil {
		t.Errorf("an error occurred: %s", err)
	}
	if len(text) > 0 {
		t.Errorf("expected is different from actual:\n%s", text)
	}
}

func TestReportOneContainerTwoPodsOneCondition(t *testing.T) {
	ownerName := "owner"
	actual := NewReport(ownerName).
		AddPodConditionBreakdownBuilder(
			NewPodConditionBreakdown(2, "Ready", "True", "").
				AddOrIncrementContainerState("app", "pod-a", "Ready", "", "").
				AddOrIncrementContainerState("app", "pod-b", "Ready", "", "")).
		Build()

	expected := &shipper.ClusterCapacityReport{
		Owner: shipper.ClusterCapacityReportOwner{Name: ownerName},
		Breakdown: []shipper.ClusterCapacityReportBreakdown{
			{
				Type:   "Ready",
				Status: "True",
				Count:  2,
				Containers: []shipper.ClusterCapacityReportContainerBreakdown{
					{
						Name: "app", States: []shipper.ClusterCapacityReportContainerStateBreakdown{
							{
								Count:  2,
								Type:   "Ready",
								Reason: "",
								Example: shipper.ClusterCapacityReportContainerBreakdownExample{
									Pod: "pod-a",
								},
							},
						},
					},
				},
			},
		},
	}

	text, err := shippertesting.YamlDiff(expected, actual)
	if err != nil {
		t.Errorf("an error occurred: %s", err)
	}
	if len(text) > 0 {
		t.Errorf("expected is different from actual:\n%s", text)
	}
}

func TestReportTwoContainersTwoPodsOneCondition(t *testing.T) {
	ownerName := "owner"
	actual := NewReport(ownerName).
		AddPodConditionBreakdownBuilder(
			NewPodConditionBreakdown(2, "Ready", "True", "").
				AddOrIncrementContainerState("app", "pod-a", "Ready", "", "").
				AddOrIncrementContainerState("app", "pod-b", "Ready", "", "").
				AddOrIncrementContainerState("nginx", "pod-a", "Ready", "", "").
				AddOrIncrementContainerState("nginx", "pod-b", "Ready", "", "")).
		Build()

	expected := &shipper.ClusterCapacityReport{
		Owner: shipper.ClusterCapacityReportOwner{Name: ownerName},
		Breakdown: []shipper.ClusterCapacityReportBreakdown{
			{
				Type:   "Ready",
				Status: "True",
				Count:  2,
				Containers: []shipper.ClusterCapacityReportContainerBreakdown{
					{
						Name: "app", States: []shipper.ClusterCapacityReportContainerStateBreakdown{
							{
								Count:  2,
								Type:   "Ready",
								Reason: "",
								Example: shipper.ClusterCapacityReportContainerBreakdownExample{
									Pod: "pod-a",
								},
							},
						},
					},

					{
						Name: "nginx", States: []shipper.ClusterCapacityReportContainerStateBreakdown{
							{
								Count:  2,
								Type:   "Ready",
								Reason: "",
								Example: shipper.ClusterCapacityReportContainerBreakdownExample{
									Pod: "pod-a",
								},
							},
						},
					},
				},
			},
		},
	}

	text, err := shippertesting.YamlDiff(expected, actual)
	if err != nil {
		t.Errorf("an error occurred: %s", err)
	}
	if len(text) > 0 {
		t.Errorf("expected is different from actual:\n%s", text)
	}
}

func TestReportTwoContainersTwoPodsTwoConditions(t *testing.T) {
	ownerName := "owner"
	actual := NewReport(ownerName).
		AddPodConditionBreakdownBuilder(
			NewPodConditionBreakdown(2, "Ready", "True", "").
				AddOrIncrementContainerState("app", "pod-a", "Ready", "", "").
				AddOrIncrementContainerState("app", "pod-b", "Ready", "", "").
				AddOrIncrementContainerState("nginx", "pod-a", "Ready", "", "").
				AddOrIncrementContainerState("nginx", "pod-b", "Ready", "", "")).AddPodConditionBreakdownBuilder(
		NewPodConditionBreakdown(2, "PodInitialized", "True", "").
			AddOrIncrementContainerState("app", "pod-a", "Ready", "", "").
			AddOrIncrementContainerState("app", "pod-b", "Ready", "", "").
			AddOrIncrementContainerState("nginx", "pod-a", "Ready", "", "").
			AddOrIncrementContainerState("nginx", "pod-b", "Ready", "", "")).
		Build()

	expected := &shipper.ClusterCapacityReport{
		Owner: shipper.ClusterCapacityReportOwner{Name: ownerName},
		Breakdown: []shipper.ClusterCapacityReportBreakdown{
			{
				Type:   "PodInitialized",
				Status: "True",
				Count:  2,
				Containers: []shipper.ClusterCapacityReportContainerBreakdown{
					{
						Name: "app", States: []shipper.ClusterCapacityReportContainerStateBreakdown{
							{
								Count:  2,
								Type:   "Ready",
								Reason: "",
								Example: shipper.ClusterCapacityReportContainerBreakdownExample{
									Pod: "pod-a",
								},
							},
						},
					},

					{
						Name: "nginx", States: []shipper.ClusterCapacityReportContainerStateBreakdown{
							{
								Count:  2,
								Type:   "Ready",
								Reason: "",
								Example: shipper.ClusterCapacityReportContainerBreakdownExample{
									Pod: "pod-a",
								},
							},
						},
					},
				},
			},

			{
				Type:   "Ready",
				Status: "True",
				Count:  2,
				Containers: []shipper.ClusterCapacityReportContainerBreakdown{
					{
						Name: "app", States: []shipper.ClusterCapacityReportContainerStateBreakdown{
							{
								Count:  2,
								Type:   "Ready",
								Reason: "",
								Example: shipper.ClusterCapacityReportContainerBreakdownExample{
									Pod: "pod-a",
								},
							},
						},
					},

					{
						Name: "nginx", States: []shipper.ClusterCapacityReportContainerStateBreakdown{
							{
								Count:  2,
								Type:   "Ready",
								Reason: "",
								Example: shipper.ClusterCapacityReportContainerBreakdownExample{
									Pod: "pod-a",
								},
							},
						},
					},
				},
			},
		},
	}

	text, err := shippertesting.YamlDiff(expected, actual)
	if err != nil {
		t.Errorf("an error occurred: %s", err)
	}
	if len(text) > 0 {
		t.Errorf("expected is different from actual:\n%s", text)
	}
}
