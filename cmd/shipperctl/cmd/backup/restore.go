package backup

import (
	"encoding/json"
	"fmt"
	"io/ioutil"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/yaml"

	"github.com/bookingcom/shipper/cmd/shipperctl/configurator"
	"github.com/bookingcom/shipper/cmd/shipperctl/ui"
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
)

var (
	backupFile string

	restoreBackupCmd = &cobra.Command{
		Use:   "restore",
		Short: "Restore backup that was prepared using `shipperctl prepare` command. Make sure that Shipper is down (`spec.replicas: 0`) before running this command.",
		Long: "Restore backup that was prepared using `shipperctl prepare` command." +
			"Make sure that Shipper is down (`spec.replicas: 0`) before running this command." +
			"this will update the owner reference of releases and target objects",
		RunE: runRestoreCommand,
	}
)

func init() {
	BackupCmd.AddCommand(restoreBackupCmd)
}

func runRestoreCommand(cmd *cobra.Command, args []string) error {
	kubeClient, err := configurator.NewKubeClientFromKubeConfig(kubeConfigFile, managementClusterContext)
	if err != nil {
		return err
	}

	shipperClient, err := configurator.NewShipperClientFromKubeConfig(kubeConfigFile, managementClusterContext)
	if err != nil {
		return err
	}

	shipperBackupApplications, err := unmarshalShipperBackupApplicationFromFile()
	if err != nil {
		return err
	}

	confirm, err := ui.AskForConfirmation(cmd.InOrStdin(), "Would you like to see an overview of your backup?")
	if err != nil {
		return err
	}
	if confirm {
		printShipperBackupApplication(shipperBackupApplications)
		confirm, err = ui.AskForConfirmation(cmd.InOrStdin(), "Would you like to review backup?")
		if err != nil {
			return err
		}
		if confirm {
			data, err := marshalShipperBackupApplication(shipperBackupApplications)
			if err != nil {
				return err
			}
			if _, err := cmd.OutOrStdout().Write(data); err != nil {
				return err
			}
		}
	}

	confirm, err = ui.AskForConfirmation(cmd.InOrStdin(), "Would you like to restore backup?")
	if err != nil {
		return err
	}
	if !confirm {
		return nil
	}

	if err := restore(shipperBackupApplications, kubeClient, shipperClient); err != nil {
		return err
	}

	return nil
}

func restore(shipperBackupApplications []shipperBackupApplication, kubeClient kubernetes.Interface, shipperClient shipperclientset.Interface) error {
	for _, obj := range shipperBackupApplications {
		// apply application
		if err := applyApplication(kubeClient, shipperClient, obj.Application); err != nil {
			return err
		}
		fmt.Printf("application %q created\n", fmt.Sprintf("%s/%s", obj.Application.Namespace, obj.Application.Name))

		// get the new UID
		uid, err := uidOfApplication(shipperClient, obj.Application.Name, obj.Application.Namespace)
		if err != nil {
			return err
		}

		// update owner ref for all releases and apply them
		for _, backupRelease := range obj.BackupReleases {
			rel := &backupRelease.Release
			err := updateOwnerRefUid(&rel.ObjectMeta, uid)
			if err != nil {
				return err
			}
			fmt.Printf("release %q owner reference updates with uid %q\n", fmt.Sprintf("%s/%s", rel.Namespace, rel.Name), uid)

			rel.ResourceVersion = ""
			if _, err = shipperClient.ShipperV1alpha1().Releases(rel.Namespace).Create(rel); err != nil {
				return err
			}
			fmt.Printf("release %q created\n", fmt.Sprintf("%s/%s", rel.Namespace, rel.Name))

			// get the new UID
			relUid, err := uidOfRelease(shipperClient, rel.Name, rel.Namespace)
			if err != nil {
				return err
			}

			if err := restoreTargetObjects(backupRelease, relUid, shipperClient); err != nil {
				return err
			}

		}

	}
	return nil
}

// update owner ref for all target objects and apply them
func restoreTargetObjects(backupRelease shipperBackupRelease, relUid types.UID, shipperClient shipperclientset.Interface) error {
	// InstallationTarget
	if err := updateOwnerRefUid(&backupRelease.InstallationTarget.ObjectMeta, relUid); err != nil {
		return err
	}
	fmt.Printf(
		"installation target %q owner reference updates with uid %q\n",
		fmt.Sprintf("%s/%s", backupRelease.InstallationTarget.Namespace, backupRelease.InstallationTarget.Name),
		relUid,
	)
	backupRelease.InstallationTarget.ResourceVersion = ""
	if _, err := shipperClient.ShipperV1alpha1().InstallationTargets(backupRelease.InstallationTarget.Namespace).Create(&backupRelease.InstallationTarget); err != nil {
		return err
	}
	fmt.Printf("installation target %q created\n", fmt.Sprintf("%s/%s", backupRelease.InstallationTarget.Namespace, backupRelease.InstallationTarget.Name))

	// TrafficTarget
	if err := updateOwnerRefUid(&backupRelease.TrafficTarget.ObjectMeta, relUid); err != nil {
		return err
	}
	fmt.Printf(
		"traffic target %q owner reference updates with uid %q\n",
		fmt.Sprintf("%s/%s", backupRelease.TrafficTarget.Namespace, backupRelease.TrafficTarget.Name),
		relUid,
	)
	backupRelease.TrafficTarget.ResourceVersion = ""
	if _, err := shipperClient.ShipperV1alpha1().TrafficTargets(backupRelease.TrafficTarget.Namespace).Create(&backupRelease.TrafficTarget); err != nil {
		return err
	}
	fmt.Printf("traffic target %q created\n", fmt.Sprintf("%s/%s", backupRelease.TrafficTarget.Namespace, backupRelease.TrafficTarget.Name))

	// CapacityTarget
	if err := updateOwnerRefUid(&backupRelease.CapacityTarget.ObjectMeta, relUid); err != nil {
		return err
	}
	fmt.Printf(
		"capacity target %q owner reference updates with uid %q\n",
		fmt.Sprintf("%s/%s", backupRelease.CapacityTarget.Namespace, backupRelease.CapacityTarget.Name),
		relUid,
	)
	backupRelease.CapacityTarget.ResourceVersion = ""
	if _, err := shipperClient.ShipperV1alpha1().CapacityTargets(backupRelease.CapacityTarget.Namespace).Create(&backupRelease.CapacityTarget); err != nil {
		return err
	}
	fmt.Printf("capacity target %q created\n", fmt.Sprintf("%s/%s", backupRelease.CapacityTarget.Namespace, backupRelease.CapacityTarget.Name))

	return nil
}

func applyApplication(kubeClient kubernetes.Interface, shipperClient shipperclientset.Interface, app shipper.Application) error {
	// make sure namespace exists
	ns, err := kubeClient.CoreV1().Namespaces().Get(app.Namespace, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			// create ns:
			newNs := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: app.Namespace,
				},
			}
			if ns, err = kubeClient.CoreV1().Namespaces().Create(&newNs); err != nil {
				return err
			}
		} else {
			return err
		}
	}
	// apply application
	app.ResourceVersion = ""
	if _, err = shipperClient.ShipperV1alpha1().Applications(ns.Name).Create(&app); err != nil {
		return err
	}

	return nil
}

func uidOfApplication(shipperClient shipperclientset.Interface, appName, appNamespace string) (types.UID, error) {
	application, err := shipperClient.ShipperV1alpha1().Applications(appNamespace).Get(appName, metav1.GetOptions{})
	if err != nil {
		return types.UID(""), err
	}
	uid := application.GetUID()
	return uid, nil
}

func uidOfRelease(shipperClient shipperclientset.Interface, relName, relNamespace string) (types.UID, error) {
	release, err := shipperClient.ShipperV1alpha1().Releases(relNamespace).Get(relName, metav1.GetOptions{})
	if err != nil {
		return types.UID(""), err
	}
	uid := release.GetUID()
	return uid, nil
}

func updateOwnerRefUid(obj *metav1.ObjectMeta, uid types.UID) error {
	ownerReferences := obj.GetOwnerReferences()
	if n := len(ownerReferences); n != 1 {
		key, _ := cache.MetaNamespaceKeyFunc(obj)
		return fmt.Errorf("expected exactly one owner for object %q but got %d", key, n)
	}
	ownerReferences[0].UID = uid
	obj.SetOwnerReferences(ownerReferences)
	return nil
}

func unmarshalShipperBackupApplicationFromFile() ([]shipperBackupApplication, error) {
	backupBytes, err := ioutil.ReadFile(backupFile)
	if err != nil {
		return nil, err
	}

	shipperBackupApplications := &[]shipperBackupApplication{}

	switch outputFormat {
	case "yaml":
		err = yaml.Unmarshal(backupBytes, shipperBackupApplications)
		if err != nil {
			return nil, err
		}
	case "json":
		err = json.Unmarshal(backupBytes, shipperBackupApplications)
		if err != nil {
			return nil, err
		}
	}

	return *shipperBackupApplications, nil
}
