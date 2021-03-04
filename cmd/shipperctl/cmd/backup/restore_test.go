package backup

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

const (
	testNamespaceName           = "unit-test-namespace"
	testAppName                 = "unit-test-app"
	testRelName                 = "unit-test-release"
	appUid            types.UID = "app-UID"
	relUid            types.UID = "rel-UID"
)

func TestRestore(t *testing.T) {
	application := shipper.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testAppName,
			Namespace: testNamespaceName,
			UID:       appUid,
		},
	}
	release := shipper.Release{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testRelName,
			Namespace: testNamespaceName,
			UID:       relUid,
			OwnerReferences: []metav1.OwnerReference{
				{
					UID: "just-a-UID",
				},
			},
		},
	}
	installationTarget := shipper.InstallationTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testRelName,
			Namespace: testNamespaceName,
			OwnerReferences: []metav1.OwnerReference{
				{
					UID: "just-another-UID",
				},
			},
		},
	}
	trafficTarget := shipper.TrafficTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testRelName,
			Namespace: testNamespaceName,
			OwnerReferences: []metav1.OwnerReference{
				{
					UID: "just-another-UID",
				},
			},
		},
	}
	capacityTarget := shipper.CapacityTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testRelName,
			Namespace: testNamespaceName,
			OwnerReferences: []metav1.OwnerReference{
				{
					UID: "just-another-UID",
				},
			},
		},
	}

	tests := []struct {
		Name          string
		Backup        []shipperBackupApplication
		ExpectedError error
	}{
		{
			"Regular working restore",
			[]shipperBackupApplication{
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
			nil,
		},
		{
			"Missing object fail restore",
			[]shipperBackupApplication{
				{
					Application: application,
					BackupReleases: []shipperBackupRelease{
						{
							Release:            release,
							InstallationTarget: installationTarget,
							CapacityTarget:     capacityTarget,
						},
					},
				},
			},
			fmt.Errorf(
				"failed to update traffic target owner reference: expected exactly one owner for object \"%s\" but got %d",
				"",
				0,
			),
		},
		{
			"Multiple owner ref release fail restore",
			[]shipperBackupApplication{
				{
					Application: application,
					BackupReleases: []shipperBackupRelease{
						{
							Release: shipper.Release{
								ObjectMeta: metav1.ObjectMeta{
									Name:      testRelName,
									Namespace: testNamespaceName,
									UID:       relUid,
									OwnerReferences: []metav1.OwnerReference{
										{
											UID: "just-a-UID",
										},
										{
											UID: "another-UID",
										},
									},
								},
							},
							InstallationTarget: installationTarget,
							TrafficTarget:      trafficTarget,
							CapacityTarget:     capacityTarget,
						},
					},
				},
			},
			fmt.Errorf(
				"failed to update release owner reference: expected exactly one owner for object \"%s/%s\" but got %d",
				testNamespaceName,
				testRelName,
				2,
			),
		},
		{
			"Multiple owner ref capacity target fail restore",
			[]shipperBackupApplication{
				{
					Application: application,
					BackupReleases: []shipperBackupRelease{
						{
							Release:            release,
							InstallationTarget: installationTarget,
							TrafficTarget:      trafficTarget,
							CapacityTarget: shipper.CapacityTarget{
								ObjectMeta: metav1.ObjectMeta{
									Name:      testRelName,
									Namespace: testNamespaceName,
									OwnerReferences: []metav1.OwnerReference{
										{
											UID: "just-another-UID",
										},
										{
											UID: "just-another-one-UID",
										},
									},
								},
							},
						},
					},
				},
			},
			fmt.Errorf(
				"failed to update capacity target owner reference: expected exactly one owner for object \"%s/%s\" but got %d",
				testNamespaceName,
				testRelName,
				2,
			),
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			f := newFixture(t, test.Backup, test.ExpectedError)
			f.runRestore()
		})
	}
}

type fixture struct {
	t             *testing.T
	objects       []runtime.Object
	backupObjects []shipperBackupApplication
	shipperClient *shipperfake.Clientset
	kubeClient    *kubefake.Clientset

	actions       []kubetesting.Action
	expectedError error
}

func newFixture(t *testing.T, backupObjects []shipperBackupApplication, err error) *fixture {
	f := fixture{
		t:             t,
		backupObjects: backupObjects,
		actions:       make([]kubetesting.Action, 0),
		expectedError: err,
	}
	f.shipperClient = shipperfake.NewSimpleClientset()
	f.kubeClient = kubefake.NewSimpleClientset()
	f.addBackupObjectsAndActions(backupObjects)
	return &f
}

func (f *fixture) runRestore() {
	err := restore(f.backupObjects, f.kubeClient, f.shipperClient, restoreBackupCmd)
	if !reflect.DeepEqual(err, f.expectedError) {
		f.t.Fatalf("expected error \n%v\ngot error \n%v", f.expectedError, err)
	}
	actual := shippertesting.FilterActions(f.shipperClient.Actions())
	shippertesting.CheckActions(f.actions, actual, f.t)

}

func (f *fixture) addCreateApplicationAction(app *shipper.Application) {
	gvr := shipper.SchemeGroupVersion.WithResource("applications")
	action := kubetesting.NewCreateAction(gvr, app.GetNamespace(), app)

	f.actions = append(f.actions, action)
}

func (f *fixture) addCreateReleaseAction(rel *shipper.Release) {
	gvr := shipper.SchemeGroupVersion.WithResource("releases")
	action := kubetesting.NewCreateAction(gvr, rel.GetNamespace(), rel)

	f.actions = append(f.actions, action)
}

func (f *fixture) addCreateITAction(it *shipper.InstallationTarget) {
	gvr := shipper.SchemeGroupVersion.WithResource("installationtargets")
	action := kubetesting.NewCreateAction(gvr, it.GetNamespace(), it)

	f.actions = append(f.actions, action)
}

func (f *fixture) addCreateTTAction(tt *shipper.TrafficTarget) {
	gvr := shipper.SchemeGroupVersion.WithResource("traffictargets")
	action := kubetesting.NewCreateAction(gvr, tt.GetNamespace(), tt)

	f.actions = append(f.actions, action)
}

func (f *fixture) addCreateCTAction(ct *shipper.CapacityTarget) {
	gvr := shipper.SchemeGroupVersion.WithResource("capacitytargets")
	action := kubetesting.NewCreateAction(gvr, ct.GetNamespace(), ct)

	f.actions = append(f.actions, action)
}

func (f *fixture) addBackupObjectsAndActions(bkups []shipperBackupApplication) {
	for _, bkup := range bkups {
		f.objects = append(f.objects, &bkup.Application)
		f.addCreateApplicationAction(&bkup.Application)
		for _, backupRelease := range bkup.BackupReleases {
			expectedRel := backupRelease.Release.DeepCopy()
			if len(expectedRel.OwnerReferences) != 1 {
				return
			}
			expectedRel.OwnerReferences[0].UID = appUid
			f.objects = append(f.objects, expectedRel)
			f.addCreateReleaseAction(expectedRel)

			expectedIT := backupRelease.InstallationTarget.DeepCopy()
			if len(expectedIT.OwnerReferences) != 1 {
				return
			}
			expectedIT.OwnerReferences[0].UID = relUid
			f.objects = append(f.objects, expectedIT)
			f.addCreateITAction(expectedIT)

			expectedTT := backupRelease.TrafficTarget.DeepCopy()
			if len(expectedTT.OwnerReferences) != 1 {
				return
			}
			expectedTT.OwnerReferences[0].UID = relUid
			f.objects = append(f.objects, expectedTT)
			f.addCreateTTAction(expectedTT)

			expectedCT := backupRelease.CapacityTarget.DeepCopy()
			if len(expectedCT.OwnerReferences) != 1 {
				return
			}
			expectedCT.OwnerReferences[0].UID = relUid
			f.objects = append(f.objects, expectedCT)
			f.addCreateCTAction(expectedCT)

		}
	}
}

func TestScaleDownShipper(t *testing.T) {
	var zero int32 = 0
	var one int32 = 1

	tests := []struct {
		Name              string
		ShipperDeployment *appsv1.Deployment
		ExpectedError     error
	}{
		{
			"Shipper has 0 replicas",
			&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shipper",
					Namespace: "shipper-system",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &zero,
				},
			},
			nil,
		},
		{
			"Shipper has 1 replicas",
			&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shipper",
					Namespace: "shipper-system",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &one,
				},
			},
			fmt.Errorf(
				"shipper deployment has %d replicas. "+
					"Scale it down first with `kubectl -n shipper-system patch deploy shipper --type=merge -p '{\"spec\":{\"replicas\":0}}'`",
				one,
			),
		},
		{
			"Shipper is nil",
			nil,
			nil,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			fakeKube, err := initialiseFakeEnv(test.ShipperDeployment)
			if err != nil {
				t.Fatalf("unexpected error \n%v", err)
			}
			err = scaleDownShipper(restoreBackupCmd, fakeKube)
			if !reflect.DeepEqual(err, test.ExpectedError) {
				t.Fatalf("expected error \n%v\ngot error \n%v", test.ExpectedError, err)
			}
		})
	}
}

func initialiseFakeEnv(shipperDeployment *appsv1.Deployment) (*kubefake.Clientset, error) {
	fakeKube := kubefake.NewSimpleClientset()
	if shipperDeployment == nil {
		return fakeKube, nil
	}
	if _, err := fakeKube.AppsV1().Deployments("shipper-system").Create(context.TODO(), shipperDeployment, metav1.CreateOptions{}); err != nil {
		return nil, err
	}
	return fakeKube, nil
}

func TestMakeSureNotShipperObjects(t *testing.T) {
	tests := []struct {
		Name           string
		ShipperObjects []shipper.Application
		ExpectedError  error
	}{
		{
			Name:           "No shipper objects",
			ShipperObjects: []shipper.Application{},
			ExpectedError:  nil,
		},
		{
			Name:           "Cluster's Not Empty",
			ShipperObjects: []shipper.Application{
				shipper.Application{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testAppName,
						Namespace: testNamespaceName,
						UID:       appUid,
					},
				},
			},
			ExpectedError:  fmt.Errorf(
				"found Shipper objects:\n - %s\ndelete them first with `kubectl delete app,rel,it,tt,ct --all-namespaces --all`",
				"Applications: 1",
			),
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			shipperFakeClient, err := initialiseFakeEnvWithShipperApps(test.ShipperObjects)
			if err != nil {
				t.Fatalf("unexpected error \n%v", err)
			}
			err = makeSureNotShipperObjects(restoreBackupCmd, shipperFakeClient)
			if !reflect.DeepEqual(err, test.ExpectedError) {
				t.Fatalf("expected error \n%v\ngot error \n%v", test.ExpectedError, err)
			}
		})
	}
}

func initialiseFakeEnvWithShipperApps(shipperApps []shipper.Application) (*shipperfake.Clientset, error) {
	shipperFakeClient := shipperfake.NewSimpleClientset()
	for _, app := range shipperApps {
		if _, err := shipperFakeClient.ShipperV1alpha1().Applications(app.GetNamespace()).Create(context.TODO(), &app, metav1.CreateOptions{}); err != nil {
			return nil, err
		}
	}
	return shipperFakeClient, nil
}
