package metrics

import (
	"testing"
	"time"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMetricsControllerCreateAndUpdatesCache(t *testing.T) {
	f := shippertesting.NewControllerTestFixture()
	c := runController(f)

	// adds the app to the cache
	creationTime := parseTime("Jan 22 2021 08:38:22")
	creationTimeWrapped := metav1.NewTime(creationTime)
	app := &shipper.Application{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: creationTimeWrapped,
			Name:              "unit-test-app",
		},
	}

	c.handleAppCreates(app)
	entry, ok := c.appLastModifiedTimes[app.Name]
	if !ok {
		t.Fatalf("expected %q to be in cache, got %v", app.Name, c.appLastModifiedTimes)
	}
	if !entry.time.Equal(creationTime) {
		t.Fatalf("expected time to be the same as creation time\nentry.time = %s\ncreationTime = %s\n", entry.time, creationTime)
	}

	// if the spec doesn't change than ignore the update
	appWithLabels := &shipper.Application{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: app.ObjectMeta.CreationTimestamp,
			Name:              "unit-test-app",
			Labels: map[string]string{
				"random": "label",
			},
		},
	}
	c.handleAppUpdates(app, appWithLabels)
	entry, ok = c.appLastModifiedTimes[app.Name]
	if !ok {
		t.Fatalf("expected %q to be in cache, got %v", app.Name, c.appLastModifiedTimes)
	}
	if !entry.time.Equal(creationTime) {
		t.Fatalf("expected time to be the same as creation time\nentry.time = %s\ncreationTime = %s\n", entry.time, creationTime)
	}

	// if the spec does change update the cache entry
	var one int32 = 1
	appWithSpec := &shipper.Application{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: app.ObjectMeta.CreationTimestamp,
			Name:              "unit-test-app",
		},
		Spec: shipper.ApplicationSpec{
			RevisionHistoryLimit: &one,
		},
	}
	c.handleAppUpdates(app, appWithSpec)
	entry, ok = c.appLastModifiedTimes[app.Name]
	if !ok {
		t.Fatalf("expected %q to be in cache, got %v", app.Name, c.appLastModifiedTimes)
	}
	if entry.time.Equal(creationTime) {
		t.Fatalf("expected time to be the differt from the creation time\nentry.time = %s\ncreationTime = %s\n", entry.time, creationTime)
	}
}

func TestMetricsControllerCalculateTimeToInstall(t *testing.T) {
	app := &shipper.Application{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.NewTime(parseTime("Jan 22 2021 08:38:22")),
			Name:              "unit-test-app",
			Namespace:         "unit-test-ns",
		},
	}
	oldRelease := &shipper.Release{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unit-test",
			Namespace: "unit-test-ns",
			Labels: map[string]string{
				shipper.AppLabel: "unit-test-app",
			},
			Annotations: map[string]string{
				shipper.ReleaseGenerationAnnotation: "1",
			},
		},
		Status: shipper.ReleaseStatus{
			Strategy: &shipper.ReleaseStrategyStatus{
				Conditions: []shipper.ReleaseStrategyCondition{
					{
						Type:   shipper.StrategyConditionContenderAchievedInstallation,
						Status: corev1.ConditionFalse,
					},
				},
			},
		},
	}
	newRelease := &shipper.Release{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unit-test",
			Namespace: "unit-test-ns",
			Labels: map[string]string{
				shipper.AppLabel: "unit-test-app",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					Name: "unit-test-app",
				},
			},
		},
		Spec: shipper.ReleaseSpec{
			TargetStep: 0,
		},
		Status: shipper.ReleaseStatus{
			Strategy: &shipper.ReleaseStrategyStatus{
				Conditions: []shipper.ReleaseStrategyCondition{
					{
						Type:               shipper.StrategyConditionContenderAchievedInstallation,
						Status:             corev1.ConditionTrue,
						LastTransitionTime: metav1.NewTime(parseTime("Jan 22 2021 09:38:22")),
					},
				},
			},
		},
	}

	f := shippertesting.NewControllerTestFixture(app, oldRelease)
	c := runController(f)

	// adds the app to the cache
	c.handleAppCreates(app)

	// calculate time to installation
	c.handleReleaseUpdates(oldRelease, newRelease)

	// get the metrics
	fakeHistogram := c.metricsBundle.TimeToInstallation.(*fakeHistogram)
	if len(fakeHistogram.data) != 1 {
		t.Fatalf("expected exactly one metric, got %v", fakeHistogram.data)
	}
	if fakeHistogram.data[0] != 3600.0 {
		t.Fatalf("expected installation time to be 3600s but got %f", fakeHistogram.data[0])
	}
}

func runController(f *shippertesting.ControllerTestFixture) *Controller {
	c := NewController(
		f.ShipperClient,
		f.ShipperInformerFactory,
		fakeMetricsBundle(),
	)

	stopCh := make(chan struct{})
	defer close(stopCh)

	f.Run(stopCh)

	return c
}

func parseTime(str string) time.Time {
	layout := "Jan 02 2006 15:04:05"
	t, _ := time.Parse(layout, str)
	return t
}

func fakeMetricsBundle() *MetricsBundle {
	return &MetricsBundle{
		TimeToInstallation: &fakeHistogram{
			data: make([]float64, 0),
		},
	}
}

type fakeHistogram struct {
	data []float64
}

func (f *fakeHistogram) Observe(m float64) {
	f.data = append(f.data, m)
}

func (f *fakeHistogram) Describe(chan<- *prometheus.Desc) {}
func (f *fakeHistogram) Collect(chan<- prometheus.Metric) {}
func (f *fakeHistogram) Desc() *prometheus.Desc           { return nil }
func (f *fakeHistogram) Write(*dto.Metric) error          { return nil }
