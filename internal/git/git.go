package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kube-git-backup/internal/config"
	"kube-git-backup/internal/sanitizer"

	"github.com/go-git/go-git/v5"
	config2 "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
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

		auth, err := ssh.NewPublicKeysFromFile("git", gm.config.SSHKeyPath, "")
		if err != nil {
			return nil, fmt.Errorf("failed to load SSH key: %w", err)
		}
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
