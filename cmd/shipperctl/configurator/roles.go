package configurator

import (
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func getManagementClusterRole(name, domain string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Rules: []rbacv1.PolicyRule{
			{
				Verbs:     []string{rbacv1.VerbAll},
				APIGroups: []string{shipper.SchemeGroupVersion.Group},
				Resources: []string{rbacv1.ResourceAll},
			},
			{
				Verbs:     []string{"update", "get", "list", "watch"},
				APIGroups: []string{""},
				Resources: []string{"secrets"},
			},
			{
				Verbs:     []string{rbacv1.VerbAll},
				APIGroups: []string{""},
				Resources: []string{"events"},
			},
			{
				Verbs:     []string{"get", "list"},
				APIGroups: []string{""},
				Resources: []string{"namespaces"},
			},
		},
	}
}

func getApplicationClusterRole(name, domain string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Rules: []rbacv1.PolicyRule{
			{
				Verbs: []string{
					"get",
					"list",
					"watch",
					"patch",
					"delete",
					"update",
					"create",
					"deletecollection",
				},
				APIGroups: []string{
					"",
					"extensions",
					"apps",
					"batch",
					rbacv1.GroupName,
				},
				Resources: []string{
					"pods",
					"pods/log",
					"services",
					"deployments",
					"replicasets",
					"statefulsets",
					"secrets",
					"configmaps",
					"jobs",
					"cronjobs",
					"persistentvolumeclaims",
					"endpoints",
					"rolebindings",
					"roles",
					"serviceaccounts",
				},
			},
			{
				Verbs:     []string{"get", "list"},
				APIGroups: []string{""},
				Resources: []string{"namespaces"},
			},
		},
	}
}
