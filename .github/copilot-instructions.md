<!-- Use this file to provide workspace-specific custom instructions to Copilot. For more details, visit https://code.visualstudio.com/docs/copilot/copilot-customization#_use-a-githubcopilotinstructionsmd-file -->

# Kube Git Backup Project Instructions

This is a Go-based Kubernetes daemon that continuously runs inside Kubernetes clusters to collect all resources in YAML format, sanitize them, and push to a Git repository.

## Project Structure
- `cmd/` - Main application entry points
- `internal/` - Internal packages (collector, git, config, etc.)
- `k8s/` - Kubernetes manifests (RBAC, Deployment, etc.)

## Key Components
1. **Resource Collector**: Fetches Kubernetes resources using client-go
2. **YAML Sanitizer**: Strips unnecessary fields from YAML manifests  
3. **Git Manager**: Handles Git operations with SSH/token authentication
4. **Configuration**: Environment-based configuration management

## Development Guidelines
- Use structured logging with levels
- Implement proper error handling and retries
- Follow Go best practices and naming conventions
- Include comprehensive tests
- Use interfaces for better testability
- Implement graceful shutdown handling
