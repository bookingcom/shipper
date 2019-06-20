package crds

import (
	apiextensionv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var RolloutBlock = &apiextensionv1beta1.CustomResourceDefinition{
	ObjectMeta: metav1.ObjectMeta{
		Name: "rolloutblocks.shipper.booking.com",
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
			Plural:     "rolloutblocks",
			Singular:   "rolloutblock",
			Kind:       "RolloutBlock",
			ShortNames: []string{"rb"},
			Categories: []string{"all", "shipper"},
		},
		Validation: &apiextensionv1beta1.CustomResourceValidation{
			OpenAPIV3Schema: &apiextensionv1beta1.JSONSchemaProps{
				Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
					"spec": apiextensionv1beta1.JSONSchemaProps{
						Type: "object",
						Required: []string{
							"message", "author",
						},
						Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
							"message": apiextensionv1beta1.JSONSchemaProps{
								Type: "string",
							},
							"author" : apiextensionv1beta1.JSONSchemaProps{
								Type: "object",
								Required: []string{
									"type",
									"name",
								},
								Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
									"type": apiextensionv1beta1.JSONSchemaProps{
										Type: "string",
									},
									"name": apiextensionv1beta1.JSONSchemaProps{
										Type: "string",
									},
								},
							},
						},
					},
				},
			},
		},

	},
}
