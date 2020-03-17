package release

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

const (
	passingCase = false
	errorCase   = true
)

type requirements shipper.ClusterRequirements
type clusters []shipper.ClusterSpec
type expected []string

// TestComputeTargetClusters works the core of the scheduler logic: matching
// regions and capabilities between releases and clusters.
// NOTE: the "expected" clusters are due to the particular prefList outcomes,
// and as such should be expected to break if we change the hash function for
// the preflist.
func TestComputeTargetClusters(t *testing.T) {
	computeClusterTestCase(t, "error when no regions specified",
		requirements{
			Regions: []shipper.RegionRequirement{},
		},
		clusters{
			{Region: shippertesting.TestRegion, Capabilities: []string{}},
		},
		expected{},
		errorCase,
	)

	computeClusterTestCase(t, "basic region match",
		requirements{
			Regions: []shipper.RegionRequirement{{Name: "matches"}},
		},
		clusters{
			{Region: "matches", Capabilities: []string{}},
		},
		expected{"cluster-0"},
		passingCase,
	)

	computeClusterTestCase(t, "one region match one no match",
		requirements{
			Regions: []shipper.RegionRequirement{{Name: "matches"}},
		},
		clusters{
			{Region: "matches", Capabilities: []string{}},
			{Region: "does not match", Capabilities: []string{}},
		},
		expected{"cluster-0"},
		passingCase,
	)

	computeClusterTestCase(t, "both match",
		requirements{
			Regions: []shipper.RegionRequirement{{Name: "matches", Replicas: pint32(2)}},
		},
		clusters{
			{Region: "matches", Capabilities: []string{}},
			{Region: "matches", Capabilities: []string{}},
		},
		expected{"cluster-0", "cluster-1"},
		passingCase,
	)

	computeClusterTestCase(t, "two region matches, one capability match",
		requirements{
			Regions:      []shipper.RegionRequirement{{Name: "matches"}},
			Capabilities: []string{"a", "b"},
		},
		clusters{
			{Region: "matches", Capabilities: []string{"a"}},
			{Region: "matches", Capabilities: []string{"a", "b"}},
		},
		expected{"cluster-1"},
		passingCase,
	)

	computeClusterTestCase(t, "two region matches, two capability matches",
		requirements{
			Regions:      []shipper.RegionRequirement{{Name: "matches", Replicas: pint32(2)}},
			Capabilities: []string{"a"},
		},
		clusters{
			{Region: "matches", Capabilities: []string{"a"}},
			{Region: "matches", Capabilities: []string{"a", "b"}},
		},
		expected{"cluster-0", "cluster-1"},
		passingCase,
	)

	computeClusterTestCase(t, "no region match",
		requirements{
			Regions:      []shipper.RegionRequirement{{Name: "foo"}},
			Capabilities: []string{},
		},
		clusters{
			{Region: "bar", Capabilities: []string{}},
			{Region: "baz", Capabilities: []string{}},
		},
		expected{},
		errorCase,
	)

	computeClusterTestCase(t, "region match, no capability match",
		requirements{
			Regions:      []shipper.RegionRequirement{{Name: "foo"}},
			Capabilities: []string{"a"},
		},
		clusters{
			{Region: "foo", Capabilities: []string{"b"}},
			{Region: "foo", Capabilities: []string{"b"}},
		},
		expected{},
		errorCase,
	)

	computeClusterTestCase(t, "reject duplicate capabilities in requirements",
		requirements{
			Regions:      []shipper.RegionRequirement{{Name: "foo"}},
			Capabilities: []string{"a", "a"},
		},
		clusters{
			{Region: "foo", Capabilities: []string{"a"}},
		},
		expected{},
		errorCase,
	)

	computeClusterTestCase(t, "more clusters than needed, pick only one from each region",
		requirements{
			Regions: []shipper.RegionRequirement{
				{Name: "us-east", Replicas: pint32(1)},
				{Name: "eu-west", Replicas: pint32(1)},
			},
			Capabilities: []string{"a"},
		},
		clusters{
			{Region: "us-east", Capabilities: []string{"a"}},
			{Region: "us-east", Capabilities: []string{"a"}},
			{Region: "eu-west", Capabilities: []string{"a"}},
			{Region: "eu-west", Capabilities: []string{"a"}},
		},
		expected{"cluster-1", "cluster-2"},
		passingCase,
	)

	computeClusterTestCase(t, "different replica counts by region",
		requirements{
			Regions: []shipper.RegionRequirement{
				{Name: "us-east", Replicas: pint32(2)},
				{Name: "eu-west", Replicas: pint32(1)},
			},
			Capabilities: []string{"a"},
		},
		clusters{
			{Region: "us-east", Capabilities: []string{"a"}},
			{Region: "us-east", Capabilities: []string{"a"}},
			{Region: "eu-west", Capabilities: []string{"a"}},
			{Region: "eu-west", Capabilities: []string{"a"}},
		},
		expected{"cluster-0", "cluster-1", "cluster-2"},
		passingCase,
	)

	computeClusterTestCase(t, "skip unschedulable clusters",
		requirements{
			Regions: []shipper.RegionRequirement{
				{Name: "us-east", Replicas: pint32(2)},
			},
		},
		clusters{
			{
				Region:    "us-east",
				Scheduler: shipper.ClusterSchedulerSettings{Unschedulable: true},
			},
			{
				Region:    "us-east",
				Scheduler: shipper.ClusterSchedulerSettings{Unschedulable: true},
			},
			{Region: "us-east"},
		},
		expected{},
		errorCase,
	)

	computeClusterTestCase(t, "heavy weight changes normal priority",
		requirements{
			Regions: []shipper.RegionRequirement{
				{Name: "us-east", Replicas: pint32(1)},
				{Name: "eu-west", Replicas: pint32(1)},
			},
			Capabilities: []string{"a"},
		},
		clusters{
			{
				Region:       "us-east",
				Capabilities: []string{"a"},
				Scheduler:    shipper.ClusterSchedulerSettings{Weight: pint32(900)},
			},
			{Region: "us-east", Capabilities: []string{"a"}},
			{Region: "eu-west", Capabilities: []string{"a"}},
			{
				Region:       "eu-west",
				Capabilities: []string{"a"},
				Scheduler:    shipper.ClusterSchedulerSettings{Weight: pint32(900)},
			},
		},
		// This test is identical to "more clusters than needed", and without weight
		// would yield the same result (cluster-1, cluster-2).
		expected{"cluster-0", "cluster-3"},
		passingCase,
	)

	computeClusterTestCase(t, "a little weight doesn't change things",
		requirements{
			Regions: []shipper.RegionRequirement{
				{Name: "us-east", Replicas: pint32(1)},
				{Name: "eu-west", Replicas: pint32(1)},
			},
			Capabilities: []string{"a"},
		},
		clusters{
			{
				Region:       "us-east",
				Capabilities: []string{"a"},
				Scheduler:    shipper.ClusterSchedulerSettings{Weight: pint32(101)},
			},
			{Region: "us-east", Capabilities: []string{"a"}},
			{Region: "eu-west", Capabilities: []string{"a"}},
			{Region: "eu-west", Capabilities: []string{"a"}},
		},
		// Weight doesn't change things unless it is "heavy" enough: it needs to
		// overcome the natural distribution of hash values. This test is identical to
		// "more clusters than needed", and has a minimal (ineffectual) weight
		// applied, so it gives the same result as that test.
		expected{"cluster-1", "cluster-2"},
		passingCase,
	)

	computeClusterTestCase(t, "colliding identity plus a little weight does change things",
		requirements{
			Regions: []shipper.RegionRequirement{
				{Name: "us-east", Replicas: pint32(1)},
				{Name: "eu-west", Replicas: pint32(1)},
			},
			Capabilities: []string{"a"},
		},
		clusters{
			// The "identity" means that cluster-0 computes the hash exactly like
			// cluster-1, so a minimal bump in weight puts it in front.
			{
				Region:       "us-east",
				Capabilities: []string{"a"},
				Scheduler: shipper.ClusterSchedulerSettings{
					Identity: pstr("cluster-1"),
					Weight:   pint32(101),
				},
			},
			{Region: "us-east", Capabilities: []string{"a"}},
			{Region: "eu-west", Capabilities: []string{"a"}},
			{Region: "eu-west", Capabilities: []string{"a"}},
		},
		expected{"cluster-0", "cluster-2"},
		passingCase,
	)
}

func generateClusterForTestCase(name int, spec shipper.ClusterSpec) *shipper.Cluster {
	return &shipper.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("cluster-%d", name),
			Namespace: shippertesting.TestNamespace,
		},
		Spec: spec,
	}
}

func generateReleaseForTestCase(reqs shipper.ClusterRequirements) *shipper.Release {
	return &shipper.Release{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-release",
			Namespace: shippertesting.TestNamespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Kind:       "Application",
					Name:       "test-application",
				},
			},
		},
		Spec: shipper.ReleaseSpec{
			Environment: shipper.ReleaseEnvironment{
				ClusterRequirements: reqs,
			},
		},
	}
}

func pstr(s string) *string {
	return &s
}

func computeClusterTestCase(
	t *testing.T,
	name string,
	reqs requirements,
	clusterSpecs clusters,
	expectedClusters expected,
	expectError bool,
) {

	release := generateReleaseForTestCase(shipper.ClusterRequirements(reqs))
	clusters := make([]*shipper.Cluster, 0, len(clusterSpecs))
	for i, spec := range clusterSpecs {
		clusters = append(clusters, generateClusterForTestCase(i, spec))
	}

	actualClusters, err := computeTargetClusters(release, clusters)
	if expectError {
		if err == nil {
			t.Errorf("test %q expected an error but didn't get one!", name)
		}
	} else {
		if err != nil {
			t.Errorf("error %q: %q", name, err)
			return
		}
	}

	actualClusterNames := make([]string, 0, len(actualClusters))
	for _, cluster := range actualClusters {
		actualClusterNames = append(actualClusterNames, cluster.GetName())
	}
	sort.Strings(actualClusterNames)

	if strings.Join(expectedClusters, ",") != strings.Join(actualClusterNames, ",") {
		t.Errorf("%q expected clusters %q, but got %q", name, strings.Join(expectedClusters, ","), strings.Join(actualClusterNames, ","))
		return
	}
}
