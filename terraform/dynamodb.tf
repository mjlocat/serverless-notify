# Single on-demand table holding applications, clients, messages, live
# WebSocket connections and the id counter. See internal/store/store.go for the
# partition/key layout.
resource "aws_dynamodb_table" "notify" {
  name         = var.project
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "PK"
  range_key    = "SK"

  attribute {
    name = "PK"
    type = "S"
  }
  attribute {
    name = "SK"
    type = "S"
  }
  attribute {
    name = "tokenPK"
    type = "S"
  }
  attribute {
    name = "appPK"
    type = "S"
  }
  attribute {
    name = "appSK"
    type = "S"
  }

  # Resolve an app/client access token on every auth check.
  global_secondary_index {
    name            = "byToken"
    hash_key        = "tokenPK"
    projection_type = "ALL"
  }

  # List/delete a single application's messages.
  global_secondary_index {
    name            = "byApp"
    hash_key        = "appPK"
    range_key       = "appSK"
    projection_type = "ALL"
  }

  # Expires connection records (and messages when message_ttl_days > 0).
  ttl {
    attribute_name = "ttl"
    enabled        = true
  }

  point_in_time_recovery {
    enabled = true
  }
}
