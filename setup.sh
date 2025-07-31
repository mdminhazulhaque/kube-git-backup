#!/bin/bash

# Kube Git Backup Setup Script
# This script helps you set up the kube-git-backup daemon in your Kubernetes cluster

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration variables
NAMESPACE="kube-system"
SERVICE_ACCOUNT="kube-git-backup"
DEPLOYMENT_NAME="kube-git-backup"

print_header() {
    echo -e "${BLUE}"
    echo "=============================================="
    echo "    Kube Git Backup Setup Script"
    echo "=============================================="
    echo -e "${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_info() {
    echo -e "${BLUE}ℹ $1${NC}"
}

check_prerequisites() {
    print_info "Checking prerequisites..."
    
    # Check if kubectl is installed
    if ! command -v kubectl &> /dev/null; then
        print_error "kubectl is not installed or not in PATH"
        exit 1
    fi
    
    # Check if we can connect to Kubernetes cluster
    if ! kubectl cluster-info &> /dev/null; then
        print_error "Cannot connect to Kubernetes cluster. Please check your kubeconfig"
        exit 1
    fi
    
    print_success "Prerequisites check passed"
}

setup_git_auth() {
    print_info "Setting up Git authentication..."
    
    echo "Choose authentication method:"
    echo "1) SSH Key"
    echo "2) Token (GitHub/GitLab)"
    read -p "Enter choice [1-2]: " auth_choice
    
    case $auth_choice in
        1)
            setup_ssh_auth
            ;;
        2)
            setup_token_auth
            ;;
        *)
            print_error "Invalid choice. Please run the script again."
            exit 1
            ;;
    esac
}

setup_ssh_auth() {
    print_info "Setting up SSH authentication..."
    
    read -p "Enter path to SSH private key [~/.ssh/id_rsa]: " ssh_key_path
    ssh_key_path=${ssh_key_path:-~/.ssh/id_rsa}
    
    # Expand tilde to home directory
    ssh_key_path=$(eval echo $ssh_key_path)
    
    if [[ ! -f "$ssh_key_path" ]]; then
        print_error "SSH key file not found: $ssh_key_path"
        exit 1
    fi
    
    # Create SSH key secret
    kubectl create secret generic git-ssh-key \
        --from-file=id_rsa="$ssh_key_path" \
        -n "$NAMESPACE" \
        --dry-run=client -o yaml | kubectl apply -f -
    
    print_success "SSH key secret created"
    
    # Set auth method in deployment
    AUTH_METHOD="ssh"
}

setup_token_auth() {
    print_info "Setting up token authentication..."
    
    read -s -p "Enter your Git access token: " git_token
    echo
    
    if [[ -z "$git_token" ]]; then
        print_error "Token cannot be empty"
        exit 1
    fi
    
    # Create token secret
    kubectl create secret generic git-token \
        --from-literal=token="$git_token" \
        -n "$NAMESPACE" \
        --dry-run=client -o yaml | kubectl apply -f -
    
    print_success "Token secret created"
    
    # Set auth method in deployment
    AUTH_METHOD="token"
}

configure_repository() {
    print_info "Configuring Git repository..."
    
    read -p "Enter Git repository URL: " git_repo
    if [[ -z "$git_repo" ]]; then
        print_error "Repository URL cannot be empty"
        exit 1
    fi
    
    read -p "Enter Git branch [main]: " git_branch
    git_branch=${git_branch:-main}
    
    read -p "Enter Git author name [Kube Git Backup]: " git_author_name
    git_author_name=${git_author_name:-"Kube Git Backup"}
    
    read -p "Enter Git author email [kube-backup@example.com]: " git_author_email
    git_author_email=${git_author_email:-"kube-backup@example.com"}
    
    # Store configuration
    GIT_REPOSITORY="$git_repo"
    GIT_BRANCH="$git_branch"
    GIT_AUTHOR_NAME="$git_author_name"
    GIT_AUTHOR_EMAIL="$git_author_email"
    
    print_success "Repository configuration completed"
}

configure_backup_settings() {
    print_info "Configuring backup settings..."
    
    read -p "Enter backup interval [1h]: " backup_interval
    backup_interval=${backup_interval:-"1h"}
    
    read -p "Enter namespaces to backup (comma-separated, empty for all): " namespaces
    
    read -p "Enter resources to exclude [pods,events,endpoints,replicasets]: " exclude_resources
    exclude_resources=${exclude_resources:-"pods,events,endpoints,replicasets"}
    
    # Store configuration
    BACKUP_INTERVAL="$backup_interval"
    NAMESPACES="$namespaces"
    EXCLUDE_RESOURCES="$exclude_resources"
    
    print_success "Backup settings configured"
}

deploy_rbac() {
    print_info "Deploying RBAC configuration..."
    
    if [[ ! -f "k8s/rbac.yaml" ]]; then
        print_error "RBAC configuration file not found: k8s/rbac.yaml"
        exit 1
    fi
    
    kubectl apply -f k8s/rbac.yaml
    print_success "RBAC configuration deployed"
}

create_deployment_config() {
    print_info "Creating deployment configuration..."
    
    # Create a temporary deployment file with our configuration
    cat > /tmp/kube-git-backup-deployment.yaml << EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kube-git-backup
  namespace: kube-system
  labels:
    app: kube-git-backup
spec:
  replicas: 1
  selector:
    matchLabels:
      app: kube-git-backup
  template:
    metadata:
      labels:
        app: kube-git-backup
    spec:
      serviceAccountName: kube-git-backup
      containers:
      - name: kube-git-backup
        image: kube-git-backup:latest
        imagePullPolicy: IfNotPresent
        env:
        - name: GIT_REPOSITORY
          value: "$GIT_REPOSITORY"
        - name: GIT_BRANCH
          value: "$GIT_BRANCH"
        - name: GIT_AUTHOR_NAME
          value: "$GIT_AUTHOR_NAME"
        - name: GIT_AUTHOR_EMAIL
          value: "$GIT_AUTHOR_EMAIL"
        - name: BACKUP_INTERVAL
          value: "$BACKUP_INTERVAL"
        - name: EXCLUDE_RESOURCES
          value: "$EXCLUDE_RESOURCES"
EOF

    if [[ -n "$NAMESPACES" ]]; then
        cat >> /tmp/kube-git-backup-deployment.yaml << EOF
        - name: NAMESPACES
          value: "$NAMESPACES"
EOF
    fi

    if [[ "$AUTH_METHOD" == "ssh" ]]; then
        cat >> /tmp/kube-git-backup-deployment.yaml << EOF
        - name: GIT_SSH_KEY_PATH
          value: "/etc/ssh-key/id_rsa"
        volumeMounts:
        - name: ssh-key
          mountPath: /etc/ssh-key
          readOnly: true
        - name: tmp-volume
          mountPath: /tmp
        resources:
          requests:
            memory: "128Mi"
            cpu: "100m"
          limits:
            memory: "512Mi"
            cpu: "500m"
      volumes:
      - name: ssh-key
        secret:
          secretName: git-ssh-key
          defaultMode: 0600
      - name: tmp-volume
        emptyDir: {}
EOF
    else
        cat >> /tmp/kube-git-backup-deployment.yaml << EOF
        - name: GIT_TOKEN
          valueFrom:
            secretKeyRef:
              name: git-token
              key: token
        volumeMounts:
        - name: tmp-volume
          mountPath: /tmp
        resources:
          requests:
            memory: "128Mi"
            cpu: "100m"
          limits:
            memory: "512Mi"
            cpu: "500m"
      volumes:
      - name: tmp-volume
        emptyDir: {}
EOF
    fi

    cat >> /tmp/kube-git-backup-deployment.yaml << EOF
      securityContext:
        runAsNonRoot: true
        runAsUser: 1000
        fsGroup: 1000
EOF

    print_success "Deployment configuration created"
}

deploy_application() {
    print_info "Deploying kube-git-backup application..."
    
    kubectl apply -f /tmp/kube-git-backup-deployment.yaml
    
    print_success "Application deployed"
    
    # Clean up temporary file
    rm -f /tmp/kube-git-backup-deployment.yaml
}

wait_for_deployment() {
    print_info "Waiting for deployment to be ready..."
    
    kubectl wait --for=condition=available --timeout=300s deployment/$DEPLOYMENT_NAME -n $NAMESPACE
    
    print_success "Deployment is ready"
}

show_status() {
    print_info "Deployment status:"
    kubectl get pods -n $NAMESPACE -l app=kube-git-backup
    
    print_info "To view logs, run:"
    echo "kubectl logs -n $NAMESPACE -l app=kube-git-backup -f"
}

main() {
    print_header
    
    check_prerequisites
    setup_git_auth
    configure_repository
    configure_backup_settings
    deploy_rbac
    create_deployment_config
    deploy_application
    wait_for_deployment
    show_status
    
    print_success "Kube Git Backup setup completed successfully!"
}

# Handle script interruption
trap 'print_error "Setup interrupted"; exit 1' INT TERM

# Run main function
main "$@"
