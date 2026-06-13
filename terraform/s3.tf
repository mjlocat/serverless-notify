# Optional bucket for application icons served at /image/* and /static/*.
# Kept private; only CloudFront reads it via Origin Access Control.
resource "aws_s3_bucket" "images" {
  bucket = "${var.project}-images-${data.aws_caller_identity.current.account_id}"
}

resource "aws_s3_bucket_public_access_block" "images" {
  bucket                  = aws_s3_bucket.images.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_cloudfront_origin_access_control" "images" {
  name                              = "${var.project}-images"
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

data "aws_iam_policy_document" "images" {
  statement {
    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.images.arn}/*"]
    principals {
      type        = "Service"
      identifiers = ["cloudfront.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "AWS:SourceArn"
      values   = [aws_cloudfront_distribution.cdn.arn]
    }
  }
}

resource "aws_s3_bucket_policy" "images" {
  bucket = aws_s3_bucket.images.id
  policy = data.aws_iam_policy_document.images.json
}

# The fallback icon every application gets at creation (image = static/defaultapp.png).
# Without this object the app's icon request 403s against the locked-down bucket.
resource "aws_s3_object" "default_app_icon" {
  bucket       = aws_s3_bucket.images.id
  key          = "static/defaultapp.png"
  source       = "${path.module}/assets/defaultapp.png"
  source_hash  = filemd5("${path.module}/assets/defaultapp.png")
  content_type = "image/png"
}
