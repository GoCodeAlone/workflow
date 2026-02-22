variable "project_name" {
  description = "Project name used in resource naming"
  type        = string
  default     = "workflow"
}

variable "environment" {
  description = "Environment name (dev, staging, production)"
  type        = string
}

variable "vpc_id" {
  description = "VPC ID"
  type        = string
}

variable "subnet_ids" {
  description = "Public subnet IDs for the ALB"
  type        = list(string)
}

variable "certificate_arn" {
  description = "ARN of the ACM certificate for HTTPS"
  type        = string
}

variable "health_check_path" {
  description = "Path for ALB health checks"
  type        = string
  default     = "/healthz"
}

variable "access_logs_bucket" {
  description = "S3 bucket name for ALB access logs (empty string to disable)"
  type        = string
  default     = ""
}
