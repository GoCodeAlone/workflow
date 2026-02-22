variable "cluster_name" {
  description = "Name of the ECS cluster"
  type        = string
}

variable "environment" {
  description = "Environment name (dev, staging, production)"
  type        = string
}

variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "image" {
  description = "Docker image for the workflow server (repository:tag)"
  type        = string
}

variable "cpu" {
  description = "CPU units for the ECS task (256, 512, 1024, 2048, 4096)"
  type        = number
  default     = 512
}

variable "memory" {
  description = "Memory in MiB for the ECS task"
  type        = number
  default     = 1024
}

variable "desired_count" {
  description = "Desired number of ECS task instances"
  type        = number
  default     = 1
}

variable "subnet_ids" {
  description = "Subnet IDs for the ECS service (private subnets)"
  type        = list(string)
}

variable "security_group_ids" {
  description = "Security group IDs for the ECS service"
  type        = list(string)
}

variable "target_group_arn" {
  description = "ARN of the ALB target group"
  type        = string
}

variable "log_retention_days" {
  description = "CloudWatch log retention in days"
  type        = number
  default     = 30
}

variable "environment_vars" {
  description = "Environment variables to pass to the container"
  type        = map(string)
  default     = {}
}

variable "secrets" {
  description = "Secrets from SSM/Secrets Manager (name -> ARN)"
  type        = map(string)
  default     = {}
}
