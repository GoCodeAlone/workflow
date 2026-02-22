terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

resource "aws_db_subnet_group" "main" {
  name       = "${var.project_name}-${var.environment}-rds-subnet-group"
  subnet_ids = var.subnet_ids

  tags = {
    Name        = "${var.project_name}-${var.environment}-rds-subnet-group"
    Environment = var.environment
    ManagedBy   = "opentofu"
  }
}

resource "aws_security_group" "rds" {
  name        = "${var.project_name}-${var.environment}-rds-sg"
  description = "Security group for RDS PostgreSQL"
  vpc_id      = var.vpc_id

  ingress {
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = var.allowed_security_group_ids
    description     = "PostgreSQL from ECS"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Allow all outbound"
  }

  tags = {
    Name        = "${var.project_name}-${var.environment}-rds-sg"
    Environment = var.environment
    ManagedBy   = "opentofu"
  }
}

resource "aws_db_instance" "main" {
  identifier = "${var.project_name}-${var.environment}-postgres"

  engine               = "postgres"
  engine_version       = "16"
  instance_class       = var.instance_class
  allocated_storage    = var.allocated_storage
  max_allocated_storage = var.max_allocated_storage
  storage_type         = "gp3"
  storage_encrypted    = true

  db_name  = var.db_name
  username = var.username
  password = var.password

  db_subnet_group_name   = aws_db_subnet_group.main.name
  vpc_security_group_ids = [aws_security_group.rds.id]
  multi_az               = var.multi_az
  publicly_accessible    = false

  backup_retention_period = var.backup_retention_days
  backup_window           = "03:00-04:00"
  maintenance_window      = "sun:04:00-sun:05:00"

  performance_insights_enabled          = var.enable_performance_insights
  performance_insights_retention_period = var.enable_performance_insights ? 7 : null

  deletion_protection = var.deletion_protection
  skip_final_snapshot = var.environment == "dev" ? true : false
  final_snapshot_identifier = var.environment == "dev" ? null : "${var.project_name}-${var.environment}-final-snapshot"

  tags = {
    Name        = "${var.project_name}-${var.environment}-postgres"
    Environment = var.environment
    ManagedBy   = "opentofu"
  }
}
