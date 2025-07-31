package sanitizer

import (
	"fmt"
	"strings"

	"kube-git-backup/internal/collector"
	"kube-git-backup/internal/config"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

// YAMLSanitizer sanitizes Kubernetes YAML resources
type YAMLSanitizer struct {
	config *config.SanitizerConfig
}

// SanitizedResource represents a sanitized Kubernetes resource
type SanitizedResource struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
	YAML       []byte
}

// NewYAMLSanitizer creates a new YAMLSanitizer
func NewYAMLSanitizer(cfg config.SanitizerConfig) *YAMLSanitizer {
	return &YAMLSanitizer{
		config: &cfg,
	}
}

// SanitizeResources sanitizes a list of Kubernetes resources
func (ys *YAMLSanitizer) SanitizeResources(resources []collector.Resource) ([]SanitizedResource, error) {
	var sanitized []SanitizedResource

	for _, resource := range resources {
		sanitizedResource, err := ys.sanitizeResource(resource)
		if err != nil {
			return nil, fmt.Errorf("failed to sanitize resource %s/%s: %w",
				resource.Namespace, resource.Name, err)
		}
		sanitized = append(sanitized, sanitizedResource)
	}

	return sanitized, nil
}

// sanitizeResource sanitizes a single Kubernetes resource
func (ys *YAMLSanitizer) sanitizeResource(resource collector.Resource) (SanitizedResource, error) {
	// Convert runtime.Object to unstructured.Unstructured for easier manipulation
	unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(resource.Object)
	if err != nil {
		return SanitizedResource{}, fmt.Errorf("failed to convert to unstructured: %w", err)
	}

	unstructured := &unstructured.Unstructured{Object: unstructuredObj}

	// Apply sanitization rules
	ys.sanitizeMetadata(unstructured)
	ys.sanitizeSpec(unstructured)
	ys.sanitizeStatus(unstructured)
	ys.applyCustomStripFields(unstructured)

	// Convert back to YAML
	yamlBytes, err := yaml.Marshal(unstructured.Object)
	if err != nil {
		return SanitizedResource{}, fmt.Errorf("failed to marshal to YAML: %w", err)
	}

	return SanitizedResource{
		APIVersion: resource.APIVersion,
		Kind:       resource.Kind,
		Namespace:  resource.Namespace,
		Name:       resource.Name,
		YAML:       yamlBytes,
	}, nil
}

// sanitizeMetadata removes unwanted metadata fields
func (ys *YAMLSanitizer) sanitizeMetadata(obj *unstructured.Unstructured) {
	metadata := obj.Object["metadata"]
	if metadata == nil {
		return
	}

	metadataMap, ok := metadata.(map[string]interface{})
	if !ok {
		return
	}

	// Remove common metadata fields that shouldn't be in backups
	fieldsToRemove := []string{
		"uid",
		"selfLink",
		"resourceVersion",
		"generation",
		"creationTimestamp",
		"deletionTimestamp",
		"deletionGracePeriodSeconds",
		"managedFields",
	}

	for _, field := range fieldsToRemove {
		delete(metadataMap, field)
	}

	// Handle annotations
	if annotations, exists := metadataMap["annotations"]; exists {
		if annotationsMap, ok := annotations.(map[string]interface{}); ok {
			// Remove kubectl last-applied-configuration annotation
			delete(annotationsMap, "kubectl.kubernetes.io/last-applied-configuration")
			delete(annotationsMap, "deployment.kubernetes.io/revision")

			// Remove if empty
			if len(annotationsMap) == 0 {
				delete(metadataMap, "annotations")
			}
		}
	}

	// Handle labels - keep all labels as they're usually important
	// Only remove system-generated labels that change frequently
	if labels, exists := metadataMap["labels"]; exists {
		if labelsMap, ok := labels.(map[string]interface{}); ok {
			// Remove pod template hash which changes on updates
			delete(labelsMap, "pod-template-hash")

			// Remove if empty
			if len(labelsMap) == 0 {
				delete(metadataMap, "labels")
			}
		}
	}
}

// sanitizeSpec removes unwanted spec fields
func (ys *YAMLSanitizer) sanitizeSpec(obj *unstructured.Unstructured) {
	spec := obj.Object["spec"]
	if spec == nil {
		return
	}

	specMap, ok := spec.(map[string]interface{})
	if !ok {
		return
	}

	// Remove service-specific fields that are auto-assigned
	if obj.GetKind() == "Service" {
		delete(specMap, "clusterIP")
		delete(specMap, "clusterIPs")

		// Remove nodePort from ports if present
		if ports, exists := specMap["ports"]; exists {
			if portsSlice, ok := ports.([]interface{}); ok {
				for _, port := range portsSlice {
					if portMap, ok := port.(map[string]interface{}); ok {
						delete(portMap, "nodePort")
					}
				}
			}
		}
	}

	// Remove PVC-specific fields that are auto-assigned
	if obj.GetKind() == "PersistentVolumeClaim" {
		delete(specMap, "volumeName")
		delete(specMap, "volumeMode")
	}

	// Remove PV-specific fields that are auto-assigned or cluster-specific
	if obj.GetKind() == "PersistentVolume" {
		delete(specMap, "claimRef")
	}
}

// sanitizeStatus removes the entire status section
func (ys *YAMLSanitizer) sanitizeStatus(obj *unstructured.Unstructured) {
	delete(obj.Object, "status")
}

// applyCustomStripFields applies static field stripping rules
func (ys *YAMLSanitizer) applyCustomStripFields(obj *unstructured.Unstructured) {
	// Static list of fields to strip from all resources
	staticStripFields := []string{
		"metadata.uid",
		"metadata.selfLink",
		"metadata.resourceVersion", 
		"metadata.generation",
		"metadata.creationTimestamp",
		"metadata.annotations[kubectl.kubernetes.io/last-applied-configuration]",
		"metadata.annotations[deployment.kubernetes.io/revision]",
		"status",
		"spec.clusterIP",
		"spec.clusterIPs",
		"spec.ports[].nodePort",
	}
	
	for _, fieldPath := range staticStripFields {
		ys.removeFieldByPath(obj.Object, fieldPath)
	}
}

// removeFieldByPath removes a field specified by a dot-separated path
func (ys *YAMLSanitizer) removeFieldByPath(obj map[string]interface{}, path string) {
	if path == "" {
		return
	}

	parts := strings.Split(path, ".")
	if len(parts) == 1 {
		delete(obj, parts[0])
		return
	}

	// Handle nested paths
	current := obj
	for i, part := range parts[:len(parts)-1] {
		// Handle array notation like "ports[].nodePort"
		if strings.Contains(part, "[]") {
			arrayField := strings.TrimSuffix(part, "[]")
			if arrayValue, exists := current[arrayField]; exists {
				if arraySlice, ok := arrayValue.([]interface{}); ok {
					remainingPath := strings.Join(parts[i+1:], ".")
					for _, item := range arraySlice {
						if itemMap, ok := item.(map[string]interface{}); ok {
							ys.removeFieldByPath(itemMap, remainingPath)
						}
					}
				}
			}
			return
		}

		// Handle special annotation syntax like "annotations[key]"
		if strings.Contains(part, "[") && strings.Contains(part, "]") {
			fieldName := part[:strings.Index(part, "[")]
			key := part[strings.Index(part, "[")+1 : strings.Index(part, "]")]

			if fieldValue, exists := current[fieldName]; exists {
				if fieldMap, ok := fieldValue.(map[string]interface{}); ok {
					delete(fieldMap, key)
				}
			}
			return
		}

		// Regular nested field
		if nextLevel, exists := current[part]; exists {
			if nextMap, ok := nextLevel.(map[string]interface{}); ok {
				current = nextMap
			} else {
				return // Can't traverse further
			}
		} else {
			return // Path doesn't exist
		}
	}

	// Remove the final field
	finalField := parts[len(parts)-1]
	delete(current, finalField)
}
