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

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| **Git Configuration** | | | |
| `GIT_REPOSITORY` | Git repository URL | - | ✅ |
| `GIT_BRANCH` | Git branch to use | `main` | ❌ |
| `GIT_AUTHOR_NAME` | Git commit author name | `Kube Git Backup` | ❌ |
| `GIT_AUTHOR_EMAIL` | Git commit author email | `kube-backup@example.com` | ❌ |
| `GIT_SSH_KEY_PATH` | Path to SSH private key | `/root/.ssh/id_rsa` | ❌ |
| `GIT_TOKEN` | Git access token | - | ❌ |
| **Backup Settings** | | | |
| `BACKUP_INTERVAL` | Backup interval (Go duration) | `1h` | ❌ |
| `WORK_DIR` | Working directory for Git operations | `/tmp/kube-backup` | ❌ |
| **Resource Filtering** | | | |
| `INCLUDE_RESOURCES` | Resource types to include (comma-separated) | All supported types | ❌ |
| `EXCLUDE_RESOURCES` | Resource types to exclude (comma-separated) | `pods,events,endpoints,replicasets` | ❌ |
| `INCLUDE_NAMESPACES` | Namespaces to include (comma-separated, empty = all) | - | ❌ |
| `EXCLUDE_NAMESPACES` | Namespaces to exclude (comma-separated) | `kube-system,default,kube-node-lease` | ❌ |
| **YAML Processing** | | | |
| `STRIP_FIELDS` | Field paths to remove (comma-separated) | See sanitizer defaults | ❌ |

**Authentication**: Automatically detected based on repository URL (HTTPS → token, SSH → key)

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