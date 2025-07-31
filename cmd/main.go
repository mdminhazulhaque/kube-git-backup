package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"kube-git-backup/internal/collector"
	"kube-git-backup/internal/config"
	"kube-git-backup/internal/git"
	"kube-git-backup/internal/sanitizer"
)

func main() {
	log.Println("Starting Kube Git Backup daemon...")

	// Load configuration from environment variables
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	log.Printf("Configuration loaded: interval=%s, dump-only=%v", 
		cfg.BackupInterval, cfg.DumpOnly)
	
	if !cfg.DumpOnly {
		log.Printf("Git repository: %s, branch: %s, auth-method: %s", 
			cfg.Git.Repository, cfg.Git.Branch, cfg.Git.AuthMethod)
	}	// Initialize Kubernetes client
	kubeCollector, err := collector.NewKubernetesCollector(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize Kubernetes collector: %v", err)
	}

	// Initialize Git manager (skip if dump-only mode)
	var gitManager *git.Manager
	if !cfg.DumpOnly {
		var err error
		gitManager, err = git.NewManager(cfg.Git)
		if err != nil {
			log.Fatalf("Failed to initialize Git manager: %v", err)
		}
	}

	// Initialize YAML sanitizer
	yamlSanitizer := sanitizer.NewYAMLSanitizer(cfg.Sanitizer)

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start backup loop in a goroutine
	go func() {
		ticker := time.NewTicker(cfg.BackupInterval)
		defer ticker.Stop()

		// Run initial backup
		if err := runBackup(ctx, kubeCollector, yamlSanitizer, gitManager, cfg); err != nil {
			log.Printf("Initial backup failed: %v", err)
		}

		for {
			select {
			case <-ctx.Done():
				log.Println("Backup loop stopped")
				return
			case <-ticker.C:
				if err := runBackup(ctx, kubeCollector, yamlSanitizer, gitManager, cfg); err != nil {
					log.Printf("Backup failed: %v", err)
				}
			}
		}
	}()

	// Wait for shutdown signal
	<-sigChan
	log.Println("Received shutdown signal, stopping daemon...")
	cancel()

	// Give some time for graceful shutdown
	time.Sleep(5 * time.Second)
	log.Println("Kube Git Backup daemon stopped")
}

func runBackup(ctx context.Context, collector *collector.KubernetesCollector, 
	sanitizer *sanitizer.YAMLSanitizer, gitManager *git.Manager, cfg *config.Config) error {
	
	log.Println("Starting backup process...")
	
	// Collect resources from Kubernetes
	resources, err := collector.CollectResources(ctx)
	if err != nil {
		return fmt.Errorf("failed to collect resources: %w", err)
	}

	log.Printf("Collected %d resources", len(resources))

	// Sanitize YAML content
	sanitizedResources, err := sanitizer.SanitizeResources(resources)
	if err != nil {
		return fmt.Errorf("failed to sanitize resources: %w", err)
	}

	if cfg.DumpOnly {
		// Dump only mode - save to local directory
		if err := dumpResourcesLocally(sanitizedResources, cfg.WorkDir); err != nil {
			return fmt.Errorf("failed to dump resources locally: %w", err)
		}
		log.Printf("Resources dumped to local directory: %s", cfg.WorkDir)
	} else {
		// Normal mode - backup to Git repository
		if err := gitManager.BackupResources(ctx, sanitizedResources); err != nil {
			return fmt.Errorf("failed to backup resources to Git: %w", err)
		}
		log.Println("Resources backed up to Git repository")
	}

	log.Println("Backup process completed successfully")
	return nil
}

// dumpResourcesLocally saves sanitized resources to local directory structure
func dumpResourcesLocally(resources []sanitizer.SanitizedResource, workDir string) error {
	// Create directory structure: namespace/kind/name.yaml
	for _, resource := range resources {
		var resourcePath string
		
		if resource.Namespace == "" {
			// Cluster-scoped resource
			resourcePath = filepath.Join(workDir, "cluster-scoped", 
				strings.ToLower(resource.Kind), fmt.Sprintf("%s.yaml", resource.Name))
		} else {
			// Namespaced resource
			resourcePath = filepath.Join(workDir, "namespaces", resource.Namespace,
				strings.ToLower(resource.Kind), fmt.Sprintf("%s.yaml", resource.Name))
		}

		// Create directory if it doesn't exist
		dir := filepath.Dir(resourcePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}

		// Write YAML content
		if err := os.WriteFile(resourcePath, resource.YAML, 0644); err != nil {
			return fmt.Errorf("failed to write file %s: %w", resourcePath, err)
		}
	}

	return nil
}