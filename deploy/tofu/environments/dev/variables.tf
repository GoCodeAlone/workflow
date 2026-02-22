variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "vpc_cidr" {
  description = "CIDR block for the VPC"
  type        = string
  default     = "10.0.0.0/16"
}

variable "enable_nat_gateway" {
  description = "Enable NAT gateway (set false to reduce cost during testing)"
  type        = bool
  default     = true
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
  description = "Email for CloudWatch alerts (empty to skip)"
  type        = string
  default     = ""
}
