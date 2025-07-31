package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

// Config holds all configuration for the kube-git-backup daemon
type Config struct {
	BackupInterval time.Duration
	WorkDir        string
	DumpOnly       bool // If true, only dump locally without Git operations
	Git            GitConfig
	Kubernetes     KubernetesConfig
	Sanitizer      SanitizerConfig
}

// GitConfig holds Git-related configuration
type GitConfig struct {
	Repository  string
	Branch      string
	AuthorName  string
	AuthorEmail string
	AuthMethod  string // "ssh" or "token"
	SSHKeyPath  string
	Token       string
}

// KubernetesConfig holds Kubernetes-related configuration
type KubernetesConfig struct {
	IncludeResources    []string
	ExcludeResources    []string
	IncludeNamespaces   []string // Empty means all namespaces
	ExcludeNamespaces   []string // Namespaces to exclude
}

// SanitizerConfig holds YAML sanitization configuration
type SanitizerConfig struct {
	// Static configuration - no configurable fields
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	// Try to load .env file if it exists
	loadEnvFile()
	
	cfg := &Config{}

	// Backup interval (default: 1 hour)
	intervalStr := getEnvOrDefault("BACKUP_INTERVAL", "1h")
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		return nil, fmt.Errorf("invalid BACKUP_INTERVAL: %w", err)
	}
	cfg.BackupInterval = interval

	// Working directory (default: /tmp/kube-backup)
	cfg.WorkDir = getEnvOrDefault("WORK_DIR", "/tmp/kube-backup")

	// Dump only mode (default: false)
	cfg.DumpOnly = getEnvOrDefault("DUMP_ONLY", "false") == "true"

	// Git configuration
	gitRepo := os.Getenv("GIT_REPOSITORY")
	
	// Auto-detect authentication method based on repository URL
	var authMethod string
	if strings.HasPrefix(gitRepo, "https://") {
		authMethod = "token"
	} else {
		authMethod = "ssh"
	}
	
	// Allow manual override if GIT_AUTH_METHOD is explicitly set
	if envAuthMethod := os.Getenv("GIT_AUTH_METHOD"); envAuthMethod != "" {
		authMethod = envAuthMethod
	}
	
	cfg.Git = GitConfig{
		Repository:  gitRepo,
		Branch:      getEnvOrDefault("GIT_BRANCH", "main"),
		AuthorName:  getEnvOrDefault("GIT_AUTHOR_NAME", "Kube Git Backup"),
		AuthorEmail: getEnvOrDefault("GIT_AUTHOR_EMAIL", "kube-backup@example.com"),
		AuthMethod:  authMethod,
		SSHKeyPath:  getEnvOrDefault("GIT_SSH_KEY_PATH", "/root/.ssh/id_rsa"),
		Token:       os.Getenv("GIT_TOKEN"),
	}

	// Kubernetes configuration
	includeStr := getEnvOrDefault("INCLUDE_RESOURCES", "deployments,daemonsets,statefulsets,services,configmaps,secrets,ingresses,namespaces,roles,rolebindings,clusterroles,clusterrolebindings,serviceaccounts,persistentvolumes,persistentvolumeclaims,storageclasses,networkpolicies")
	excludeStr := getEnvOrDefault("EXCLUDE_RESOURCES", "pods,events,endpoints,replicasets")
	includeNamespacesStr := os.Getenv("INCLUDE_NAMESPACES")
	excludeNamespacesStr := getEnvOrDefault("EXCLUDE_NAMESPACES", "kube-system,default,kube-node-lease")

	cfg.Kubernetes = KubernetesConfig{
		IncludeResources:  parseCommaSeparated(includeStr),
		ExcludeResources:  parseCommaSeparated(excludeStr),
		IncludeNamespaces: parseCommaSeparated(includeNamespacesStr),
		ExcludeNamespaces: parseCommaSeparated(excludeNamespacesStr),
	}

	// Sanitizer configuration - using static defaults
	cfg.Sanitizer = SanitizerConfig{}

	return cfg, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Skip Git validation if in dump-only mode
	if c.DumpOnly {
		if c.BackupInterval < time.Minute {
			return fmt.Errorf("BACKUP_INTERVAL must be at least 1 minute")
		}
		return nil
	}

	if c.Git.Repository == "" {
		return fmt.Errorf("GIT_REPOSITORY is required")
	}

	if c.Git.AuthMethod == "token" && c.Git.Token == "" {
		return fmt.Errorf("GIT_TOKEN is required when using token authentication")
	}

	if c.Git.AuthMethod == "ssh" && c.Git.SSHKeyPath == "" {
		return fmt.Errorf("GIT_SSH_KEY_PATH is required when using SSH authentication")
	}

	if c.Git.AuthMethod != "ssh" && c.Git.AuthMethod != "token" {
		return fmt.Errorf("GIT_AUTH_METHOD must be either 'ssh' or 'token'")
	}

	if c.BackupInterval < time.Minute {
		return fmt.Errorf("BACKUP_INTERVAL must be at least 1 minute")
	}

	return nil
}

// getEnvOrDefault returns the environment variable value or a default value
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// parseCommaSeparated parses a comma-separated string into a slice
func parseCommaSeparated(s string) []string {
	if s == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// loadEnvFile loads environment variables from .env file if it exists
func loadEnvFile() {
	file, err := os.Open(".env")
	if err != nil {
		// .env file doesn't exist or can't be opened, continue without it
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		
		// Parse KEY=VALUE format
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		
		// Only set if environment variable is not already set
		if os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
}
