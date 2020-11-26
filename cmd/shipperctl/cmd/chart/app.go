package chart

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bookingcom/shipper/cmd/shipperctl/config"
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

var (
	appName  string
	fileName string

	renderAppCmd = &cobra.Command{
		Use:   "app",
		Short: "render Shipper Charts for an Application",
		RunE:  renderChartFromApp,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if appName == "" && fileName == "" {
				cmd.Printf("error: must have *one* of the flags: appName, filename\n")
				os.Exit(1)
			}
			if appName != "" && fileName != "" {
				cmd.Printf("error: must have *one* of the flags: appName, filename\n")
				os.Exit(1)
			}
		},
	}
)

func init() {
	fileFlagName := "filename"

	renderAppCmd.Flags().StringVar(&appName, "appName", "", "The name of an existing application to render chart for")
	renderAppCmd.Flags().StringVar(&fileName, fileFlagName, "", "An application manifest in the current context to render chart for (e.g. `applicastion.yaml`)")
	err := renderAppCmd.MarkFlagFilename(fileFlagName, "yaml")
	if err != nil {
		renderAppCmd.Printf("warning: could not mark %q for filename yaml autocompletion: %s\n", fileFlagName, err)
	}

	renderCmd.AddCommand(renderAppCmd)
}

func renderChartFromApp(cmd *cobra.Command, args []string) error {
	c, err := newChartRenderConfig()
	if err != nil {
		return err
	}
	_, shipperClient, err := config.Load(kubeConfigFile, managementClusterContext)
	if err != nil {
		return err
	}
	var application shipper.Application
	if appName != "" {
		applicationP, err := shipperClient.ShipperV1alpha1().Applications(namespace).Get(appName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		application = *applicationP
	} else {
		appYaml, err := ioutil.ReadFile(fileName)
		if err != nil {
			return err
		}

		if err := yaml.Unmarshal(appYaml, &application); err != nil {
			return err
		}

	}

	c.ReleaseName = fmt.Sprintf("%s-%s-%d", application.Name, "foobar", 0)
	c.ChartSpec = application.Spec.Template.Chart
	c.ChartValues = application.Spec.Template.Values
	rendered, err := render(c)
	if err != nil {
		return err
	}

	cmd.Println(rendered)
	return nil
}
