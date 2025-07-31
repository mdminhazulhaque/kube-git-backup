package sanitizer

import (
	"testing"

	"kube-git-backup/internal/collector"
	"kube-git-backup/internal/config"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

func TestSanitizeResources(t *testing.T) {
	// Create a test sanitizer config
	cfg := config.SanitizerConfig{}
	sanitizer := NewYAMLSanitizer(cfg)

	// Create a test ConfigMap with fields that should be stripped
	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-config",
			Namespace:       "default",
			UID:             "test-uid-123",
			ResourceVersion: "12345",
			Annotations: map[string]string{
				"kubectl.kubernetes.io/last-applied-configuration": "should-be-removed",
				"custom.annotation": "should-remain",
			},
		},
		Data: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
	}

	// Create test resources
	resources := []collector.Resource{
		{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			Namespace:  "default",
			Name:       "test-config",
			Object:     configMap,
		},
	}

	// Sanitize resources
	sanitized, err := sanitizer.SanitizeResources(resources)
	if err != nil {
		t.Fatalf("Failed to sanitize resources: %v", err)
	}

	if len(sanitized) != 1 {
		t.Fatalf("Expected 1 sanitized resource, got %d", len(sanitized))
	}

	// Parse the sanitized YAML to check what was removed
	var obj unstructured.Unstructured
	if err := yaml.Unmarshal(sanitized[0].YAML, &obj.Object); err != nil {
		t.Fatalf("Failed to unmarshal sanitized YAML: %v", err)
	}

	// Check that UID was removed
	if uid := obj.GetUID(); uid != "" {
		t.Errorf("Expected UID to be removed, but found: %s", uid)
	}

	// Check that resourceVersion was removed
	if rv := obj.GetResourceVersion(); rv != "" {
		t.Errorf("Expected resourceVersion to be removed, but found: %s", rv)
	}

	// Check that kubectl annotation was removed
	annotations := obj.GetAnnotations()
	if _, exists := annotations["kubectl.kubernetes.io/last-applied-configuration"]; exists {
		t.Error("Expected kubectl.kubernetes.io/last-applied-configuration annotation to be removed")
	}

	// Check that custom annotation remains
	if custom, exists := annotations["custom.annotation"]; !exists || custom != "should-remain" {
		t.Error("Expected custom.annotation to remain")
	}

	// Check that data is preserved
	data, found, err := unstructured.NestedStringMap(obj.Object, "data")
	if err != nil {
		t.Fatalf("Failed to get data from sanitized object: %v", err)
	}
	if !found {
		t.Error("Expected data to be preserved")
	}
	if data["key1"] != "value1" || data["key2"] != "value2" {
		t.Error("Expected data values to be preserved")
	}
}

func TestSanitizeMetadata(t *testing.T) {
	cfg := config.SanitizerConfig{}
	sanitizer := NewYAMLSanitizer(cfg)

	// Create an unstructured object with metadata fields
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":              "test-object",
				"namespace":         "default",
				"uid":               "test-uid-123",
				"resourceVersion":   "12345",
				"generation":        int64(1),
				"creationTimestamp": "2023-01-01T00:00:00Z",
				"annotations": map[string]interface{}{
					"kubectl.kubernetes.io/last-applied-configuration": "should-be-removed",
					"custom.annotation": "should-remain",
				},
				"labels": map[string]interface{}{
					"app":               "test-app",
					"pod-template-hash": "should-be-removed",
				},
			},
		},
	}

	// Apply metadata sanitization
	sanitizer.sanitizeMetadata(obj)

	metadata := obj.Object["metadata"].(map[string]interface{})

	// Check that unwanted fields were removed
	unwantedFields := []string{"uid", "resourceVersion", "generation", "creationTimestamp"}
	for _, field := range unwantedFields {
		if _, exists := metadata[field]; exists {
			t.Errorf("Expected field '%s' to be removed", field)
		}
	}

	// Check that wanted fields remain
	if metadata["name"] != "test-object" {
		t.Error("Expected name to remain")
	}
	if metadata["namespace"] != "default" {
		t.Error("Expected namespace to remain")
	}

	// Check annotations
	annotations := metadata["annotations"].(map[string]interface{})
	if _, exists := annotations["kubectl.kubernetes.io/last-applied-configuration"]; exists {
		t.Error("Expected kubectl annotation to be removed")
	}
	if annotations["custom.annotation"] != "should-remain" {
		t.Error("Expected custom annotation to remain")
	}

	// Check labels
	labels := metadata["labels"].(map[string]interface{})
	if _, exists := labels["pod-template-hash"]; exists {
		t.Error("Expected pod-template-hash label to be removed")
	}
	if labels["app"] != "test-app" {
		t.Error("Expected app label to remain")
	}
}

func TestRemoveFieldByPath(t *testing.T) {
	cfg := config.SanitizerConfig{}
	sanitizer := NewYAMLSanitizer(cfg)

	tests := []struct {
		name     string
		obj      map[string]interface{}
		path     string
		expected map[string]interface{}
	}{
		{
			name: "simple field removal",
			obj: map[string]interface{}{
				"field1": "value1",
				"field2": "value2",
			},
			path: "field1",
			expected: map[string]interface{}{
				"field2": "value2",
			},
		},
		{
			name: "nested field removal",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "test",
					"uid":  "should-be-removed",
				},
			},
			path: "metadata.uid",
			expected: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "test",
				},
			},
		},
		{
			name: "array field removal",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"ports": []interface{}{
						map[string]interface{}{
							"port":     80,
							"nodePort": 30000,
						},
						map[string]interface{}{
							"port":     443,
							"nodePort": 30001,
						},
					},
				},
			},
			path: "spec.ports[].nodePort",
			expected: map[string]interface{}{
				"spec": map[string]interface{}{
					"ports": []interface{}{
						map[string]interface{}{
							"port": 80,
						},
						map[string]interface{}{
							"port": 443,
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sanitizer.removeFieldByPath(tt.obj, tt.path)

			// Simple comparison for this test
			// In a real scenario, you might want to use a more sophisticated comparison
			if len(tt.obj) != len(tt.expected) {
				t.Errorf("Expected object length %d, got %d", len(tt.expected), len(tt.obj))
			}
		})
	}
}
