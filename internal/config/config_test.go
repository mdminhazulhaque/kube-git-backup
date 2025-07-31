package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	// Set up test environment variables
	envVars := map[string]string{
		"GIT_REPOSITORY":    "git@github.com:test/repo.git",
		"GIT_BRANCH":        "test-branch",
		"GIT_AUTHOR_NAME":   "Test Author",
		"GIT_AUTHOR_EMAIL":  "test@example.com",
		"GIT_AUTH_METHOD":   "ssh",
		"BACKUP_INTERVAL":   "30m",
		"INCLUDE_RESOURCES": "deployments,services",
		"EXCLUDE_RESOURCES": "pods,events",
	}

	// Set environment variables
	for key, value := range envVars {
		os.Setenv(key, value)
	}
	defer func() {
		// Clean up environment variables
		for key := range envVars {
			os.Unsetenv(key)
		}
	}()

	// Test loading configuration
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Failed to load configuration: %v", err)
	}

	// Test Git configuration
	if cfg.Git.Repository != "git@github.com:test/repo.git" {
		t.Errorf("Expected repository 'git@github.com:test/repo.git', got '%s'", cfg.Git.Repository)
	}

	if cfg.Git.Branch != "test-branch" {
		t.Errorf("Expected branch 'test-branch', got '%s'", cfg.Git.Branch)
	}

	if cfg.Git.AuthorName != "Test Author" {
		t.Errorf("Expected author name 'Test Author', got '%s'", cfg.Git.AuthorName)
	}

	if cfg.Git.AuthMethod != "ssh" {
		t.Errorf("Expected auth method 'ssh', got '%s'", cfg.Git.AuthMethod)
	}

	// Test backup interval
	expectedInterval := 30 * time.Minute
	if cfg.BackupInterval != expectedInterval {
		t.Errorf("Expected interval %v, got %v", expectedInterval, cfg.BackupInterval)
	}

	// Test resource configuration
	expectedInclude := []string{"deployments", "services"}
	if len(cfg.Kubernetes.IncludeResources) != len(expectedInclude) {
		t.Errorf("Expected %d include resources, got %d", len(expectedInclude), len(cfg.Kubernetes.IncludeResources))
	}

	for i, resource := range expectedInclude {
		if cfg.Kubernetes.IncludeResources[i] != resource {
			t.Errorf("Expected include resource '%s', got '%s'", resource, cfg.Kubernetes.IncludeResources[i])
		}
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid ssh config",
			config: &Config{
				BackupInterval: time.Hour,
				Git: GitConfig{
					Repository:  "git@github.com:test/repo.git",
					AuthMethod:  "ssh",
					SSHKeyPath:  "/path/to/key",
					AuthorName:  "Test",
					AuthorEmail: "test@example.com",
				},
			},
			expectError: false,
		},
		{
			name: "valid token config",
			config: &Config{
				BackupInterval: time.Hour,
				Git: GitConfig{
					Repository:  "https://github.com/test/repo.git",
					AuthMethod:  "token",
					Token:       "test-token",
					AuthorName:  "Test",
					AuthorEmail: "test@example.com",
				},
			},
			expectError: false,
		},
		{
			name: "missing repository",
			config: &Config{
				BackupInterval: time.Hour,
				Git: GitConfig{
					AuthMethod: "ssh",
				},
			},
			expectError: true,
			errorMsg:    "GIT_REPOSITORY is required",
		},
		{
			name: "invalid auth method",
			config: &Config{
				BackupInterval: time.Hour,
				Git: GitConfig{
					Repository: "git@github.com:test/repo.git",
					AuthMethod: "invalid",
				},
			},
			expectError: true,
			errorMsg:    "GIT_AUTH_METHOD must be either 'ssh' or 'token'",
		},
		{
			name: "too short interval",
			config: &Config{
				BackupInterval: 30 * time.Second,
				Git: GitConfig{
					Repository: "git@github.com:test/repo.git",
					AuthMethod: "ssh",
					SSHKeyPath: "/path/to/key",
				},
			},
			expectError: true,
			errorMsg:    "BACKUP_INTERVAL must be at least 1 minute",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if err.Error() != tt.errorMsg {
					t.Errorf("Expected error '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestParseCommaSeparated(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", []string{}},
		{"single", []string{"single"}},
		{"one,two,three", []string{"one", "two", "three"}},
		{"  spaced  ,  values  ", []string{"spaced", "values"}},
		{"mixed, spaced,values", []string{"mixed", "spaced", "values"}},
	}

	for _, tt := range tests {
		result := parseCommaSeparated(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("For input '%s', expected %d items, got %d", tt.input, len(tt.expected), len(result))
			continue
		}
		for i, expected := range tt.expected {
			if result[i] != expected {
				t.Errorf("For input '%s', expected item %d to be '%s', got '%s'", tt.input, i, expected, result[i])
			}
		}
	}
}
