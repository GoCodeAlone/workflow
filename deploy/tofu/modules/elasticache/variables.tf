variable "project_name" {
  description = "Project name used in resource naming"
  type        = string
  default     = "workflow"
}

variable "environment" {
  description = "Environment name (dev, staging, production)"
  type        = string
}

variable "node_type" {
  description = "ElastiCache node type"
  type        = string
  default     = "cache.t3.micro"
}

variable "num_cache_nodes" {
  description = "Number of cache nodes (1 for non-prod, 2+ for prod with failover)"
  type        = number
  default     = 1
}

variable "engine_version" {
  description = "Redis engine version"
  type        = string
  default     = "7.1"
}

variable "subnet_ids" {
  description = "Subnet IDs for the ElastiCache subnet group (private subnets)"
  type        = list(string)
}

variable "vpc_id" {
  description = "VPC ID"
  type        = string
}

variable "allowed_security_group_ids" {
  description = "Security group IDs allowed to connect to Redis"
  type        = list(string)
  default     = []
}

variable "snapshot_retention_days" {
  description = "Number of days to retain Redis snapshots"
  type        = number
  default     = 1
}
