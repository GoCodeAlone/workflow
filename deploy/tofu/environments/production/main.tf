terraform {
  required_version = ">= 1.6.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }

  backend "s3" {
    bucket         = "workflow-tofu-state"
    key            = "production/terraform.tfstate"
    region         = "us-east-1"
    dynamodb_table = "workflow-tofu-locks"
    encrypt        = true
  }
}

provider "aws" {
  region = var.aws_region

  default_tags {
    tags = {
      Project     = "workflow"
      Environment = "production"
      ManagedBy   = "opentofu"
    }
  }
}

module "ecr" {
  source          = "../../modules/ecr"
  repository_name = "workflow-server"
  max_image_count = 30
}

module "vpc" {
  source             = "../../modules/vpc"
  environment        = "production"
  project_name       = "workflow"
  vpc_cidr           = var.vpc_cidr
  enable_nat_gateway = true
}

module "alb" {
  source             = "../../modules/alb"
  environment        = "production"
  project_name       = "workflow"
  vpc_id             = module.vpc.vpc_id
  subnet_ids         = module.vpc.public_subnet_ids
  certificate_arn    = var.certificate_arn
  health_check_path  = "/healthz"
  access_logs_bucket = var.alb_logs_bucket
}

module "rds" {
  source       = "../../modules/rds"
  environment  = "production"
  project_name = "workflow"

  instance_class              = "db.r7g.large"
  allocated_storage           = 100
  max_allocated_storage       = 500
  db_name                     = "workflow"
  username                    = var.db_username
  password                    = var.db_password
  subnet_ids                  = module.vpc.private_subnet_ids
  vpc_id                      = module.vpc.vpc_id
  allowed_security_group_ids  = [aws_security_group.ecs.id]
  multi_az                    = true
  backup_retention_days       = 30
  enable_performance_insights = true
  deletion_protection         = true
}

module "elasticache" {
  source       = "../../modules/elasticache"
  environment  = "production"
  project_name = "workflow"

  node_type       = "cache.r7g.large"
  num_cache_nodes = 2
  subnet_ids      = module.vpc.private_subnet_ids
  vpc_id          = module.vpc.vpc_id

  allowed_security_group_ids = [aws_security_group.ecs.id]
  snapshot_retention_days    = 7
}

module "ecs" {
  source       = "../../modules/ecs"
  environment  = "production"
  cluster_name = "workflow-production"
  aws_region   = var.aws_region

  image         = "${module.ecr.repository_url}:${var.image_tag}"
  cpu           = 2048
  memory        = 4096
  desired_count = var.ecs_desired_count

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

  log_retention_days = 90
}

resource "aws_security_group" "ecs" {
  name        = "workflow-production-ecs-sg"
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
    Name = "workflow-production-ecs-sg"
  }
}

resource "aws_appautoscaling_target" "ecs" {
  max_capacity       = var.ecs_max_count
  min_capacity       = var.ecs_min_count
  resource_id        = "service/${module.ecs.cluster_id}/${module.ecs.service_name}"
  scalable_dimension = "ecs:service:DesiredCount"
  service_namespace  = "ecs"
}

resource "aws_appautoscaling_policy" "cpu" {
  name               = "workflow-production-cpu-autoscaling"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.ecs.resource_id
  scalable_dimension = aws_appautoscaling_target.ecs.scalable_dimension
  service_namespace  = aws_appautoscaling_target.ecs.service_namespace

  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "ECSServiceAverageCPUUtilization"
    }
    target_value       = 65.0
    scale_in_cooldown  = 300
    scale_out_cooldown = 60
  }
}

resource "aws_appautoscaling_policy" "memory" {
  name               = "workflow-production-memory-autoscaling"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.ecs.resource_id
  scalable_dimension = aws_appautoscaling_target.ecs.scalable_dimension
  service_namespace  = aws_appautoscaling_target.ecs.service_namespace

  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "ECSServiceAverageMemoryUtilization"
    }
    target_value       = 70.0
    scale_in_cooldown  = 300
    scale_out_cooldown = 60
  }
}

resource "aws_ssm_parameter" "db_password" {
  name  = "/workflow/production/db-password"
  type  = "SecureString"
  value = var.db_password

  tags = {
    Environment = "production"
  }
}

module "monitoring" {
  source       = "../../modules/monitoring"
  environment  = "production"
  project_name = "workflow"

  cluster_name   = module.ecs.cluster_id
  service_name   = module.ecs.service_name
  alb_arn_suffix = module.alb.alb_arn_suffix
  alert_email    = var.alert_email

  cpu_alarm_threshold    = 70
  memory_alarm_threshold = 75
  error_rate_threshold   = 5
}
