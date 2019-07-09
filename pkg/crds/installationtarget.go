package crds

import (
	apiextensionv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var InstallationTarget = &apiextensionv1beta1.CustomResourceDefinition{
	ObjectMeta: metav1.ObjectMeta{
		Name: "installationtargets.shipper.booking.com",
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
			Plural:     "installationtargets",
			Singular:   "installationtarget",
			Kind:       "InstallationTarget",
			ShortNames: []string{"it"},
			Categories: []string{"shipper"},
		},
		Validation: &apiextensionv1beta1.CustomResourceValidation{
			OpenAPIV3Schema: &apiextensionv1beta1.JSONSchemaProps{
				Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
					"spec": apiextensionv1beta1.JSONSchemaProps{
						Type: "object",
						Required: []string{
							"clusters",
							"canOverride",
						},
						Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
							"clusters": apiextensionv1beta1.JSONSchemaProps{
								Type: "array",
								Items: &apiextensionv1beta1.JSONSchemaPropsOrArray{
									Schema: &apiextensionv1beta1.JSONSchemaProps{
										Type: "string",
									},
								},
							},
							"canOverride": apiextensionv1beta1.JSONSchemaProps{
								Type: "boolean",
							},
							"chart": apiextensionv1beta1.JSONSchemaProps{
								Type: "object",
								Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
									"name":    apiextensionv1beta1.JSONSchemaProps{Type: "string"},
									"version": apiextensionv1beta1.JSONSchemaProps{Type: "string"},
									"repoUrl": apiextensionv1beta1.JSONSchemaProps{Type: "string"},
								},
							},
							"values": apiextensionv1beta1.JSONSchemaProps{
								Type: "object",
							},
						},
					},
				},
			},
		},
	},
}
