variable "region" {
  description = "AWS region for the stack (CloudFront cert is always us-east-1)."
  type        = string
  default     = "us-east-1"
}

variable "project" {
  description = "Name prefix for all resources."
  type        = string
  default     = "serverless-notify"
}

variable "domain_name" {
  description = "Public hostname the Gotify clients point at."
  type        = string
  default     = "notify.example.com"
}

variable "hosted_zone_name" {
  description = "Existing Route53 hosted zone that contains domain_name."
  type        = string
  default     = "example.com"
}

variable "ws_stage" {
  description = "Stage name for the WebSocket API (CloudFront rewrites /stream to this path)."
  type        = string
  default     = "prod"
}

variable "message_ttl_days" {
  description = "Auto-expire messages after this many days (0 = keep forever, like Gotify)."
  type        = number
  default     = 0
}

variable "basic_auth_username" {
  description = "Single-user login name (used by the Android app to register a client)."
  type        = string
}

variable "basic_auth_password" {
  description = "Single-user login password. Stored encrypted in SSM."
  type        = string
  sensitive   = true
}
