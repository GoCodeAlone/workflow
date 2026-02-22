variable "project_name" {
  description = "Project name used in resource naming"
  type        = string
  default     = "workflow"
}

variable "environment" {
  description = "Environment name (dev, staging, production)"
  type        = string
}

variable "instance_class" {
  description = "RDS instance class"
  type        = string
  default     = "db.t3.micro"
}

variable "allocated_storage" {
  description = "Initial allocated storage in GB"
  type        = number
  default     = 20
}

variable "max_allocated_storage" {
  description = "Maximum storage for autoscaling in GB"
  type        = number
  default     = 100
}

variable "db_name" {
  description = "Name of the database to create"
  type        = string
  default     = "workflow"
}

variable "username" {
  description = "Master username for the database"
  type        = string
  default     = "workflow"
}

variable "password" {
  description = "Master password for the database"
  type        = string
  sensitive   = true
}

variable "subnet_ids" {
  description = "Subnet IDs for the DB subnet group (private subnets)"
  type        = list(string)
}

variable "vpc_id" {
  description = "VPC ID"
  type        = string
}

variable "allowed_security_group_ids" {
  description = "Security group IDs allowed to connect to RDS (ECS SG)"
  type        = list(string)
  default     = []
}

variable "multi_az" {
  description = "Enable Multi-AZ deployment"
  type        = bool
  default     = false
}

variable "backup_retention_days" {
  description = "Number of days to retain automated backups"
  type        = number
  default     = 7
}

variable "enable_performance_insights" {
  description = "Enable Performance Insights"
  type        = bool
  default     = false
}

variable "deletion_protection" {
  description = "Enable deletion protection"
  type        = bool
  default     = false
}
