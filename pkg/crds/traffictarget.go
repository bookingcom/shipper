package crds

import (
	apiextensionv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var TrafficTarget = &apiextensionv1beta1.CustomResourceDefinition{
	ObjectMeta: metav1.ObjectMeta{
		Name: "traffictargets.shipper.booking.com",
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
			Plural:     "traffictargets",
			Singular:   "traffictarget",
			Kind:       "TrafficTarget",
			ShortNames: []string{"tt"},
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
							"clusters",
						},
						Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
							"clusters": apiextensionv1beta1.JSONSchemaProps{
								Type: "array",
								Items: &apiextensionv1beta1.JSONSchemaPropsOrArray{
									Schema: &apiextensionv1beta1.JSONSchemaProps{
										Type: "object",
										Required: []string{
											"name",
											"weight",
										},
										Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
											"name": apiextensionv1beta1.JSONSchemaProps{
												Type: "string",
											},
											"weight": apiextensionv1beta1.JSONSchemaProps{
												Type:    "integer",
												Minimum: &zero,
											},
										},
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
