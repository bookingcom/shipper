package release

import (
	"math"
	"sort"

	"github.com/cespare/xxhash"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

const (
	defaultClusterWeight = 100
)

type scoredCluster struct {
	cluster *shipper.Cluster
	score   float64
}

func buildPrefList(appIdentity string, clusterList []*shipper.Cluster) []*shipper.Cluster {
	/*
		This part is a bit subtle: we're creating a preference list of clusters
		by creating a sorting key composed of a hash of the Application name
		plus the cluster seed (defaulting to cluster name). This list is sorted
		by that key, and the resulting order is the "preference list" for this
		Application when it is choosing clusters. This is called the preference
		list because it is the order in which clusters will be selected for
		this Application. All other scheduling concerns (unschedulable,
		capability, capacity) just _mask_ this initial list by skipping over
		entries when requirements are not met.

		By using a good hash function and a key specific to this Application,
		we get distribution of Applications across clusters (load balancing).

		By making that key deterministic to the Cluster and Application names,
		we can be sure that this Application will continue to be scheduled to
		the same set of clusters from Release to Release. Furthermore, we
		ensure that any addition or removal of clusters has minimal impact on
		the overall set of clusters this Application resides on.

		Finally, this approach provides the possibility to raise or lower
		clusters in the preference list by weighting them, which is a valuable
		tool for managing capacity or incrementally phasing out clusters.

		This is Highest Random Weight Hashing, also known as Rendezvous
		hashing: https://en.wikipedia.org/wiki/Rendezvous_hashing.

		By transforming the hash value using a weight, it becomes an
		implementation of weighted rendezvous hashing, per
		http://www.snia.org/sites/default/files/SDC15_presentations/dist_sys/Jason_Resch_New_Consistent_Hashings_Rev.pdf.

		The weight enables system operators to tune the position of a cluster
		in the preflist. A high enough value will ensure that all Applications
		try the cluster first, while 0 will put the cluster at the end of the
		preflist.
	*/
	scoredClusters := make([]scoredCluster, 0, len(clusterList))
	for _, cluster := range clusterList {
		var clusterIdentity string

		if cluster.Spec.Scheduler.Identity == nil {
			clusterIdentity = cluster.Name
		} else {
			clusterIdentity = *cluster.Spec.Scheduler.Identity
		}

		var weight int32
		if cluster.Spec.Scheduler.Weight == nil {
			weight = defaultClusterWeight
		} else {
			weight = *cluster.Spec.Scheduler.Weight
		}

		hash := xxhash.New()
		hash.Write([]byte(appIdentity))
		hash.Write([]byte(clusterIdentity))
		sum := hash.Sum64()

		score := float64(-weight) * math.Log(float64(sum)/float64(math.MaxUint64))

		// Decorate.
		scoredClusters = append(scoredClusters, scoredCluster{cluster: cluster, score: score})
	}

	// Descending sort so highest scores are first (as you'd expect from higher
	// weight).
	sort.Slice(scoredClusters, func(i, j int) bool {
		return scoredClusters[j].score < scoredClusters[i].score
	})

	// Undecorate.
	prefList := make([]*shipper.Cluster, 0, len(scoredClusters))
	for _, scoredCluster := range scoredClusters {
		prefList = append(prefList, scoredCluster.cluster)
	}

	return prefList
}
