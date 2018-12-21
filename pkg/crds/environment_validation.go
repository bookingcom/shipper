package crds

import (
	apiextensionv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
)

var environmentValidation = apiextensionv1beta1.JSONSchemaProps{
	Type: "object",
	Required: []string{
		"clusterRequirements",
		"strategy",
		"chart",
		"values",
	},
	Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
		"chart": apiextensionv1beta1.JSONSchemaProps{
			Type: "object",
			Required: []string{
				"name",
				"version",
				"repoUrl",
			},
			Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
				"name": apiextensionv1beta1.JSONSchemaProps{
					Type: "string",
				},
				"version": apiextensionv1beta1.JSONSchemaProps{
					Type: "string",
				},
				"repoUrl": apiextensionv1beta1.JSONSchemaProps{
					Type: "string",
				},
			},
		},
		"clusterRequirements": apiextensionv1beta1.JSONSchemaProps{
			Type: "object",
			Required: []string{
				"regions",
			},
			Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
				"regions": apiextensionv1beta1.JSONSchemaProps{
					Type: "array",
					Items: &apiextensionv1beta1.JSONSchemaPropsOrArray{
						Schema: &apiextensionv1beta1.JSONSchemaProps{
							Type: "object",
						},
					},
				},
				"capabilities": apiextensionv1beta1.JSONSchemaProps{
					Type: "array",
					Items: &apiextensionv1beta1.JSONSchemaPropsOrArray{
						Schema: &apiextensionv1beta1.JSONSchemaProps{
							Type: "string",
						},
					},
				},
			},
		},
		"strategy": apiextensionv1beta1.JSONSchemaProps{
			Type: "object",
			Required: []string{
				"steps",
			},
			Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
				"steps": apiextensionv1beta1.JSONSchemaProps{
					Type: "array",
					Items: &apiextensionv1beta1.JSONSchemaPropsOrArray{
						Schema: &apiextensionv1beta1.JSONSchemaProps{
							Type: "object",
							Required: []string{
								"name",
								"traffic",
								"capacity",
							},
							Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
								"name": apiextensionv1beta1.JSONSchemaProps{
									Type: "string",
								},
								"capacity": apiextensionv1beta1.JSONSchemaProps{
									Type: "object",
									Required: []string{
										"incumbent",
										"contender",
									},
									Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
										"incumbent": apiextensionv1beta1.JSONSchemaProps{
											Type:    "integer",
											Minimum: &zero,
											Maximum: &hundred,
										},
										"contender": apiextensionv1beta1.JSONSchemaProps{
											Type:    "integer",
											Minimum: &zero,
											Maximum: &hundred,
										},
									},
								},
								"traffic": apiextensionv1beta1.JSONSchemaProps{
									Type: "object",
									Required: []string{
										"incumbent",
										"contender",
									},
									Properties: map[string]apiextensionv1beta1.JSONSchemaProps{
										"incumbent": apiextensionv1beta1.JSONSchemaProps{
											Type:    "integer",
											Minimum: &zero,
										},
										"contender": apiextensionv1beta1.JSONSchemaProps{
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
		"values": apiextensionv1beta1.JSONSchemaProps{
			Type: "object",
		},
	},
}
