# Basic-auth credentials are stored encrypted with a dedicated KMS key so the
# Lambda execution role's kms:Decrypt grant can be scoped to exactly this key.
resource "aws_kms_key" "auth" {
  description             = "${var.project} basic-auth credential encryption"
  deletion_window_in_days = 7
  enable_key_rotation     = true
}

resource "aws_kms_alias" "auth" {
  name          = "alias/${var.project}-auth"
  target_key_id = aws_kms_key.auth.key_id
}

resource "aws_ssm_parameter" "auth" {
  name   = "/${var.project}/auth"
  type   = "SecureString"
  key_id = aws_kms_key.auth.key_id
  value  = jsonencode({ username = var.basic_auth_username, password = var.basic_auth_password })
}
