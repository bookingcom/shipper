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
		Short: "Restore backup that was prepared using `shipperctl prepare` command",
		Long: "Restore backup that was prepared using `shipperctl prepare` command" +
			"this will update the owner reference of releases objects",
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

	releasesPerApplications, err := unmarshalReleasesPerApplicationFromFile()
	if err != nil {
		return err
	}

	confirm, err := ui.AskForConfirmation(cmd.InOrStdin(), "Would you like to see an overview of your backup?")
	if err != nil {
		return err
	}
	if confirm {
		printReleasesPerApplications(releasesPerApplications)
		confirm, err = ui.AskForConfirmation(cmd.InOrStdin(), "Would you like to review backup?")
		if err != nil {
			return err
		}
		if confirm {
			data, err := marshalReleasesPerApplications(releasesPerApplications)
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

	if err := restore(releasesPerApplications, kubeClient, shipperClient); err != nil {
		return err
	}

	return nil
}

func restore(releasesPerApplications []shipperBackupObject, kubeClient kubernetes.Interface, shipperClient shipperclientset.Interface) error {
	for _, obj := range releasesPerApplications {
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
		for _, release := range obj.Releases {
			rel, err := updateOwnerRefUid(release, *uid)
			if err != nil {
				return err
			}
			fmt.Printf("release %q owner reference updates with uid %q\n", fmt.Sprintf("%s/%s", rel.Namespace, rel.Name), *uid)

			rel.ResourceVersion = ""
			if _, err = shipperClient.ShipperV1alpha1().Releases(rel.Namespace).Create(rel); err != nil {
				return err
			}
			fmt.Printf("release %q created\n", fmt.Sprintf("%s/%s", rel.Namespace, rel.Name))
		}

	}
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

func uidOfApplication(shipperClient shipperclientset.Interface, appName, appNamespace string) (*types.UID, error) {
	application, err := shipperClient.ShipperV1alpha1().Applications(appNamespace).Get(appName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	uid := application.GetUID()
	return &uid, nil
}

func updateOwnerRefUid(rel shipper.Release, uid types.UID) (*shipper.Release, error) {
	ownerReferences := rel.GetOwnerReferences()
	if n := len(rel.OwnerReferences); n != 1 {
		key, _ := cache.MetaNamespaceKeyFunc(rel)
		return nil, fmt.Errorf("expected exactly one owner for Release %q but got %d", key, n)
	}
	ownerReferences[0].UID = uid
	rel.SetOwnerReferences(ownerReferences)
	return &rel, nil
}

func unmarshalReleasesPerApplicationFromFile() ([]shipperBackupObject, error) {
	backupBytes, err := ioutil.ReadFile(backupFile)
	if err != nil {
		return nil, err
	}

	releasesPerApplications := &[]shipperBackupObject{}

	switch outputFormat {
	case "yaml":
		err = yaml.Unmarshal(backupBytes, releasesPerApplications)
		if err != nil {
			return nil, err
		}
	case "json":
		err = json.Unmarshal(backupBytes, releasesPerApplications)
		if err != nil {
			return nil, err
		}
	}

	return *releasesPerApplications, nil
}
