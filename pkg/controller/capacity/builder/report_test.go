package builder

import (
	"github.com/pmezard/go-difflib/difflib"
	"gopkg.in/yaml.v2"
	"testing"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func yamlDiff(a interface{}, b interface{}) (string, error) {
	yamlActual, _ := yaml.Marshal(a)
	yamlExpected, _ := yaml.Marshal(b)

	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(yamlExpected)),
		B:        difflib.SplitLines(string(yamlActual)),
		FromFile: "Expected",
		ToFile:   "Actual",
		Context:  4,
	}

	return difflib.GetUnifiedDiffString(diff)
}

func TestEmptyReport(t *testing.T) {

	ownerName := "owner"

	actual := NewReport(ownerName).Build()
	expected := &shipper.ClusterCapacityReport{
		Owner: shipper.ClusterCapacityReportOwner{Name: ownerName},
	}

	text, err := yamlDiff(expected, actual)
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
				AddContainerState("app", 1, "pod-a", "Ready", "", "")).
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

	text, err := yamlDiff(expected, actual)
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
				AddContainerState("app", 1, "pod-a", "Ready", "", "Terminated with exit code 1")).
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

	text, err := yamlDiff(expected, actual)
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
				AddContainerState("app", 1, "pod-a", "Ready", "", "").
				AddContainerState("app", 1, "pod-b", "Ready", "", "")).
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

	text, err := yamlDiff(expected, actual)
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
				AddContainerState("app", 1, "pod-a", "Ready", "", "").
				AddContainerState("app", 1, "pod-b", "Ready", "", "").
				AddContainerState("nginx", 1, "pod-a", "Ready", "", "").
				AddContainerState("nginx", 1, "pod-b", "Ready", "", "")).
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

	text, err := yamlDiff(expected, actual)
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
				AddContainerState("app", 1, "pod-a", "Ready", "", "").
				AddContainerState("app", 1, "pod-b", "Ready", "", "").
				AddContainerState("nginx", 1, "pod-a", "Ready", "", "").
				AddContainerState("nginx", 1, "pod-b", "Ready", "", "")).AddPodConditionBreakdownBuilder(
		NewPodConditionBreakdown(2, "PodInitialized", "True", "").
			AddContainerState("app", 1, "pod-a", "Ready", "", "").
			AddContainerState("app", 1, "pod-b", "Ready", "", "").
			AddContainerState("nginx", 1, "pod-a", "Ready", "", "").
			AddContainerState("nginx", 1, "pod-b", "Ready", "", "")).
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

	text, err := yamlDiff(expected, actual)
	if err != nil {
		t.Errorf("an error occurred: %s", err)
	}
	if len(text) > 0 {
		t.Errorf("expected is different from actual:\n%s", text)
	}
}
