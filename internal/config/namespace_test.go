package config

import (
	"os"
	"testing"
)

func TestNamespaceFiltering(t *testing.T) {
	tests := []struct {
		name                string
		includeNamespaces   string
		excludeNamespaces   string
		testNamespace      string
		expectedIncluded   bool
	}{
		{
			name:               "exclude system namespaces by default",
			includeNamespaces:  "",
			excludeNamespaces:  "kube-system,default,kube-node-lease",
			testNamespace:     "kube-system",
			expectedIncluded:  false,
		},
		{
			name:               "include user namespace when exclude list set",
			includeNamespaces:  "",
			excludeNamespaces:  "kube-system,default,kube-node-lease",
			testNamespace:     "my-app",
			expectedIncluded:  true,
		},
		{
			name:               "only include specified namespaces",
			includeNamespaces:  "production,staging",
			excludeNamespaces:  "",
			testNamespace:     "production",
			expectedIncluded:  true,
		},
		{
			name:               "exclude namespace not in include list",
			includeNamespaces:  "production,staging",
			excludeNamespaces:  "",
			testNamespace:     "development",
			expectedIncluded:  false,
		},
		{
			name:               "exclude takes precedence over include",
			includeNamespaces:  "production,staging,kube-system",
			excludeNamespaces:  "kube-system",
			testNamespace:     "kube-system",
			expectedIncluded:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			if tt.includeNamespaces != "" {
				os.Setenv("INCLUDE_NAMESPACES", tt.includeNamespaces)
			} else {
				os.Unsetenv("INCLUDE_NAMESPACES")
			}
			
			if tt.excludeNamespaces != "" {
				os.Setenv("EXCLUDE_NAMESPACES", tt.excludeNamespaces)
			} else {
				os.Unsetenv("EXCLUDE_NAMESPACES")
			}

			// Load config
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Failed to load config: %v", err)
			}

			// Test shouldIncludeNamespace logic
			shouldInclude := shouldIncludeNamespaceLogic(
				tt.testNamespace,
				cfg.Kubernetes.IncludeNamespaces,
				cfg.Kubernetes.ExcludeNamespaces,
			)

			if shouldInclude != tt.expectedIncluded {
				t.Errorf("Expected %v for namespace %s, got %v", 
					tt.expectedIncluded, tt.testNamespace, shouldInclude)
			}
		})
	}
}

// Helper function to test the namespace filtering logic
func shouldIncludeNamespaceLogic(namespace string, includeList, excludeList []string) bool {
	// Check exclude list first (explicit exclusions)
	for _, excluded := range excludeList {
		if excluded == namespace {
			return false
		}
	}

	// If include list is specified, only include those namespaces
	if len(includeList) > 0 {
		for _, included := range includeList {
			if included == namespace {
				return true
			}
		}
		return false
	}

	// If no include list specified, include all (except excluded ones)
	return true
}
