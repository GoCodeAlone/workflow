variable "project_name" {
  description = "Project name used in resource naming"
  type        = string
  default     = "workflow"
}

variable "environment" {
  description = "Environment name (dev, staging, production)"
  type        = string
}

variable "cluster_name" {
  description = "ECS cluster name"
  type        = string
}

variable "service_name" {
  description = "ECS service name"
  type        = string
}

variable "alb_arn_suffix" {
  description = "ALB ARN suffix (used in CloudWatch dimensions)"
  type        = string
}

variable "alert_email" {
  description = "Email address for CloudWatch alarm notifications (empty to skip)"
  type        = string
  default     = ""
}

variable "cpu_alarm_threshold" {
  description = "CPU utilization threshold percentage to trigger alarm"
  type        = number
  default     = 80
}

variable "memory_alarm_threshold" {
  description = "Memory utilization threshold percentage to trigger alarm"
  type        = number
  default     = 85
}

variable "error_rate_threshold" {
  description = "5xx error count threshold to trigger alarm"
  type        = number
  default     = 10
}
