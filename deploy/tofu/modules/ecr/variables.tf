variable "repository_name" {
  description = "Name of the ECR repository"
  type        = string
}

variable "max_image_count" {
  description = "Maximum number of images to keep in the repository"
  type        = number
  default     = 20
}

variable "allowed_account_ids" {
  description = "AWS account IDs allowed to pull images (for cross-account access)"
  type        = list(string)
  default     = []
}
