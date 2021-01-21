package backup

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/yaml"

	"github.com/bookingcom/shipper/cmd/shipperctl/config"
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
	Cmd.AddCommand(restoreBackupCmd)
}

func runRestoreCommand(cmd *cobra.Command, args []string) error {
	kubeClient, shipperClient, err := config.Load(kubeConfigFile, managementClusterContext)
	if err != nil {
		return err
	}

	shipperBackupApplications, err := unmarshalShipperBackupApplicationFromFile()
	if err != nil {
		return err
	}

	if err := scaleDownShipper(cmd, kubeClient); err != nil {
		return err
	}

	if err := makeSureNotShipperObjects(cmd, shipperClient); err != nil {
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

	if err := restore(shipperBackupApplications, kubeClient, shipperClient, cmd); err != nil {
		return err
	}

	return nil
}

func restore(shipperBackupApplications []shipperBackupApplication, kubeClient kubernetes.Interface, shipperClient shipperclientset.Interface, cmd *cobra.Command) error {
	for _, obj := range shipperBackupApplications {
		// apply application and get the new UID
		uid, err := applyApplication(kubeClient, shipperClient, obj.Application)
		if err != nil {
			return fmt.Errorf("failed to apply application: %v", err)
		}
		cmd.Printf("application %q created\n", fmt.Sprintf("%s/%s", obj.Application.Namespace, obj.Application.Name))

		// update owner ref for all releases and apply them
		for _, backupRelease := range obj.BackupReleases {
			rel := &backupRelease.Release
			err := updateOwnerRefUid(&rel.ObjectMeta, uid)
			if err != nil {
				return fmt.Errorf("failed to update release owner reference: %v", err)
			}
			cmd.Printf("release %q owner reference updates with uid %q\n", fmt.Sprintf("%s/%s", rel.Namespace, rel.Name), uid)

			rel.ResourceVersion = ""
			newRel, err := shipperClient.ShipperV1alpha1().Releases(rel.Namespace).Create(rel)
			if err != nil {
				return err
			}
			cmd.Printf("release %q created\n", fmt.Sprintf("%s/%s", newRel.Namespace, newRel.Name))

			// get the new UID
			relUid := newRel.GetUID()

			if err := restoreTargetObjects(backupRelease, relUid, shipperClient, cmd); err != nil {
				return err
			}

		}

	}
	return nil
}

// update owner ref for all target objects and apply them
func restoreTargetObjects(backupRelease shipperBackupRelease, relUid types.UID, shipperClient shipperclientset.Interface, cmd *cobra.Command) error {
	// InstallationTarget
	if err := updateOwnerRefUid(&backupRelease.InstallationTarget.ObjectMeta, relUid); err != nil {
		return fmt.Errorf("failed to update installation target owner reference: %v", err)
	}
	cmd.Printf(
		"installation target %q owner reference updates with uid %q\n",
		fmt.Sprintf("%s/%s", backupRelease.InstallationTarget.Namespace, backupRelease.InstallationTarget.Name),
		relUid,
	)
	backupRelease.InstallationTarget.ResourceVersion = ""
	if _, err := shipperClient.ShipperV1alpha1().InstallationTargets(backupRelease.InstallationTarget.Namespace).Create(&backupRelease.InstallationTarget); err != nil {
		return err
	}
	cmd.Printf("installation target %q created\n", fmt.Sprintf("%s/%s", backupRelease.InstallationTarget.Namespace, backupRelease.InstallationTarget.Name))

	// TrafficTarget
	if err := updateOwnerRefUid(&backupRelease.TrafficTarget.ObjectMeta, relUid); err != nil {
		return fmt.Errorf("failed to update traffic target owner reference: %v", err)
	}
	cmd.Printf(
		"traffic target %q owner reference updates with uid %q\n",
		fmt.Sprintf("%s/%s", backupRelease.TrafficTarget.Namespace, backupRelease.TrafficTarget.Name),
		relUid,
	)
	backupRelease.TrafficTarget.ResourceVersion = ""
	if _, err := shipperClient.ShipperV1alpha1().TrafficTargets(backupRelease.TrafficTarget.Namespace).Create(&backupRelease.TrafficTarget); err != nil {
		return err
	}
	cmd.Printf("traffic target %q created\n", fmt.Sprintf("%s/%s", backupRelease.TrafficTarget.Namespace, backupRelease.TrafficTarget.Name))

	// CapacityTarget
	if err := updateOwnerRefUid(&backupRelease.CapacityTarget.ObjectMeta, relUid); err != nil {
		return fmt.Errorf("failed to update capacity target owner reference: %v", err)
	}
	cmd.Printf(
		"capacity target %q owner reference updates with uid %q\n",
		fmt.Sprintf("%s/%s", backupRelease.CapacityTarget.Namespace, backupRelease.CapacityTarget.Name),
		relUid,
	)
	backupRelease.CapacityTarget.ResourceVersion = ""
	if _, err := shipperClient.ShipperV1alpha1().CapacityTargets(backupRelease.CapacityTarget.Namespace).Create(&backupRelease.CapacityTarget); err != nil {
		return err
	}
	cmd.Printf("capacity target %q created\n", fmt.Sprintf("%s/%s", backupRelease.CapacityTarget.Namespace, backupRelease.CapacityTarget.Name))

	return nil
}

func applyApplication(kubeClient kubernetes.Interface, shipperClient shipperclientset.Interface, app shipper.Application) (types.UID, error) {
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
				return types.UID(""), err
			}
		} else {
			return types.UID(""), err
		}
	}
	// apply application
	app.ResourceVersion = ""
	createdApp, err := shipperClient.ShipperV1alpha1().Applications(ns.Name).Create(&app)
	if err != nil {
		return types.UID(""), err
	}

	return createdApp.GetUID(), nil
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

func scaleDownShipper(cmd *cobra.Command, kubeClient kubernetes.Interface) error {
	cmd.Print("Making sure shipper is down... ")
	shipperDeployment, err := kubeClient.AppsV1().Deployments(shipper.ShipperNamespace).Get("shipper", metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			cmd.Println("done")
			return nil
		}
		return fmt.Errorf("failed to find Shipper deployment: %s", err.Error())
	}

	if *shipperDeployment.Spec.Replicas != 0 {
		cmd.Println()
		return fmt.Errorf(
			"shipper deployment has %d replicas. " +
				"Scale it down first with `kubectl -n shipper-system patch deploy shipper --type=merge -p '{\"spec\":{\"replicas\":0}}'`",
			*shipperDeployment.Spec.Replicas,
		)
	}
	cmd.Println("done")
	return nil
}

func makeSureNotShipperObjects(cmd *cobra.Command, shipperClient shipperclientset.Interface) error {
	cmd.Print("Making sure there are no Shipper objects lying around... ")
	unexpectedMessage := []string{}
	// Applications
	applicationList, err := shipperClient.ShipperV1alpha1().Applications("").List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	if n := len(applicationList.Items); n != 0 {
		unexpectedMessage = append(unexpectedMessage, fmt.Sprintf("Applications: %d", n))
	}
	// Releases
	releaseList, err := shipperClient.ShipperV1alpha1().Releases("").List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	if n := len(releaseList.Items); n != 0 {
		unexpectedMessage = append(unexpectedMessage, fmt.Sprintf("Releases: %d", n))
	}
	// Installation targets
	installationTargetsList, err := shipperClient.ShipperV1alpha1().InstallationTargets("").List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	if n := len(installationTargetsList.Items); n != 0 {
		unexpectedMessage = append(unexpectedMessage, fmt.Sprintf("InstallationTargets: %d", n))
	}
	// Traffic targets
	trafficTargetsList, err := shipperClient.ShipperV1alpha1().TrafficTargets("").List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	if n := len(trafficTargetsList.Items); n != 0 {
		unexpectedMessage = append(unexpectedMessage, fmt.Sprintf("TrafficTargets: %d", n))
	}
	// Capacity targets
	capacityTargetsList, err := shipperClient.ShipperV1alpha1().CapacityTargets("").List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	if n := len(capacityTargetsList.Items); n != 0 {
		unexpectedMessage = append(unexpectedMessage, fmt.Sprintf("CapacityTargets: %d", n))
	}

	if len(unexpectedMessage) > 0 {
		cmd.Println()
		return fmt.Errorf(
			"found Shipper objects:\n - %s\ndelete them first with `kubectl delete app,rel,it,tt,ct --all-namespaces --all`",
			strings.Join(unexpectedMessage, "\n - "),
		)
	}
	cmd.Println("done")
	return nil
}
