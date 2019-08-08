package crds

import (
	apiextensionv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var Release = &apiextensionv1beta1.CustomResourceDefinition{
	ObjectMeta: metav1.ObjectMeta{
		Name: "releases.shipper.booking.com",
	},
	Spec: apiextensionv1beta1.CustomResourceDefinitionSpec{
		Group: "shipper.booking.com",
		Versions: []apiextensionv1beta1.CustomResourceDefinitionVersion{
			apiextensionv1beta1.CustomResourceDefinitionVersion{
				Name:    "v1alpha1",
				Served:  true,
				Storage: true,
			},
		},
		Names: apiextensionv1beta1.CustomResourceDefinitionNames{
			Plural:     "releases",
			Singular:   "release",
			Kind:       "Release",
			ShortNames: []string{"rel"},
			Categories: []string{"all", "shipper"},
		},
		Validation: &apiextensionv1beta1.CustomResourceValidation{
			OpenAPIV3Schema: &apiextensionv1beta1.JSONSchemaProps{
				Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
					"spec": apiextensionv1beta1.JSONSchemaProps{
						Type: "object",
						Required: []string{
							"targetStep",
							"environment",
						},
						Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
							"targetStep": apiextensionv1beta1.JSONSchemaProps{
								Type:    "integer",
								Minimum: &zero,
							},
							"environment": environmentValidation,
						},
					},
				},
			},
		},
		AdditionalPrinterColumns: []apiextensionv1beta1.CustomResourceColumnDefinition{
			apiextensionv1beta1.CustomResourceColumnDefinition{
				Name:        "Step",
				Type:        "string",
				Description: "The current achieved step for a release as defined in the rollout strategy.",
				JSONPath:    ".status.achievedStep.name",
			},
			apiextensionv1beta1.CustomResourceColumnDefinition{
				Name:        "Errors",
				Type:        "string",
				Description: "Reasons from False conditions that show what's wrong with the release.",
				JSONPath:    ".status.conditions[?(@.status=='False')].reason",
			},
			apiextensionv1beta1.CustomResourceColumnDefinition{
				Name:        "Age",
				Type:        "date",
				Description: "The release's age.",
				JSONPath:    ".metadata.creationTimestamp",
			},
			apiextensionv1beta1.CustomResourceColumnDefinition{
				Name:        "Clusters",
				Type:        "string",
				Description: "The list of clusters where a release is supposed to be rolled out as per strategy.",
				JSONPath:    ".metadata.annotations.shipper\\.booking\\.com\\/release\\.clusters",
				Priority:    -1,
			},
		},
	},
}
