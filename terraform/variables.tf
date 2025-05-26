variable "github_owner" {
  description = "GitHub repository owner"
  type        = string
}

variable "github_username" {
  description = "GitHub username for container registry authentication"
  type        = string
}

variable "github_token" {
  description = "GitHub token for container registry authentication"
  type        = string
  sensitive   = true
} 