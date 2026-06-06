# Zip the prebuilt `bootstrap` binaries produced by `make build`. Run the build
# before `terraform plan/apply` or these data sources will fail.
data "archive_file" "api" {
  type        = "zip"
  source_dir  = "${path.module}/../dist/api"
  output_path = "${path.module}/../dist/api.zip"
}

data "archive_file" "ws" {
  type        = "zip"
  source_dir  = "${path.module}/../dist/ws"
  output_path = "${path.module}/../dist/ws.zip"
}

resource "aws_lambda_function" "api" {
  function_name    = "${var.project}-api"
  role             = aws_iam_role.lambda.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = data.archive_file.api.output_path
  source_code_hash = data.archive_file.api.output_base64sha256
  timeout          = 15
  memory_size      = 128

  environment {
    variables = {
      TABLE_NAME          = aws_dynamodb_table.notify.name
      BYTOKEN_INDEX       = "byToken"
      BYAPP_INDEX         = "byApp"
      WS_MGMT_ENDPOINT    = "https://${aws_apigatewayv2_api.ws.id}.execute-api.${var.region}.amazonaws.com/${var.ws_stage}"
      BASE_URL            = "https://${var.domain_name}"
      SSM_AUTH_PARAM      = aws_ssm_parameter.auth.name
      MESSAGE_TTL_DAYS    = tostring(var.message_ttl_days)
      CORS_ALLOWED_ORIGIN = var.cors_allowed_origin
    }
  }
}

resource "aws_lambda_function" "ws" {
  function_name    = "${var.project}-ws"
  role             = aws_iam_role.lambda.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = data.archive_file.ws.output_path
  source_code_hash = data.archive_file.ws.output_base64sha256
  timeout          = 10
  memory_size      = 128

  environment {
    variables = {
      TABLE_NAME    = aws_dynamodb_table.notify.name
      BYTOKEN_INDEX = "byToken"
      BYAPP_INDEX   = "byApp"
    }
  }
}

resource "aws_cloudwatch_log_group" "api" {
  name              = "/aws/lambda/${aws_lambda_function.api.function_name}"
  retention_in_days = 14
}

resource "aws_cloudwatch_log_group" "ws" {
  name              = "/aws/lambda/${aws_lambda_function.ws.function_name}"
  retention_in_days = 14
}

resource "aws_lambda_permission" "api" {
  statement_id  = "AllowHTTPAPIInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.api.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.http.execution_arn}/*/*"
}

resource "aws_lambda_permission" "ws" {
  statement_id  = "AllowWSAPIInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.ws.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.ws.execution_arn}/*/*"
}
