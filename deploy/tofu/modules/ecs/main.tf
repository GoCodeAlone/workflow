terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

resource "aws_ecs_cluster" "main" {
  name = var.cluster_name

  setting {
    name  = "containerInsights"
    value = "enabled"
  }

  tags = {
    Name        = var.cluster_name
    Environment = var.environment
    ManagedBy   = "opentofu"
  }
}

resource "aws_cloudwatch_log_group" "app" {
  name              = "/ecs/${var.cluster_name}/workflow-server"
  retention_in_days = var.log_retention_days

  tags = {
    Environment = var.environment
    ManagedBy   = "opentofu"
  }
}

resource "aws_iam_role" "task_execution" {
  name = "${var.cluster_name}-task-execution-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "ecs-tasks.amazonaws.com"
      }
    }]
  })

  tags = {
    Environment = var.environment
    ManagedBy   = "opentofu"
  }
}

resource "aws_iam_role_policy_attachment" "task_execution" {
  role       = aws_iam_role.task_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

resource "aws_iam_role" "task" {
  name = "${var.cluster_name}-task-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "ecs-tasks.amazonaws.com"
      }
    }]
  })

  tags = {
    Environment = var.environment
    ManagedBy   = "opentofu"
  }
}

resource "aws_iam_role_policy" "task" {
  name = "${var.cluster_name}-task-policy"
  role = aws_iam_role.task.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogStream",
          "logs:PutLogEvents",
          "ssm:GetParameters",
          "secretsmanager:GetSecretValue",
        ]
        Resource = "*"
      }
    ]
  })
}

resource "aws_ecs_task_definition" "app" {
  family                   = "${var.cluster_name}-workflow-server"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = var.cpu
  memory                   = var.memory
  execution_role_arn       = aws_iam_role.task_execution.arn
  task_role_arn            = aws_iam_role.task.arn

  container_definitions = jsonencode([{
    name      = "workflow-server"
    image     = var.image
    essential = true

    portMappings = [{
      containerPort = 8080
      hostPort      = 8080
      protocol      = "tcp"
    }]

    environment = [for k, v in var.environment_vars : { name = k, value = v }]

    secrets = [for k, v in var.secrets : { name = k, valueFrom = v }]

    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = aws_cloudwatch_log_group.app.name
        "awslogs-region"        = var.aws_region
        "awslogs-stream-prefix" = "ecs"
      }
    }

    healthCheck = {
      command     = ["CMD-SHELL", "wget -qO- http://localhost:8080/healthz || exit 1"]
      interval    = 30
      timeout     = 5
      retries     = 3
      startPeriod = 60
    }
  }])

  tags = {
    Environment = var.environment
    ManagedBy   = "opentofu"
  }
}

resource "aws_ecs_service" "app" {
  name            = "${var.cluster_name}-workflow-server"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.app.arn
  desired_count   = var.desired_count
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = var.subnet_ids
    security_groups  = var.security_group_ids
    assign_public_ip = false
  }

  load_balancer {
    target_group_arn = var.target_group_arn
    container_name   = "workflow-server"
    container_port   = 8080
  }

  deployment_minimum_healthy_percent = 50
  deployment_maximum_percent         = 200

  lifecycle {
    ignore_changes = [desired_count]
  }

  tags = {
    Environment = var.environment
    ManagedBy   = "opentofu"
  }
}
