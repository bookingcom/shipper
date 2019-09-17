package crds

import (
	apiextensionv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var Application = &apiextensionv1beta1.CustomResourceDefinition{
	ObjectMeta: metav1.ObjectMeta{
		Name: "applications.shipper.booking.com",
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
			Plural:     "applications",
			Singular:   "application",
			Kind:       "Application",
			ShortNames: []string{"app"},
			Categories: []string{"all", "shipper"},
		},
		Validation: &apiextensionv1beta1.CustomResourceValidation{
			OpenAPIV3Schema: &apiextensionv1beta1.JSONSchemaProps{
				Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
					"spec": apiextensionv1beta1.JSONSchemaProps{
						Type: "object",
						Required: []string{
							"template",
						},
						Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
							"template": environmentValidation,
						},
					},
				},
			},
		},
		AdditionalPrinterColumns: []apiextensionv1beta1.CustomResourceColumnDefinition{
			apiextensionv1beta1.CustomResourceColumnDefinition{
				Name:        "Latest Release",
				Type:        "string",
				Description: "The application's latest release.",
				JSONPath:    ".status.history[(@.length-1)]",
			},
			apiextensionv1beta1.CustomResourceColumnDefinition{
				Name:        "Rolling Out",
				Type:        "string",
				Description: "Whether the application is going through a rollout.",
				JSONPath:    ".status.conditions[?(@.type=='RollingOut')].status",
			},
			apiextensionv1beta1.CustomResourceColumnDefinition{
				Name:        "Age",
				Type:        "date",
				Description: "The application's age.",
				JSONPath:    ".metadata.creationTimestamp",
			},
		},
	},
}
