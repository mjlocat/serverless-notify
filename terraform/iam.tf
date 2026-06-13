# Both Lambdas share one execution role.
data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "lambda" {
  name               = "${var.project}-lambda"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "lambda_basic" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

data "aws_iam_policy_document" "lambda" {
  statement {
    sid = "DynamoDB"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:PutItem",
      "dynamodb:UpdateItem",
      "dynamodb:DeleteItem",
      "dynamodb:Query",
      "dynamodb:BatchWriteItem",
    ]
    resources = [
      aws_dynamodb_table.notify.arn,
      "${aws_dynamodb_table.notify.arn}/index/*",
    ]
  }

  statement {
    sid       = "ReadAuthParam"
    actions   = ["ssm:GetParameter"]
    resources = [aws_ssm_parameter.auth.arn]
  }

  statement {
    sid       = "DecryptAuthParam"
    actions   = ["kms:Decrypt"]
    resources = [aws_kms_key.auth.arn]
  }

  # Push messages down live WebSocket connections.
  statement {
    sid       = "ManageConnections"
    actions   = ["execute-api:ManageConnections"]
    resources = ["${aws_apigatewayv2_api.ws.execution_arn}/*"]
  }

  # Store uploaded application icons (POST /application/{id}/image).
  statement {
    sid       = "WriteImages"
    actions   = ["s3:PutObject"]
    resources = ["${aws_s3_bucket.images.arn}/image/*"]
  }
}

resource "aws_iam_role_policy" "lambda" {
  name   = "${var.project}-lambda"
  role   = aws_iam_role.lambda.id
  policy = data.aws_iam_policy_document.lambda.json
}
