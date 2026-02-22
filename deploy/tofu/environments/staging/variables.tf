variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "vpc_cidr" {
  description = "CIDR block for the VPC"
  type        = string
  default     = "10.1.0.0/16"
}

variable "certificate_arn" {
  description = "ACM certificate ARN for HTTPS"
  type        = string
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
  default     = "latest"
}

variable "alert_email" {
  description = "Email for CloudWatch alerts"
  type        = string
  default     = ""
}
