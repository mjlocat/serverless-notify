# CloudFront unifies the REST API and the WebSocket API under one hostname,
# which is required because the stock Gotify app derives wss://host/stream from
# the same base URL as REST, and API Gateway forbids sharing a custom domain
# between an HTTP and a WebSocket API.

data "aws_cloudfront_cache_policy" "disabled" {
  name = "Managed-CachingDisabled"
}

data "aws_cloudfront_cache_policy" "optimized" {
  name = "Managed-CachingOptimized"
}

# Forwards all viewer headers (incl. Authorization and Sec-WebSocket-*) and
# query strings to the origin, but rewrites Host to the origin's host so the
# execute-api endpoints accept the request.
data "aws_cloudfront_origin_request_policy" "all_viewer_except_host" {
  name = "Managed-AllViewerExceptHostHeader"
}

# Rewrites the /stream upgrade request to the WebSocket API's stage path. This
# runs on the WebSocket handshake (viewer-request) and is what makes /stream
# reach the WS API, which otherwise only answers at /<stage>.
resource "aws_cloudfront_function" "stream_rewrite" {
  name    = "${var.project}-stream-rewrite"
  runtime = "cloudfront-js-2.0"
  comment = "Rewrite /stream to the WebSocket API stage path"
  publish = true
  code    = <<-EOT
    function handler(event) {
      var request = event.request;
      request.uri = "/${var.ws_stage}";
      return request;
    }
  EOT
}

locals {
  http_origin = "${aws_apigatewayv2_api.http.id}.execute-api.${var.region}.amazonaws.com"
  ws_origin   = "${aws_apigatewayv2_api.ws.id}.execute-api.${var.region}.amazonaws.com"
}

resource "aws_cloudfront_distribution" "cdn" {
  enabled         = true
  is_ipv6_enabled = true
  aliases         = [var.domain_name]
  price_class     = "PriceClass_100"
  comment         = var.project

  origin {
    origin_id   = "http-api"
    domain_name = local.http_origin
    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = "https-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }

  origin {
    origin_id   = "ws-api"
    domain_name = local.ws_origin
    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = "https-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }

  origin {
    origin_id                = "images"
    domain_name              = aws_s3_bucket.images.bucket_regional_domain_name
    origin_access_control_id = aws_cloudfront_origin_access_control.images.id
  }

  # Default: the Gotify REST API.
  default_cache_behavior {
    target_origin_id         = "http-api"
    viewer_protocol_policy   = "redirect-to-https"
    allowed_methods          = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
    cached_methods           = ["GET", "HEAD"]
    cache_policy_id          = data.aws_cloudfront_cache_policy.disabled.id
    origin_request_policy_id = data.aws_cloudfront_origin_request_policy.all_viewer_except_host.id
  }

  # WebSocket stream.
  ordered_cache_behavior {
    path_pattern             = "/stream*"
    target_origin_id         = "ws-api"
    viewer_protocol_policy   = "redirect-to-https"
    allowed_methods          = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
    cached_methods           = ["GET", "HEAD"]
    cache_policy_id          = data.aws_cloudfront_cache_policy.disabled.id
    origin_request_policy_id = data.aws_cloudfront_origin_request_policy.all_viewer_except_host.id

    function_association {
      event_type   = "viewer-request"
      function_arn = aws_cloudfront_function.stream_rewrite.arn
    }
  }

  # Application icons (optional; bucket may be empty, app falls back to default).
  ordered_cache_behavior {
    path_pattern           = "/image/*"
    target_origin_id       = "images"
    viewer_protocol_policy = "redirect-to-https"
    allowed_methods        = ["GET", "HEAD", "OPTIONS"]
    cached_methods         = ["GET", "HEAD"]
    cache_policy_id        = data.aws_cloudfront_cache_policy.optimized.id
  }

  ordered_cache_behavior {
    path_pattern           = "/static/*"
    target_origin_id       = "images"
    viewer_protocol_policy = "redirect-to-https"
    allowed_methods        = ["GET", "HEAD", "OPTIONS"]
    cached_methods         = ["GET", "HEAD"]
    cache_policy_id        = data.aws_cloudfront_cache_policy.optimized.id
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  viewer_certificate {
    acm_certificate_arn      = aws_acm_certificate_validation.cdn.certificate_arn
    ssl_support_method       = "sni-only"
    minimum_protocol_version = "TLSv1.2_2021"
  }
}
