package crds

import (
	apiextensionv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var Cluster = &apiextensionv1beta1.CustomResourceDefinition{
	ObjectMeta: metav1.ObjectMeta{
		Name: "clusters.shipper.booking.com",
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
			Plural:     "clusters",
			Singular:   "cluster",
			Kind:       "Cluster",
			ShortNames: []string{"cl"},
			Categories: []string{"shipper"},
		},
		Scope: apiextensionv1beta1.ClusterScoped,
		Validation: &apiextensionv1beta1.CustomResourceValidation{
			OpenAPIV3Schema: &apiextensionv1beta1.JSONSchemaProps{
				Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
					"spec": apiextensionv1beta1.JSONSchemaProps{
						Type: "object",
						Required: []string{
							"region",
							"apiMaster",
						},
						Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
							"region": apiextensionv1beta1.JSONSchemaProps{
								Type: "string",
							},
							"apiMaster": apiextensionv1beta1.JSONSchemaProps{
								Type: "string",
							},
							"capabilities": apiextensionv1beta1.JSONSchemaProps{
								Type: "array",
								Items: &apiextensionv1beta1.JSONSchemaPropsOrArray{
									Schema: &apiextensionv1beta1.JSONSchemaProps{
										Type: "string",
									},
								},
							},
							"scheduler": apiextensionv1beta1.JSONSchemaProps{
								Type: "object",
								Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
									"unschedulable": apiextensionv1beta1.JSONSchemaProps{
										Type: "boolean",
									},
									"weight": apiextensionv1beta1.JSONSchemaProps{
										Type: "integer",
									},
									"identity": apiextensionv1beta1.JSONSchemaProps{
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
