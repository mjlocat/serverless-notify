terraform {
  required_version = ">= 1.11" # S3 native state locking (use_lockfile)

  backend "s3" {
    bucket       = "serverless-notify-tfstate-316974673039"
    key          = "serverless-notify/terraform.tfstate"
    region       = "us-east-1"
    encrypt      = true
    use_lockfile = true
  }

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.40"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.4"
    }
  }
}

provider "aws" {
  region = var.region
}

# CloudFront viewer certificates must be issued in us-east-1, regardless of the
# region the rest of the stack runs in.
provider "aws" {
  alias  = "us_east_1"
  region = "us-east-1"
}

data "aws_caller_identity" "current" {}
