terraform {
  required_version = ">= 1.6.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }

  # backend "s3" {
  #   bucket         = "workflow-tofu-state"
  #   key            = "staging/terraform.tfstate"
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
      Environment = "staging"
      ManagedBy   = "opentofu"
    }
  }
}

module "ecr" {
  source          = "../../modules/ecr"
  repository_name = "workflow-server"
}

module "vpc" {
  source             = "../../modules/vpc"
  environment        = "staging"
  project_name       = "workflow"
  vpc_cidr           = var.vpc_cidr
  enable_nat_gateway = true
}

module "alb" {
  source            = "../../modules/alb"
  environment       = "staging"
  project_name      = "workflow"
  vpc_id            = module.vpc.vpc_id
  subnet_ids        = module.vpc.public_subnet_ids
  certificate_arn   = var.certificate_arn
  health_check_path = "/healthz"
}

module "rds" {
  source       = "../../modules/rds"
  environment  = "staging"
  project_name = "workflow"

  instance_class              = "db.t3.small"
  allocated_storage           = 20
  db_name                     = "workflow"
  username                    = var.db_username
  password                    = var.db_password
  subnet_ids                  = module.vpc.private_subnet_ids
  vpc_id                      = module.vpc.vpc_id
  allowed_security_group_ids  = [aws_security_group.ecs.id]
  multi_az                    = true
  backup_retention_days       = 7
  enable_performance_insights = true
  deletion_protection         = true
}

module "elasticache" {
  source       = "../../modules/elasticache"
  environment  = "staging"
  project_name = "workflow"

  node_type       = "cache.t3.small"
  num_cache_nodes = 1
  subnet_ids      = module.vpc.private_subnet_ids
  vpc_id          = module.vpc.vpc_id

  allowed_security_group_ids = [aws_security_group.ecs.id]
  snapshot_retention_days    = 3
}

module "ecs" {
  source       = "../../modules/ecs"
  environment  = "staging"
  cluster_name = "workflow-staging"
  aws_region   = var.aws_region

  image         = "${module.ecr.repository_url}:${var.image_tag}"
  cpu           = 512
  memory        = 1024
  desired_count = 2

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

  log_retention_days = 30
}

resource "aws_security_group" "ecs" {
  name        = "workflow-staging-ecs-sg"
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
    Name = "workflow-staging-ecs-sg"
  }
}

resource "aws_appautoscaling_target" "ecs" {
  max_capacity       = 4
  min_capacity       = 2
  resource_id        = "service/${module.ecs.cluster_id}/${module.ecs.service_name}"
  scalable_dimension = "ecs:service:DesiredCount"
  service_namespace  = "ecs"
}

resource "aws_appautoscaling_policy" "cpu" {
  name               = "workflow-staging-cpu-autoscaling"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.ecs.resource_id
  scalable_dimension = aws_appautoscaling_target.ecs.scalable_dimension
  service_namespace  = aws_appautoscaling_target.ecs.service_namespace

  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "ECSServiceAverageCPUUtilization"
    }
    target_value       = 70.0
    scale_in_cooldown  = 300
    scale_out_cooldown = 60
  }
}

resource "aws_ssm_parameter" "db_password" {
  name  = "/workflow/staging/db-password"
  type  = "SecureString"
  value = var.db_password

  tags = {
    Environment = "staging"
  }
}

module "monitoring" {
  source       = "../../modules/monitoring"
  environment  = "staging"
  project_name = "workflow"

  cluster_name   = module.ecs.cluster_id
  service_name   = module.ecs.service_name
  alb_arn_suffix = module.alb.alb_arn_suffix
  alert_email    = var.alert_email

  cpu_alarm_threshold    = 75
  memory_alarm_threshold = 80
  error_rate_threshold   = 5
}
