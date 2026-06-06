output "app_url" {
  description = "Point the Gotify clients and Android app here."
  value       = "https://${var.domain_name}"
}

output "cloudfront_domain" {
  value = aws_cloudfront_distribution.cdn.domain_name
}

output "http_api_endpoint" {
  value = aws_apigatewayv2_api.http.api_endpoint
}

output "ws_api_endpoint" {
  value = aws_apigatewayv2_api.ws.api_endpoint
}

output "dynamodb_table" {
  value = aws_dynamodb_table.notify.name
}
