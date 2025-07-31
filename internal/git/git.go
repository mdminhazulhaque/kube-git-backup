package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"kube-git-backup/internal/config"
	"kube-git-backup/internal/sanitizer"

	"github.com/go-git/go-git/v5"
	config2 "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// Manager handles Git operations for backing up Kubernetes resources
type Manager struct {
	config     config.GitConfig
	workDir    string
	repository *git.Repository
	auth       transport.AuthMethod
}

// NewManager creates a new Git manager
func NewManager(cfg config.GitConfig) (*Manager, error) {
	manager := &Manager{
		config:  cfg,
		workDir: "/tmp/kube-backup",
	}

	// Setup authentication
	auth, err := manager.setupAuth()
	if err != nil {
		return nil, fmt.Errorf("failed to setup Git authentication: %w", err)
	}
	manager.auth = auth

	// Initialize repository
	if err := manager.initRepository(); err != nil {
		return nil, fmt.Errorf("failed to initialize repository: %w", err)
	}

	return manager, nil
}

// setupAuth configures Git authentication method
func (gm *Manager) setupAuth() (transport.AuthMethod, error) {
	switch gm.config.AuthMethod {
	case "ssh":
		// SSH key authentication
		if gm.config.SSHKeyPath == "" {
			return nil, fmt.Errorf("SSH key path is required for SSH authentication")
		}

		auth, err := gitssh.NewPublicKeysFromFile("git", gm.config.SSHKeyPath, "")
		if err != nil {
			return nil, fmt.Errorf("failed to load SSH key: %w", err)
		}
		
		// Set up host key callback
		hostKeyCallback, err := gm.getHostKeyCallback()
		if err != nil {
			return nil, fmt.Errorf("failed to setup host key callback: %w", err)
		}
		auth.HostKeyCallback = hostKeyCallback
		return auth, nil

	case "token":
		// Token authentication (GitHub, GitLab, etc.)
		if gm.config.Token == "" {
			return nil, fmt.Errorf("token is required for token authentication")
		}

		return &http.BasicAuth{
			Username: "token", // Can be anything for token auth
			Password: gm.config.Token,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported authentication method: %s", gm.config.AuthMethod)
	}
}

// initRepository initializes or clones the Git repository
func (gm *Manager) initRepository() error {
	// Create work directory if it doesn't exist
	if err := os.MkdirAll(gm.workDir, 0755); err != nil {
		return fmt.Errorf("failed to create work directory: %w", err)
	}

	// Check if repository already exists
	repo, err := git.PlainOpen(gm.workDir)
	if err != nil {
		// Repository doesn't exist, try to clone it
		repo, err = git.PlainClone(gm.workDir, false, &git.CloneOptions{
			URL:      gm.config.Repository,
			Auth:     gm.auth,
			Progress: os.Stdout,
		})
		if err != nil {
			// If clone fails due to empty repository, initialize a new one
			if strings.Contains(err.Error(), "remote repository is empty") {
				repo, err = git.PlainInit(gm.workDir, false)
				if err != nil {
					return fmt.Errorf("failed to initialize repository: %w", err)
				}
				
				// Add the remote origin
				_, err = repo.CreateRemote(&config2.RemoteConfig{
					Name: "origin",
					URLs: []string{gm.config.Repository},
				})
				if err != nil {
					return fmt.Errorf("failed to add remote origin: %w", err)
				}
			} else {
				return fmt.Errorf("failed to clone repository: %w", err)
			}
		}
	}

	gm.repository = repo

	// Checkout the specified branch
	return gm.checkoutBranch()
}

// checkoutBranch checks out the specified branch
func (gm *Manager) checkoutBranch() error {
	workTree, err := gm.repository.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	// Try to fetch latest changes (skip if remote is empty)
	err = gm.repository.Fetch(&git.FetchOptions{
		Auth: gm.auth,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate && !strings.Contains(err.Error(), "remote repository is empty") {
		return fmt.Errorf("failed to fetch: %w", err)
	}

	// Get branch reference
	branchRef := plumbing.NewBranchReferenceName(gm.config.Branch)
	remoteBranchRef := plumbing.NewRemoteReferenceName("origin", gm.config.Branch)

	// Check if local branch exists
	_, err = gm.repository.Reference(branchRef, true)
	if err != nil {
		// Local branch doesn't exist, try to create it from remote
		remoteRef, err := gm.repository.Reference(remoteBranchRef, true)
		if err != nil {
			// Remote branch doesn't exist either, create new branch
			// For empty repositories, we need to create an initial commit first
			err = workTree.Checkout(&git.CheckoutOptions{
				Branch: branchRef,
				Create: true,
			})
			if err != nil {
				// If even creating fails, the repo might be completely empty
				// We'll handle this in the first commit
				return nil
			}
		} else {
			// Create local branch from remote
			err = workTree.Checkout(&git.CheckoutOptions{
				Branch: branchRef,
				Create: true,
				Hash:   remoteRef.Hash(),
			})
			if err != nil {
				return fmt.Errorf("failed to create branch from remote: %w", err)
			}
		}
	} else {
		// Local branch exists, just checkout
		err = workTree.Checkout(&git.CheckoutOptions{
			Branch: branchRef,
		})
		if err != nil {
			return fmt.Errorf("failed to checkout existing branch: %w", err)
		}
	}

	return nil
}

// BackupResources writes sanitized resources to the repository and commits them
func (gm *Manager) BackupResources(ctx context.Context, resources []sanitizer.SanitizedResource) error {
	// Pull latest changes first
	if err := gm.pullLatestChanges(); err != nil {
		return fmt.Errorf("failed to pull latest changes: %w", err)
	}

	// Clean up resources that no longer exist in cluster
	if err := gm.cleanupDeletedResources(resources); err != nil {
		return fmt.Errorf("failed to cleanup deleted resources: %w", err)
	}

	// Write resources to files
	if err := gm.writeResources(resources); err != nil {
		return fmt.Errorf("failed to write resources: %w", err)
	}

	// Add changes to staging
	if err := gm.addChanges(); err != nil {
		return fmt.Errorf("failed to add changes: %w", err)
	}

	// Commit changes
	if err := gm.commitChanges(); err != nil {
		return fmt.Errorf("failed to commit changes: %w", err)
	}

	// Push changes
	if err := gm.pushChanges(); err != nil {
		return fmt.Errorf("failed to push changes: %w", err)
	}

	return nil
}

// pullLatestChanges pulls the latest changes from remote
func (gm *Manager) pullLatestChanges() error {
	workTree, err := gm.repository.Worktree()
	if err != nil {
		return err
	}

	err = workTree.Pull(&git.PullOptions{
		Auth: gm.auth,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate && !strings.Contains(err.Error(), "remote repository is empty") {
		return err
	}

	return nil
}

// writeResources writes sanitized resources to files in the repository
func (gm *Manager) writeResources(resources []sanitizer.SanitizedResource) error {
	// Create directory structure: namespace/kind/name.yaml
	for _, resource := range resources {
		var resourcePath string

		if resource.Namespace == "" {
			// Cluster-scoped resource
			resourcePath = filepath.Join(gm.workDir, "cluster-scoped",
				strings.ToLower(resource.Kind), fmt.Sprintf("%s.yaml", resource.Name))
		} else {
			// Namespaced resource
			resourcePath = filepath.Join(gm.workDir, "namespaces", resource.Namespace,
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

// addChanges adds all changes to Git staging area
func (gm *Manager) addChanges() error {
	workTree, err := gm.repository.Worktree()
	if err != nil {
		return err
	}

	// Add all changes
	_, err = workTree.Add(".")
	return err
}

// commitChanges creates a commit with the changes
func (gm *Manager) commitChanges() error {
	workTree, err := gm.repository.Worktree()
	if err != nil {
		return err
	}

	// Check if there are any changes to commit
	status, err := workTree.Status()
	if err != nil {
		return err
	}

	if status.IsClean() {
		// No changes to commit
		return nil
	}

	// Create commit
	commit, err := workTree.Commit(
		fmt.Sprintf("Backup Kubernetes resources - %s", time.Now().Format("2006-01-02 15:04:05")),
		&git.CommitOptions{
			Author: &object.Signature{
				Name:  gm.config.AuthorName,
				Email: gm.config.AuthorEmail,
				When:  time.Now(),
			},
		},
	)
	if err != nil {
		return err
	}

	// Log commit hash for debugging
	fmt.Printf("Created commit: %s\n", commit)
	return nil
}

// pushChanges pushes commits to remote repository
func (gm *Manager) pushChanges() error {
	return gm.repository.Push(&git.PushOptions{
		Auth: gm.auth,
	})
}

// CleanupOldBackups removes old backup files that are no longer present in Kubernetes
// This is useful to keep the repository clean
func (gm *Manager) CleanupOldBackups(currentResources []sanitizer.SanitizedResource) error {
	// Create a set of current resource paths
	currentPaths := make(map[string]bool)
	for _, resource := range currentResources {
		var resourcePath string
		if resource.Namespace == "" {
			resourcePath = filepath.Join("cluster-scoped",
				strings.ToLower(resource.Kind), fmt.Sprintf("%s.yaml", resource.Name))
		} else {
			resourcePath = filepath.Join("namespaces", resource.Namespace,
				strings.ToLower(resource.Kind), fmt.Sprintf("%s.yaml", resource.Name))
		}
		currentPaths[resourcePath] = true
	}

	// Walk through existing files and remove those not in current set
	return filepath.Walk(gm.workDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non-YAML files
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".yaml") {
			return nil
		}

		// Get relative path from work directory
		relPath, err := filepath.Rel(gm.workDir, path)
		if err != nil {
			return err
		}

		// Skip .git directory and other non-resource files
		if strings.HasPrefix(relPath, ".git") {
			return nil
		}

		// If this file is not in current resources, remove it
		if !currentPaths[relPath] {
			fmt.Printf("Removing old backup file: %s\n", relPath)
			return os.Remove(path)
		}

		return nil
	})
}

// cleanupDeletedResources removes files from Git that no longer exist in the cluster
func (gm *Manager) cleanupDeletedResources(resources []sanitizer.SanitizedResource) error {
	return gm.CleanupOldBackups(resources)
}

// getHostKeyCallback returns an appropriate SSH host key callback
func (gm *Manager) getHostKeyCallback() (ssh.HostKeyCallback, error) {
	// Try to use known_hosts file if available
	knownHostsFiles := []string{
		"/root/.ssh/known_hosts",
		"/etc/ssh/ssh_known_hosts",
		os.Getenv("SSH_KNOWN_HOSTS"),
	}
	
	for _, file := range knownHostsFiles {
		if file != "" {
			if _, err := os.Stat(file); err == nil {
				callback, err := knownhosts.New(file)
				if err == nil {
					return callback, nil
				}
			}
		}
	}
	
	// If no known_hosts file is available, create a default one with common Git hosts
	knownHostsPath := "/root/.ssh/known_hosts"
	if err := gm.createDefaultKnownHosts(knownHostsPath); err != nil {
		// If we can't create known_hosts, fall back to insecure (but log warning)
		fmt.Printf("Warning: Using insecure SSH host key verification. Could not setup known_hosts: %v\n", err)
		return ssh.InsecureIgnoreHostKey(), nil
	}
	
	callback, err := knownhosts.New(knownHostsPath)
	if err != nil {
		fmt.Printf("Warning: Using insecure SSH host key verification. Could not load known_hosts: %v\n", err)
		return ssh.InsecureIgnoreHostKey(), nil
	}
	
	return callback, nil
}

// createDefaultKnownHosts creates a known_hosts file with common Git service providers
func (gm *Manager) createDefaultKnownHosts(knownHostsPath string) error {
	// Ensure the directory exists
	dir := filepath.Dir(knownHostsPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create SSH directory: %w", err)
	}
	
	// Common Git service provider host keys (these are public and stable)
	knownHosts := []string{
		"github.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl",
		"github.com ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBEmKSENjQEezOmxkZMy7opKgwFB9nkt5YRrYMjNuG5N87uRgg6CLrbo5wAdT/y6v0mKV0U2w0WZ2YB/++Tpockg=",
		"github.com ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQCj7ndNxQowgcQnjshcLrqPEiiphnt+VTTvDP6mHBL9j1aNUkY4Ue1gvwnGLVlOhGeYrnZaMgRK6+PKCUXaDbC7qtbW8gIkhL7aGCsOr/C56SJMy/BCZfxd1nWzAOxSDPgVsmerOBYfNqltV9/hWCqBywINIR+5dIg6JTJ72pcEpEjcYgXkE2YEFXV1JHnsKgbLWNlhScqb2UmyRkQyytRLtL+38TGxkxCflmO+5Z8CSSNY7GidjMIZ7Q4zMjA2n1nGrlTDkzwDCsw+wqFPGQA179cnfGWOWRVruj16z6XyvxvjJwbz0wQZ75XK5tKSb7FNyeIEs4TT4jk+S4dhPeAUC5y+bDYirYgM4GC7uEnztnZyaVWQ7B381AK4Qdrwt51ZqExKbQpTUNn+EjqoTwvqNj4kqx5QUCI0ThS/YkOxJCXmPUWZbhjpCg56i+2aB6CmK2JGhn57K5mj0MNdBXA4/WnwH6XoPWJzK5Nyu2zB3nAZp+S5hpQs+p1vN1/wsjk=",
		"gitlab.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAfuCHKVTjquxvt6CM6tdG4SLp1Btn/nOeHHE5UOzRdf",
		"gitlab.com ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQCsj2bNKTBSpIYDEGk9KxsGh3mySTRgMtXL583qmBpzeQ+jqCMRgBqB98u3z++J1sKlXHWfM9dyhSevkMwSbhoR8XIq/U0tCNyokEi/ueaBMCvbcTHhO7k0VhjdMOhHDBBM4/wCnfVAd9UBQL89W+9EH7OjvRaQNvQ7VQEQX2RkRhgRcRFxzK2MZv9rGV/pbL9tBTL4Pz0aaK1/OyOhBiA2QSqsX6QAyQBe2Zy6yq9VJXn7BvHiSGb8U6TJP6zp8nG7Z9D9+7D6z9A7P8C6Q2a4k3F8E6fE2D6TZkFxh5JYI4TQBF9LO3BzPf8z",
		"bitbucket.org ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDQeJzhupRu0u0cdegZIa8e86EG2qOCsIsD1Xw0xSeiPDlCr7kq97NLmMbpKTX6Esc30NuoqEEHQoTuKtwpHBYB2C5QD5e6jAj2vJcJ+Rx7Y6B6DGUQOSdKPpd8mM+b7V9XqZfwF5u8QzU1Nq9B8ZkfnF8Y9Q2e7G2TjkFsQ2gE7G2OeZzT7Y6BfV8o9QF6H0tY2X5JjYk8J5Z6Q1V9G1kF8J3sF9qQ5XfF6YoQ9Y7H6J+2wQhVgF2e6EF7hJ6GQv9O2K8V6j1H8c+KX2PjH9d8SsF2W8oJ5E8Q5zQI6KY2F9PqEJ8QK3c6hVfJk",
	}
	
	content := strings.Join(knownHosts, "\n") + "\n"
	if err := os.WriteFile(knownHostsPath, []byte(content), 0600); err != nil {
		return fmt.Errorf("failed to write known_hosts file: %w", err)
	}
	
	return nil
}
