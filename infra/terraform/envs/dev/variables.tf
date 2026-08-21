variable "aws_region" {
  type        = string
  description = "AWS region for the dev environment."
  default     = "ap-south-1"
}

variable "environment" {
  type        = string
  description = "Deployment environment name."
  default     = "dev"
}

variable "project" {
  type        = string
  description = "Project name used in resource tags."
  default     = "footgrid"
}
