# Kube Git Backup

A Go-based Kubernetes daemon that continuously runs inside Kubernetes clusters to collect all resources in YAML format, sanitize them, and push to a Git repository for backup and versioning.

## Quick Start

### 1. Prerequisites

- Kubernetes cluster with RBAC enabled
- Git repository (GitHub, GitLab, Bitbucket, etc.)
- SSH key or access token for Git authentication

### 2. Configure Git Authentication

#### For SSH Authentication:
```bash
# Create SSH key secret
kubectl create secret generic git-ssh-key \
  --from-file=id_rsa=/path/to/your/private/key \
  -n kube-system
```

**Note**: SSH host key verification is handled automatically. The application will:
1. Look for existing `known_hosts` files (`/root/.ssh/known_hosts`, `/etc/ssh/ssh_known_hosts`)
2. Use `SSH_KNOWN_HOSTS` environment variable if set
3. Create a default `known_hosts` file with common Git service providers (GitHub, GitLab, Bitbucket)

#### For Token Authentication:
```bash
# Create token secret
kubectl create secret generic git-token \
  --from-literal=token=your-github-token \
  -n kube-system
```

### 3. Deploy to Kubernetes

```bash
# Apply RBAC configuration
kubectl apply -f k8s/rbac.yaml

# Update deployment configuration
# Edit k8s/deployment.yaml and update:
# - GIT_REPOSITORY: Your Git repository URL
# - Other environment variables as needed

# Deploy the application
kubectl apply -f k8s/deployment.yaml
```

## Configuration

All configuration is done via environment variables:

### Git Configuration
| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `GIT_REPOSITORY` | Git repository URL | - | ✅ |
| `GIT_BRANCH` | Git branch to use | `main` | ❌ |
| `GIT_AUTHOR_NAME` | Git commit author name | `Kube Git Backup` | ❌ |
| `GIT_AUTHOR_EMAIL` | Git commit author email | `kube-backup@example.com` | ❌ |
| `GIT_SSH_KEY_PATH` | Path to SSH private key | `/root/.ssh/id_rsa` | ❌ |
| `GIT_TOKEN` | Git access token | - | ❌ |

**Note**: Authentication method is automatically detected based on the repository URL:
- HTTPS URLs (`https://github.com/...`) → Token authentication
- SSH URLs (`git@github.com:...`) → SSH key authentication

### Backup Configuration
| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `BACKUP_INTERVAL` | Backup interval (Go duration) | `1h` | ❌ |
| `WORK_DIR` | Working directory for Git operations | `/tmp/kube-backup` | ❌ |

### Resource Configuration
| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `INCLUDE_RESOURCES` | Comma-separated list of resource types to include | All supported types | ❌ |
| `EXCLUDE_RESOURCES` | Comma-separated list of resource types to exclude | `pods,events,endpoints,replicasets` | ❌ |
| `INCLUDE_NAMESPACES` | Comma-separated list of namespaces to include (empty = all) | - | ❌ |
| `EXCLUDE_NAMESPACES` | Comma-separated list of namespaces to exclude | `kube-system,default,kube-node-lease` | ❌ |

### YAML Sanitization
| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `STRIP_FIELDS` | Comma-separated list of field paths to remove | See sanitizer defaults | ❌ |

## Repository Structure

The backup creates an organized directory structure in your Git repository:

```
├── cluster-scoped/
│   ├── clusterrole/
│   │   ├── admin.yaml
│   │   └── view.yaml
│   ├── clusterrolebinding/
│   ├── persistentvolume/
│   └── storageclass/
└── namespaces/
    ├── default/
    │   ├── deployment/
    │   │   └── my-app.yaml
    │   ├── service/
    │   │   └── my-app.yaml
    │   └── configmap/
    └── kube-system/
        ├── daemonset/
        └── service/
```

## Advanced Configuration

### Custom Field Stripping

You can customize which fields are stripped from the YAML using the `STRIP_FIELDS` environment variable:

```bash
STRIP_FIELDS="metadata.uid,metadata.resourceVersion,status,spec.clusterIP,metadata.annotations[custom.io/annotation]"
```

Supported field path formats:
- Simple paths: `metadata.uid`
- Nested paths: `spec.template.metadata.labels`
- Array fields: `spec.ports[].nodePort`
- Annotation keys: `metadata.annotations[key]`