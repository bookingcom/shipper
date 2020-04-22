package crds

import (
	apiextensionv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var CapacityTarget = &apiextensionv1beta1.CustomResourceDefinition{
	ObjectMeta: metav1.ObjectMeta{
		Name: "capacitytargets.shipper.booking.com",
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
			Plural:     "capacitytargets",
			Singular:   "capacitytarget",
			Kind:       "CapacityTarget",
			ShortNames: []string{"ct"},
			Categories: []string{"shipper"},
		},
		Subresources: &apiextensionv1beta1.CustomResourceSubresources{
			Status: &apiextensionv1beta1.CustomResourceSubresourceStatus{},
		},
		Validation: &apiextensionv1beta1.CustomResourceValidation{
			OpenAPIV3Schema: &apiextensionv1beta1.JSONSchemaProps{
				Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
					"spec": apiextensionv1beta1.JSONSchemaProps{
						Type: "object",
						Required: []string{
							"percent",
							"totalReplicaCount",
						},
						Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
							"percent": apiextensionv1beta1.JSONSchemaProps{
								Type:    "integer",
								Minimum: &zero,
								Maximum: &hundred,
							},
							"totalReplicaCount": apiextensionv1beta1.JSONSchemaProps{
								Type:    "integer",
								Minimum: &zero,
							},
						},
					},
				},
			},
		},
		AdditionalPrinterColumns: []apiextensionv1beta1.CustomResourceColumnDefinition{
			apiextensionv1beta1.CustomResourceColumnDefinition{
				Name:        "Operational",
				Type:        "string",
				Description: "Whether the capacity target is operational.",
				JSONPath:    `.status.conditions[?(.type=="Operational")].status`,
			},
			apiextensionv1beta1.CustomResourceColumnDefinition{
				Name:        "Ready",
				Type:        "string",
				Description: "Whether the capacity target is ready.",
				JSONPath:    `.status.conditions[?(.type=="Ready")].status`,
			},
			apiextensionv1beta1.CustomResourceColumnDefinition{
				Name:        "Reason",
				Type:        "string",
				Description: "Reason for the capacity target to not be ready or operational.",
				JSONPath:    `.status.conditions[?(.status=="False")].message`,
			},
			apiextensionv1beta1.CustomResourceColumnDefinition{
				Name:        "Age",
				Type:        "date",
				Description: "The capacity target's age.",
				JSONPath:    ".metadata.creationTimestamp",
			},
		},
	},
}
