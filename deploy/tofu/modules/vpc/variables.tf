variable "vpc_cidr" {
  description = "CIDR block for the VPC"
  type        = string
  default     = "10.0.0.0/16"
}

variable "environment" {
  description = "Environment name (dev, staging, production)"
  type        = string
}

variable "project_name" {
  description = "Project name used in resource naming"
  type        = string
  default     = "workflow"
}

variable "enable_nat_gateway" {
  description = "Whether to create a NAT gateway for private subnets"
  type        = bool
  default     = true
}
