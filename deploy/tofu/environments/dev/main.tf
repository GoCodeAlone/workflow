terraform {
  required_version = ">= 1.6.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }

  # Uncomment to use S3 backend for remote state
  # backend "s3" {
  #   bucket         = "workflow-tofu-state"
  #   key            = "dev/terraform.tfstate"
  #   region         = "us-east-1"
  #   dynamodb_table = "workflow-tofu-locks"
  #   encrypt        = true
  # }
}

provider "aws" {
  region = var.aws_region

  default_tags {
    tags = {
      Project     = "workflow"
      Environment = "dev"
      ManagedBy   = "opentofu"
    }
  }
}

module "ecr" {
  source          = "../../modules/ecr"
  repository_name = "workflow-server"
}

module "vpc" {
  source       = "../../modules/vpc"
  environment  = "dev"
  project_name = "workflow"
  vpc_cidr     = var.vpc_cidr
  # Use NAT gateway for dev (disable to save cost during testing)
  enable_nat_gateway = var.enable_nat_gateway
}

module "alb" {
  source            = "../../modules/alb"
  environment       = "dev"
  project_name      = "workflow"
  vpc_id            = module.vpc.vpc_id
  subnet_ids        = module.vpc.public_subnet_ids
  certificate_arn   = var.certificate_arn
  health_check_path = "/healthz"
}

module "rds" {
  source       = "../../modules/rds"
  environment  = "dev"
  project_name = "workflow"

  instance_class = "db.t3.micro"
  db_name        = "workflow"
  username       = var.db_username
  password       = var.db_password
  subnet_ids     = module.vpc.private_subnet_ids
  vpc_id         = module.vpc.vpc_id

  allowed_security_group_ids = [aws_security_group.ecs.id]
  multi_az                   = false
  backup_retention_days      = 3
  deletion_protection        = false
}

module "elasticache" {
  source       = "../../modules/elasticache"
  environment  = "dev"
  project_name = "workflow"

  node_type       = "cache.t3.micro"
  num_cache_nodes = 1
  subnet_ids      = module.vpc.private_subnet_ids
  vpc_id          = module.vpc.vpc_id

  allowed_security_group_ids = [aws_security_group.ecs.id]
}

module "ecs" {
  source       = "../../modules/ecs"
  environment  = "dev"
  cluster_name = "workflow-dev"
  aws_region   = var.aws_region

  image         = "${module.ecr.repository_url}:${var.image_tag}"
  cpu           = 256
  memory        = 512
  desired_count = 1

  subnet_ids         = module.vpc.private_subnet_ids
  security_group_ids = [aws_security_group.ecs.id]
  target_group_arn   = module.alb.target_group_arn

  environment_vars = {
    WORKFLOW_ADDR       = ":8080"
    WORKFLOW_DB_HOST    = module.rds.endpoint
    WORKFLOW_DB_NAME    = module.rds.db_name
    WORKFLOW_REDIS_ADDR = "${module.elasticache.endpoint}:${module.elasticache.port}"
  }

  secrets = {
    WORKFLOW_DB_PASSWORD = aws_ssm_parameter.db_password.arn
  }
}

resource "aws_security_group" "ecs" {
  name        = "workflow-dev-ecs-sg"
  description = "Security group for ECS tasks"
  vpc_id      = module.vpc.vpc_id

  ingress {
    from_port       = 8080
    to_port         = 8080
    protocol        = "tcp"
    security_groups = [module.alb.alb_security_group_id]
    description     = "HTTP from ALB"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Allow all outbound"
  }

  tags = {
    Name = "workflow-dev-ecs-sg"
  }
}

resource "aws_ssm_parameter" "db_password" {
  name  = "/workflow/dev/db-password"
  type  = "SecureString"
  value = var.db_password

  tags = {
    Environment = "dev"
  }
}

module "monitoring" {
  source       = "../../modules/monitoring"
  environment  = "dev"
  project_name = "workflow"

  cluster_name   = module.ecs.cluster_id
  service_name   = module.ecs.service_name
  alb_arn_suffix = module.alb.alb_arn_suffix
  alert_email    = var.alert_email
}
