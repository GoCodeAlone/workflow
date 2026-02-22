terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

resource "aws_elasticache_subnet_group" "main" {
  name       = "${var.project_name}-${var.environment}-redis-subnet-group"
  subnet_ids = var.subnet_ids

  tags = {
    Name        = "${var.project_name}-${var.environment}-redis-subnet-group"
    Environment = var.environment
    ManagedBy   = "opentofu"
  }
}

resource "aws_security_group" "redis" {
  name        = "${var.project_name}-${var.environment}-redis-sg"
  description = "Security group for ElastiCache Redis"
  vpc_id      = var.vpc_id

  ingress {
    from_port       = 6379
    to_port         = 6379
    protocol        = "tcp"
    security_groups = var.allowed_security_group_ids
    description     = "Redis from ECS"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Allow all outbound"
  }

  tags = {
    Name        = "${var.project_name}-${var.environment}-redis-sg"
    Environment = var.environment
    ManagedBy   = "opentofu"
  }
}

resource "aws_elasticache_replication_group" "main" {
  replication_group_id = "${var.project_name}-${var.environment}-redis"
  description          = "Redis cluster for ${var.project_name} ${var.environment}"

  node_type            = var.node_type
  num_cache_clusters   = var.num_cache_nodes
  engine_version       = var.engine_version
  port                 = 6379
  parameter_group_name = "default.redis7"

  subnet_group_name  = aws_elasticache_subnet_group.main.name
  security_group_ids = [aws_security_group.redis.id]

  automatic_failover_enabled = var.num_cache_nodes > 1
  multi_az_enabled           = var.num_cache_nodes > 1

  at_rest_encryption_enabled = true
  transit_encryption_enabled = true

  snapshot_retention_limit = var.snapshot_retention_days
  snapshot_window          = "02:00-03:00"
  maintenance_window       = "sun:03:00-sun:04:00"

  apply_immediately = var.environment == "dev" ? true : false

  tags = {
    Name        = "${var.project_name}-${var.environment}-redis"
    Environment = var.environment
    ManagedBy   = "opentofu"
  }
}
