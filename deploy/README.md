# Workflow Engine — Deployment Guide

This directory contains deployment configurations for the Workflow engine.

## Directory Structure

```
deploy/
├── tofu/                    # OpenTofu (Terraform-compatible) IaC
│   ├── modules/
│   │   ├── alb/             # Application Load Balancer
│   │   ├── ecr/             # Elastic Container Registry
│   │   ├── ecs/             # ECS Fargate cluster + service
│   │   ├── elasticache/     # Redis via ElastiCache
│   │   ├── monitoring/      # CloudWatch dashboards + alarms
│   │   ├── rds/             # PostgreSQL via RDS
│   │   └── vpc/             # VPC, subnets, NAT gateway
│   └── environments/
│       ├── dev/             # Development — small instances, single-AZ
│       ├── staging/         # Staging — multi-AZ, medium instances
│       └── production/      # Production — multi-AZ, large instances, autoscaling
├── docker-compose/          # Local development stack
│   ├── docker-compose.yml
│   ├── prometheus.yml
│   └── grafana/
│       └── provisioning/
├── helm/                    # Kubernetes Helm chart
│   └── workflow/
├── grafana/                 # Grafana dashboard JSON files
└── prometheus/              # Prometheus alert rules
```

## Architecture Overview

### AWS (OpenTofu)

```
Internet → ALB (public subnets) → ECS Fargate (private subnets)
                                       ↓
                               RDS PostgreSQL (private subnets)
                               ElastiCache Redis (private subnets)
```

All compute and data services run in private subnets. Only the ALB is public-facing. Traffic flows:

1. HTTPS on port 443 terminates at the ALB with an ACM certificate
2. HTTP on port 80 is redirected to HTTPS
3. ALB forwards to ECS tasks on port 8080
4. ECS tasks connect to RDS (port 5432) and Redis (port 6379) via security groups

### Kubernetes (Helm)

The Helm chart deploys the workflow server as a `Deployment` with:
- `HorizontalPodAutoscaler` for CPU/memory-based scaling
- `PodDisruptionBudget` for safe maintenance
- `ServiceMonitor` for Prometheus Operator metrics scraping
- `Ingress` for external access

## Prerequisites

### OpenTofu

- [OpenTofu](https://opentofu.org/docs/intro/install/) >= 1.6.0
- AWS CLI configured with appropriate permissions
- An ACM certificate for HTTPS (must be in the same region)

### Helm

- [Helm](https://helm.sh/docs/intro/install/) >= 3.0
- A running Kubernetes cluster
- kubectl configured

### Docker Compose

- Docker >= 24.0 with Compose v2

## Deploying with OpenTofu

### First-time setup

```bash
cd deploy/tofu/environments/dev

# Copy and edit the example vars file
cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars with your values

# Initialize
tofu init

# Preview changes
tofu plan

# Apply
tofu apply
```

### Deploying a new image version

```bash
cd deploy/tofu/environments/<env>
tofu apply -var="image_tag=v0.5.1"
```

### Environment differences

| Setting              | dev           | staging        | production      |
|----------------------|---------------|----------------|-----------------|
| ECS CPU              | 256           | 512            | 2048            |
| ECS Memory           | 512 MB        | 1024 MB        | 4096 MB         |
| ECS Desired Count    | 1             | 2              | 3 (autoscales)  |
| RDS Instance         | db.t3.micro   | db.t3.small    | db.r7g.large    |
| RDS Multi-AZ         | No            | Yes            | Yes             |
| Redis Nodes          | 1             | 1              | 2 (with failover)|
| Redis Instance       | cache.t3.micro| cache.t3.small | cache.r7g.large |
| Log Retention        | 30 days       | 30 days        | 90 days         |
| Deletion Protection  | No            | Yes            | Yes             |

### Remote state (recommended for teams)

The production environment uses an S3 backend. Create the backend resources once:

```bash
# Create the S3 bucket and DynamoDB table for state
aws s3api create-bucket --bucket workflow-tofu-state --region us-east-1
aws s3api put-bucket-versioning --bucket workflow-tofu-state \
  --versioning-configuration Status=Enabled
aws dynamodb create-table \
  --table-name workflow-tofu-locks \
  --attribute-definitions AttributeName=LockID,AttributeType=S \
  --key-schema AttributeName=LockID,KeyType=HASH \
  --billing-mode PAY_PER_REQUEST
```

## Deploying with Helm

```bash
# Add the chart (if published to a registry)
# helm repo add workflow https://charts.example.com/workflow

# Or install from local path:
helm install workflow ./deploy/helm/workflow \
  --namespace workflow \
  --create-namespace \
  --set image.tag=v0.5.0 \
  --set ingress.enabled=true \
  --set ingress.hosts[0].host=workflow.example.com \
  --set autoscaling.enabled=true

# Upgrade
helm upgrade workflow ./deploy/helm/workflow \
  --set image.tag=v0.5.1

# With a values file (recommended)
helm upgrade --install workflow ./deploy/helm/workflow \
  -f my-values.yaml
```

### Key Helm values

| Value | Default | Description |
|-------|---------|-------------|
| `image.tag` | Chart appVersion | Docker image tag |
| `replicaCount` | 1 | Number of replicas (when autoscaling disabled) |
| `autoscaling.enabled` | false | Enable HPA |
| `autoscaling.minReplicas` | 1 | Min pods |
| `autoscaling.maxReplicas` | 10 | Max pods |
| `podDisruptionBudget.enabled` | false | Enable PDB |
| `podDisruptionBudget.minAvailable` | 1 | Min available pods |
| `ingress.enabled` | false | Enable Ingress |
| `ingress.className` | "" | Ingress class (e.g., nginx, alb) |
| `monitoring.serviceMonitor.enabled` | false | Enable Prometheus ServiceMonitor |
| `envFromSecret` | "" | Name of K8s Secret for env vars |

### Production Helm values example

```yaml
replicaCount: 3

autoscaling:
  enabled: true
  minReplicas: 2
  maxReplicas: 20
  targetCPUUtilizationPercentage: 70

podDisruptionBudget:
  enabled: true
  minAvailable: 1

ingress:
  enabled: true
  className: alb
  annotations:
    kubernetes.io/ingress.class: alb
    alb.ingress.kubernetes.io/scheme: internet-facing
    alb.ingress.kubernetes.io/target-type: ip
  hosts:
    - host: workflow.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: workflow-tls
      hosts:
        - workflow.example.com

resources:
  requests:
    cpu: 500m
    memory: 512Mi
  limits:
    cpu: 2000m
    memory: 2Gi

monitoring:
  enabled: true
  serviceMonitor:
    enabled: true
    labels:
      prometheus: kube-prometheus

envFromSecret: workflow-secrets
```

## Local Development with Docker Compose

```bash
cd deploy/docker-compose

# Start all services (workflow-server + postgres + redis + adminer + prometheus + grafana)
docker compose up -d

# Follow logs
docker compose logs -f workflow-server

# Access:
#   Workflow API:  http://localhost:8080
#   Admin UI:      http://localhost:8081
#   Adminer:       http://localhost:8888  (server: postgres, user/pass: workflow)
#   Prometheus:    http://localhost:9090
#   Grafana:       http://localhost:3000  (admin/admin)

# Stop
docker compose down

# Stop and remove data volumes
docker compose down -v
```

### Running just the server dependencies (external server)

```bash
# Start only postgres and redis
docker compose up -d postgres redis

# Then run the server locally
cd ../..
go run ./cmd/server -config example/order-processing-pipeline.yaml
```

## Configuration Options

The workflow server is configured via a YAML file passed with `-config`. See `example/` for sample configs.

Key environment variables:

| Variable | Description |
|----------|-------------|
| `WORKFLOW_ADDR` | HTTP listen address (default `:8080`) |
| `WORKFLOW_DB_HOST` | PostgreSQL host:port |
| `WORKFLOW_DB_NAME` | Database name |
| `WORKFLOW_DB_USER` | Database user |
| `WORKFLOW_DB_PASSWORD` | Database password |
| `WORKFLOW_REDIS_ADDR` | Redis address (host:port) |
| `JWT_SECRET` | Secret for JWT token signing |

## Monitoring

CloudWatch alarms are configured for:
- ECS CPU > threshold (default 80%)
- ECS Memory > threshold (default 85%)
- ALB 5xx error count > threshold (default 10/min)
- ALB unhealthy host count > 0

Alerts are sent to an SNS topic with email subscription.

Grafana dashboards in `deploy/grafana/` cover:
- Workflow overview (request rates, latency, errors)
- Dynamic components (hot-reload activity)
- Chat platform metrics
