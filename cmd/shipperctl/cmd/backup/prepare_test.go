package backup

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

func TestBuildShipperBackupApplication(t *testing.T) {
	application := shipper.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testAppName,
			Namespace: testNamespaceName,
			UID:       appUid,
		},
		Spec: shipper.ApplicationSpec{
			RevisionHistoryLimit: nil,
			Template: shipper.ReleaseEnvironment{
				Chart: shipper.Chart{
					Name:    "",
					Version: "",
					RepoURL: "",
				},
				Values: nil,
				ClusterRequirements: shipper.ClusterRequirements{
					Regions:      nil,
					Capabilities: nil,
				},
			},
		},
		Status: shipper.ApplicationStatus{},
	}
	release := shipper.Release{
		ObjectMeta: metav1.ObjectMeta{

			Name:      testRelName,
			Namespace: testNamespaceName,
			UID:       relUid,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "",
					Kind:       "",
					Name:       "",
					UID:        appUid,
				},
			},
			Labels: map[string]string{
				shipper.AppLabel: testAppName,
			},
		},
		Spec: shipper.ReleaseSpec{
			TargetStep: 0,
			Environment: shipper.ReleaseEnvironment{
				Chart: shipper.Chart{
					Name:    "",
					Version: "",
					RepoURL: "",
				},
				Values: nil,
				ClusterRequirements: shipper.ClusterRequirements{
					Regions:      nil,
					Capabilities: nil,
				},
			},
		},
		Status: shipper.ReleaseStatus{},
	}
	installationTarget := shipper.InstallationTarget{
		ObjectMeta: metav1.ObjectMeta{

			Name:      testRelName,
			Namespace: testNamespaceName,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "",
					Kind:       "",
					Name:       "",
					UID:        relUid,
				},
			},
			Labels: map[string]string{
				shipper.ReleaseLabel: testRelName,
			},
		},
		Spec: shipper.InstallationTargetSpec{
			Clusters:    nil,
			CanOverride: false,
			Chart:       nil,
		},
		Status: shipper.InstallationTargetStatus{},
	}
	trafficTarget := shipper.TrafficTarget{
		ObjectMeta: metav1.ObjectMeta{

			Name:      testRelName,
			Namespace: testNamespaceName,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "",
					Kind:       "",
					Name:       "",
					UID:        relUid,
				},
			},
			Labels: map[string]string{
				shipper.ReleaseLabel: testRelName,
			},
		},
		Spec: shipper.TrafficTargetSpec{
			Clusters: nil,
		},
		Status: shipper.TrafficTargetStatus{},
	}
	capacityTarget := shipper.CapacityTarget{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.Time{
				Time: time.Time{},
			},
			Name:      testRelName,
			Namespace: testNamespaceName,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "",
					Kind:       "",
					Name:       "",
					UID:        relUid,
				},
			},
			Labels: map[string]string{
				shipper.ReleaseLabel: testRelName,
			},
		},
		Spec: shipper.CapacityTargetSpec{
			Clusters: nil,
		},
		Status: shipper.CapacityTargetStatus{},
	}

	tests := []struct {
		Name           string
		ExpectedBackup []shipperBackupApplication
		ExpectedError  error
		PreTest        func() (*shipperfake.Clientset, *kubefake.Clientset, error)
	}{
		{
			Name: "One application with one release tree",
			ExpectedBackup: []shipperBackupApplication{
				{
					Application: application,
					BackupReleases: []shipperBackupRelease{
						{
							Release:            release,
							InstallationTarget: installationTarget,
							TrafficTarget:      trafficTarget,
							CapacityTarget:     capacityTarget,
						},
					},
				},
			},
			ExpectedError: nil,
			PreTest: func() (*shipperfake.Clientset, *kubefake.Clientset, error) {
				shipperfakeClient := shipperfake.NewSimpleClientset()
				kubefakeClient := kubefake.NewSimpleClientset()
				namespace := corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: testNamespaceName,
					},
				}
				if _, err := kubefakeClient.CoreV1().Namespaces().Create(&namespace); err != nil {
					return nil, nil, err
				}
				if _, err := shipperfakeClient.ShipperV1alpha1().Applications(testNamespaceName).Create(&application); err != nil {
					return nil, nil, err
				}
				if _, err := shipperfakeClient.ShipperV1alpha1().Releases(testNamespaceName).Create(&release); err != nil {
					return nil, nil, err
				}
				if _, err := shipperfakeClient.ShipperV1alpha1().InstallationTargets(testNamespaceName).Create(&installationTarget); err != nil {
					return nil, nil, err
				}
				if _, err := shipperfakeClient.ShipperV1alpha1().TrafficTargets(testNamespaceName).Create(&trafficTarget); err != nil {
					return nil, nil, err
				}
				if _, err := shipperfakeClient.ShipperV1alpha1().CapacityTargets(testNamespaceName).Create(&capacityTarget); err != nil {
					return nil, nil, err
				}

				return shipperfakeClient, kubefakeClient, nil
			},
		},
		{
			Name:           "One application with release and missing target objects",
			ExpectedBackup: nil,
			ExpectedError: fmt.Errorf(
				"Failed to retrieve some objects:\n - expected 1 shipper.booking.com/v1alpha1, Kind=%s for selector \"%s=%s\", got %d instead\n",
				"InstallationTarget",
				shipper.ReleaseLabel,
				testRelName,
				0,
			),
			PreTest: func() (*shipperfake.Clientset, *kubefake.Clientset, error) {
				shipperfakeClient := shipperfake.NewSimpleClientset()
				kubefakeClient := kubefake.NewSimpleClientset()
				namespace := corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: testNamespaceName,
					},
				}
				if _, err := kubefakeClient.CoreV1().Namespaces().Create(&namespace); err != nil {
					return nil, nil, err
				}
				if _, err := shipperfakeClient.ShipperV1alpha1().Applications(testNamespaceName).Create(&application); err != nil {
					return nil, nil, err
				}
				if _, err := shipperfakeClient.ShipperV1alpha1().Releases(testNamespaceName).Create(&release); err != nil {
					return nil, nil, err
				}

				return shipperfakeClient, kubefakeClient, nil
			},
		},
		{
			Name: "One application with no release or target objects",
			ExpectedBackup: []shipperBackupApplication{
				{
					Application:    application,
					BackupReleases: []shipperBackupRelease{},
				},
			},
			ExpectedError: nil,
			PreTest: func() (*shipperfake.Clientset, *kubefake.Clientset, error) {
				shipperfakeClient := shipperfake.NewSimpleClientset()
				kubefakeClient := kubefake.NewSimpleClientset()
				namespace := corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: testNamespaceName,
					},
				}
				if _, err := kubefakeClient.CoreV1().Namespaces().Create(&namespace); err != nil {
					return nil, nil, err
				}
				if _, err := shipperfakeClient.ShipperV1alpha1().Applications(testNamespaceName).Create(&application); err != nil {
					return nil, nil, err
				}

				return shipperfakeClient, kubefakeClient, nil
			},
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			shipperFakeClient, kubefakeClient, err := test.PreTest()
			if err != nil {
				t.Fatalf("unexpected error \n%v", err)
			}
			actualBackup, actualErr := buildShipperBackupApplication(kubefakeClient, shipperFakeClient)
			if !reflect.DeepEqual(actualErr, test.ExpectedError) {
				t.Fatalf("expected error \n%v\ngot error \n%v", test.ExpectedError, actualErr)
			}
			eq, diff := shippertesting.DeepEqualDiff(actualBackup, test.ExpectedBackup)
			if !eq {
				t.Fatalf("backup differ from expected:\n%s", diff)
			}
		})
	}
}
