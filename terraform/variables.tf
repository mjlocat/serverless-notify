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

variable "throttle_rate_limit" {
  description = "API Gateway steady-state requests/sec per stage (cost/DoS guardrail)."
  type        = number
  default     = 20
}

variable "throttle_burst_limit" {
  description = "API Gateway burst request limit per stage."
  type        = number
  default     = 40
}

variable "cors_allowed_origin" {
  description = "Exact browser origin allowed via CORS (empty = no CORS; native clients don't need it)."
  type        = string
  default     = ""
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
