variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "vpc_cidr" {
  description = "CIDR block for the VPC"
  type        = string
  default     = "10.2.0.0/16"
}

variable "certificate_arn" {
  description = "ACM certificate ARN for HTTPS"
  type        = string
}

variable "alb_logs_bucket" {
  description = "S3 bucket name for ALB access logs"
  type        = string
  default     = ""
}

variable "db_username" {
  description = "PostgreSQL master username"
  type        = string
  default     = "workflow"
}

variable "db_password" {
  description = "PostgreSQL master password"
  type        = string
  sensitive   = true
}

variable "image_tag" {
  description = "Docker image tag to deploy"
  type        = string
}

variable "alert_email" {
  description = "Email for CloudWatch alerts"
  type        = string
}

variable "ecs_desired_count" {
  description = "Initial desired number of ECS tasks"
  type        = number
  default     = 3
}

variable "ecs_min_count" {
  description = "Minimum number of ECS tasks for autoscaling"
  type        = number
  default     = 2
}

variable "ecs_max_count" {
  description = "Maximum number of ECS tasks for autoscaling"
  type        = number
  default     = 10
}
