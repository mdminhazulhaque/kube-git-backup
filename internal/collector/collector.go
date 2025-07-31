package collector

import (
	"context"
	"fmt"
	"log"

	"kube-git-backup/internal/config"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Resource represents a Kubernetes resource
type Resource struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
	Object     runtime.Object
}

// KubernetesCollector collects resources from Kubernetes cluster
type KubernetesCollector struct {
	clientset     kubernetes.Interface
	dynamicClient dynamic.Interface
	config        *config.Config
}

// NewKubernetesCollector creates a new KubernetesCollector
func NewKubernetesCollector(cfg *config.Config) (*KubernetesCollector, error) {
	// Try in-cluster config first, then fall back to kubeconfig
	var kubeConfig *rest.Config
	var err error

	kubeConfig, err = rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig file
		kubeConfig, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create Kubernetes config: %w", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	return &KubernetesCollector{
		clientset:     clientset,
		dynamicClient: dynamicClient,
		config:        cfg,
	}, nil
}

// CollectResources collects all specified resources from the cluster
func (kc *KubernetesCollector) CollectResources(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	// Define resource types to collect
	resourceTypes := map[string]func(context.Context) ([]Resource, error){
		"namespaces":             kc.collectNamespaces,
		"deployments":            kc.collectDeployments,
		"daemonsets":             kc.collectDaemonSets,
		"statefulsets":           kc.collectStatefulSets,
		"services":               kc.collectServices,
		"configmaps":             kc.collectConfigMaps,
		"secrets":                kc.collectSecrets,
		"ingresses":              kc.collectIngresses,
		"persistentvolumes":      kc.collectPersistentVolumes,
		"persistentvolumeclaims": kc.collectPersistentVolumeClaims,
		"storageclasses":         kc.collectStorageClasses,
		"serviceaccounts":        kc.collectServiceAccounts,
		"roles":                  kc.collectRoles,
		"rolebindings":           kc.collectRoleBindings,
		"clusterroles":           kc.collectClusterRoles,
		"clusterrolebindings":    kc.collectClusterRoleBindings,
		"networkpolicies":        kc.collectNetworkPolicies,
	}

	// Collect included resources
	for resourceType, collectFunc := range resourceTypes {
		if kc.shouldIncludeResource(resourceType) {
			log.Printf("Collecting %s...", resourceType)
			collected, err := collectFunc(ctx)
			if err != nil {
				log.Printf("Failed to collect %s: %v", resourceType, err)
				continue
			}
			resources = append(resources, collected...)
			log.Printf("Collected %d %s", len(collected), resourceType)
		}
	}

	return resources, nil
}

// shouldIncludeResource checks if a resource type should be included
func (kc *KubernetesCollector) shouldIncludeResource(resourceType string) bool {
	// Check exclude list first
	for _, excluded := range kc.config.Kubernetes.ExcludeResources {
		if excluded == resourceType {
			return false
		}
	}

	// If include list is empty, include all (except excluded)
	if len(kc.config.Kubernetes.IncludeResources) == 0 {
		return true
	}

	// Check include list
	for _, included := range kc.config.Kubernetes.IncludeResources {
		if included == resourceType {
			return true
		}
	}

	return false
}

// shouldIncludeNamespace checks if a namespace should be included
func (kc *KubernetesCollector) shouldIncludeNamespace(namespace string) bool {
	// If no specific namespaces configured, include all
	if len(kc.config.Kubernetes.Namespaces) == 0 {
		return true
	}

	for _, ns := range kc.config.Kubernetes.Namespaces {
		if ns == namespace {
			return true
		}
	}
	return false
}

// Namespace collection
func (kc *KubernetesCollector) collectNamespaces(ctx context.Context) ([]Resource, error) {
	namespaces, err := kc.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var resources []Resource
	for _, ns := range namespaces.Items {
		if kc.shouldIncludeNamespace(ns.Name) {
			resources = append(resources, Resource{
				APIVersion: "v1",
				Kind:       "Namespace",
				Namespace:  "",
				Name:       ns.Name,
				Object:     &ns,
			})
		}
	}
	return resources, nil
}

// Deployment collection
func (kc *KubernetesCollector) collectDeployments(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	if len(kc.config.Kubernetes.Namespaces) == 0 {
		// Collect from all namespaces
		deployments, err := kc.clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, deploy := range deployments.Items {
			if kc.shouldIncludeNamespace(deploy.Namespace) {
				resources = append(resources, Resource{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Namespace:  deploy.Namespace,
					Name:       deploy.Name,
					Object:     &deploy,
				})
			}
		}
	} else {
		// Collect from specific namespaces
		for _, ns := range kc.config.Kubernetes.Namespaces {
			deployments, err := kc.clientset.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				log.Printf("Failed to list deployments in namespace %s: %v", ns, err)
				continue
			}
			for _, deploy := range deployments.Items {
				resources = append(resources, Resource{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Namespace:  deploy.Namespace,
					Name:       deploy.Name,
					Object:     &deploy,
				})
			}
		}
	}

	return resources, nil
}

// DaemonSet collection
func (kc *KubernetesCollector) collectDaemonSets(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	if len(kc.config.Kubernetes.Namespaces) == 0 {
		daemonsets, err := kc.clientset.AppsV1().DaemonSets("").List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, ds := range daemonsets.Items {
			if kc.shouldIncludeNamespace(ds.Namespace) {
				resources = append(resources, Resource{
					APIVersion: "apps/v1",
					Kind:       "DaemonSet",
					Namespace:  ds.Namespace,
					Name:       ds.Name,
					Object:     &ds,
				})
			}
		}
	} else {
		for _, ns := range kc.config.Kubernetes.Namespaces {
			daemonsets, err := kc.clientset.AppsV1().DaemonSets(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				log.Printf("Failed to list daemonsets in namespace %s: %v", ns, err)
				continue
			}
			for _, ds := range daemonsets.Items {
				resources = append(resources, Resource{
					APIVersion: "apps/v1",
					Kind:       "DaemonSet",
					Namespace:  ds.Namespace,
					Name:       ds.Name,
					Object:     &ds,
				})
			}
		}
	}

	return resources, nil
}

// StatefulSet collection
func (kc *KubernetesCollector) collectStatefulSets(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	if len(kc.config.Kubernetes.Namespaces) == 0 {
		statefulsets, err := kc.clientset.AppsV1().StatefulSets("").List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, sts := range statefulsets.Items {
			if kc.shouldIncludeNamespace(sts.Namespace) {
				resources = append(resources, Resource{
					APIVersion: "apps/v1",
					Kind:       "StatefulSet",
					Namespace:  sts.Namespace,
					Name:       sts.Name,
					Object:     &sts,
				})
			}
		}
	} else {
		for _, ns := range kc.config.Kubernetes.Namespaces {
			statefulsets, err := kc.clientset.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				log.Printf("Failed to list statefulsets in namespace %s: %v", ns, err)
				continue
			}
			for _, sts := range statefulsets.Items {
				resources = append(resources, Resource{
					APIVersion: "apps/v1",
					Kind:       "StatefulSet",
					Namespace:  sts.Namespace,
					Name:       sts.Name,
					Object:     &sts,
				})
			}
		}
	}

	return resources, nil
}

// Service collection
func (kc *KubernetesCollector) collectServices(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	if len(kc.config.Kubernetes.Namespaces) == 0 {
		services, err := kc.clientset.CoreV1().Services("").List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, svc := range services.Items {
			if kc.shouldIncludeNamespace(svc.Namespace) {
				resources = append(resources, Resource{
					APIVersion: "v1",
					Kind:       "Service",
					Namespace:  svc.Namespace,
					Name:       svc.Name,
					Object:     &svc,
				})
			}
		}
	} else {
		for _, ns := range kc.config.Kubernetes.Namespaces {
			services, err := kc.clientset.CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				log.Printf("Failed to list services in namespace %s: %v", ns, err)
				continue
			}
			for _, svc := range services.Items {
				resources = append(resources, Resource{
					APIVersion: "v1",
					Kind:       "Service",
					Namespace:  svc.Namespace,
					Name:       svc.Name,
					Object:     &svc,
				})
			}
		}
	}

	return resources, nil
}

// ConfigMap collection
func (kc *KubernetesCollector) collectConfigMaps(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	if len(kc.config.Kubernetes.Namespaces) == 0 {
		configmaps, err := kc.clientset.CoreV1().ConfigMaps("").List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, cm := range configmaps.Items {
			if kc.shouldIncludeNamespace(cm.Namespace) && cm.Name != "kube-root-ca.crt" {
				resources = append(resources, Resource{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Namespace:  cm.Namespace,
					Name:       cm.Name,
					Object:     &cm,
				})
			}
		}
	} else {
		for _, ns := range kc.config.Kubernetes.Namespaces {
			configmaps, err := kc.clientset.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				log.Printf("Failed to list configmaps in namespace %s: %v", ns, err)
				continue
			}
			for _, cm := range configmaps.Items {
				if cm.Name != "kube-root-ca.crt" {
					resources = append(resources, Resource{
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Namespace:  cm.Namespace,
						Name:       cm.Name,
						Object:     &cm,
					})
				}
			}
		}
	}

	return resources, nil
}

// Secret collection
func (kc *KubernetesCollector) collectSecrets(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	if len(kc.config.Kubernetes.Namespaces) == 0 {
		secrets, err := kc.clientset.CoreV1().Secrets("").List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, secret := range secrets.Items {
			if kc.shouldIncludeNamespace(secret.Namespace) {
				resources = append(resources, Resource{
					APIVersion: "v1",
					Kind:       "Secret",
					Namespace:  secret.Namespace,
					Name:       secret.Name,
					Object:     &secret,
				})
			}
		}
	} else {
		for _, ns := range kc.config.Kubernetes.Namespaces {
			secrets, err := kc.clientset.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				log.Printf("Failed to list secrets in namespace %s: %v", ns, err)
				continue
			}
			for _, secret := range secrets.Items {
				resources = append(resources, Resource{
					APIVersion: "v1",
					Kind:       "Secret",
					Namespace:  secret.Namespace,
					Name:       secret.Name,
					Object:     &secret,
				})
			}
		}
	}

	return resources, nil
}

// Ingress collection
func (kc *KubernetesCollector) collectIngresses(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	if len(kc.config.Kubernetes.Namespaces) == 0 {
		ingresses, err := kc.clientset.NetworkingV1().Ingresses("").List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, ing := range ingresses.Items {
			if kc.shouldIncludeNamespace(ing.Namespace) {
				resources = append(resources, Resource{
					APIVersion: "networking.k8s.io/v1",
					Kind:       "Ingress",
					Namespace:  ing.Namespace,
					Name:       ing.Name,
					Object:     &ing,
				})
			}
		}
	} else {
		for _, ns := range kc.config.Kubernetes.Namespaces {
			ingresses, err := kc.clientset.NetworkingV1().Ingresses(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				log.Printf("Failed to list ingresses in namespace %s: %v", ns, err)
				continue
			}
			for _, ing := range ingresses.Items {
				resources = append(resources, Resource{
					APIVersion: "networking.k8s.io/v1",
					Kind:       "Ingress",
					Namespace:  ing.Namespace,
					Name:       ing.Name,
					Object:     &ing,
				})
			}
		}
	}

	return resources, nil
}

// PersistentVolume collection (cluster-scoped)
func (kc *KubernetesCollector) collectPersistentVolumes(ctx context.Context) ([]Resource, error) {
	pvs, err := kc.clientset.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var resources []Resource
	for _, pv := range pvs.Items {
		resources = append(resources, Resource{
			APIVersion: "v1",
			Kind:       "PersistentVolume",
			Namespace:  "",
			Name:       pv.Name,
			Object:     &pv,
		})
	}
	return resources, nil
}

// PersistentVolumeClaim collection
func (kc *KubernetesCollector) collectPersistentVolumeClaims(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	if len(kc.config.Kubernetes.Namespaces) == 0 {
		pvcs, err := kc.clientset.CoreV1().PersistentVolumeClaims("").List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, pvc := range pvcs.Items {
			if kc.shouldIncludeNamespace(pvc.Namespace) {
				resources = append(resources, Resource{
					APIVersion: "v1",
					Kind:       "PersistentVolumeClaim",
					Namespace:  pvc.Namespace,
					Name:       pvc.Name,
					Object:     &pvc,
				})
			}
		}
	} else {
		for _, ns := range kc.config.Kubernetes.Namespaces {
			pvcs, err := kc.clientset.CoreV1().PersistentVolumeClaims(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				log.Printf("Failed to list pvcs in namespace %s: %v", ns, err)
				continue
			}
			for _, pvc := range pvcs.Items {
				resources = append(resources, Resource{
					APIVersion: "v1",
					Kind:       "PersistentVolumeClaim",
					Namespace:  pvc.Namespace,
					Name:       pvc.Name,
					Object:     &pvc,
				})
			}
		}
	}

	return resources, nil
}

// StorageClass collection (cluster-scoped)
func (kc *KubernetesCollector) collectStorageClasses(ctx context.Context) ([]Resource, error) {
	scs, err := kc.clientset.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var resources []Resource
	for _, sc := range scs.Items {
		resources = append(resources, Resource{
			APIVersion: "storage.k8s.io/v1",
			Kind:       "StorageClass",
			Namespace:  "",
			Name:       sc.Name,
			Object:     &sc,
		})
	}
	return resources, nil
}

// ServiceAccount collection
func (kc *KubernetesCollector) collectServiceAccounts(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	if len(kc.config.Kubernetes.Namespaces) == 0 {
		sas, err := kc.clientset.CoreV1().ServiceAccounts("").List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, sa := range sas.Items {
			if kc.shouldIncludeNamespace(sa.Namespace) {
				resources = append(resources, Resource{
					APIVersion: "v1",
					Kind:       "ServiceAccount",
					Namespace:  sa.Namespace,
					Name:       sa.Name,
					Object:     &sa,
				})
			}
		}
	} else {
		for _, ns := range kc.config.Kubernetes.Namespaces {
			sas, err := kc.clientset.CoreV1().ServiceAccounts(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				log.Printf("Failed to list serviceaccounts in namespace %s: %v", ns, err)
				continue
			}
			for _, sa := range sas.Items {
				resources = append(resources, Resource{
					APIVersion: "v1",
					Kind:       "ServiceAccount",
					Namespace:  sa.Namespace,
					Name:       sa.Name,
					Object:     &sa,
				})
			}
		}
	}

	return resources, nil
}

// Role collection
func (kc *KubernetesCollector) collectRoles(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	if len(kc.config.Kubernetes.Namespaces) == 0 {
		roles, err := kc.clientset.RbacV1().Roles("").List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, role := range roles.Items {
			if kc.shouldIncludeNamespace(role.Namespace) {
				resources = append(resources, Resource{
					APIVersion: "rbac.authorization.k8s.io/v1",
					Kind:       "Role",
					Namespace:  role.Namespace,
					Name:       role.Name,
					Object:     &role,
				})
			}
		}
	} else {
		for _, ns := range kc.config.Kubernetes.Namespaces {
			roles, err := kc.clientset.RbacV1().Roles(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				log.Printf("Failed to list roles in namespace %s: %v", ns, err)
				continue
			}
			for _, role := range roles.Items {
				resources = append(resources, Resource{
					APIVersion: "rbac.authorization.k8s.io/v1",
					Kind:       "Role",
					Namespace:  role.Namespace,
					Name:       role.Name,
					Object:     &role,
				})
			}
		}
	}

	return resources, nil
}

// RoleBinding collection
func (kc *KubernetesCollector) collectRoleBindings(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	if len(kc.config.Kubernetes.Namespaces) == 0 {
		roleBindings, err := kc.clientset.RbacV1().RoleBindings("").List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, rb := range roleBindings.Items {
			if kc.shouldIncludeNamespace(rb.Namespace) {
				resources = append(resources, Resource{
					APIVersion: "rbac.authorization.k8s.io/v1",
					Kind:       "RoleBinding",
					Namespace:  rb.Namespace,
					Name:       rb.Name,
					Object:     &rb,
				})
			}
		}
	} else {
		for _, ns := range kc.config.Kubernetes.Namespaces {
			roleBindings, err := kc.clientset.RbacV1().RoleBindings(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				log.Printf("Failed to list rolebindings in namespace %s: %v", ns, err)
				continue
			}
			for _, rb := range roleBindings.Items {
				resources = append(resources, Resource{
					APIVersion: "rbac.authorization.k8s.io/v1",
					Kind:       "RoleBinding",
					Namespace:  rb.Namespace,
					Name:       rb.Name,
					Object:     &rb,
				})
			}
		}
	}

	return resources, nil
}

// ClusterRole collection (cluster-scoped)
func (kc *KubernetesCollector) collectClusterRoles(ctx context.Context) ([]Resource, error) {
	clusterRoles, err := kc.clientset.RbacV1().ClusterRoles().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var resources []Resource
	for _, cr := range clusterRoles.Items {
		resources = append(resources, Resource{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRole",
			Namespace:  "",
			Name:       cr.Name,
			Object:     &cr,
		})
	}
	return resources, nil
}

// ClusterRoleBinding collection (cluster-scoped)
func (kc *KubernetesCollector) collectClusterRoleBindings(ctx context.Context) ([]Resource, error) {
	clusterRoleBindings, err := kc.clientset.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var resources []Resource
	for _, crb := range clusterRoleBindings.Items {
		resources = append(resources, Resource{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRoleBinding",
			Namespace:  "",
			Name:       crb.Name,
			Object:     &crb,
		})
	}
	return resources, nil
}

// NetworkPolicy collection
func (kc *KubernetesCollector) collectNetworkPolicies(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	if len(kc.config.Kubernetes.Namespaces) == 0 {
		networkPolicies, err := kc.clientset.NetworkingV1().NetworkPolicies("").List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, np := range networkPolicies.Items {
			if kc.shouldIncludeNamespace(np.Namespace) {
				resources = append(resources, Resource{
					APIVersion: "networking.k8s.io/v1",
					Kind:       "NetworkPolicy",
					Namespace:  np.Namespace,
					Name:       np.Name,
					Object:     &np,
				})
			}
		}
	} else {
		for _, ns := range kc.config.Kubernetes.Namespaces {
			networkPolicies, err := kc.clientset.NetworkingV1().NetworkPolicies(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				log.Printf("Failed to list networkpolicies in namespace %s: %v", ns, err)
				continue
			}
			for _, np := range networkPolicies.Items {
				resources = append(resources, Resource{
					APIVersion: "networking.k8s.io/v1",
					Kind:       "NetworkPolicy",
					Namespace:  np.Namespace,
					Name:       np.Name,
					Object:     &np,
				})
			}
		}
	}

	return resources, nil
}
