output "endpoint" {
  description = "RDS instance endpoint"
  value       = aws_db_instance.main.endpoint
}

output "port" {
  description = "RDS instance port"
  value       = aws_db_instance.main.port
}

output "db_name" {
  description = "Name of the database"
  value       = aws_db_instance.main.db_name
}

output "security_group_id" {
  description = "Security group ID for the RDS instance"
  value       = aws_security_group.rds.id
}

output "identifier" {
  description = "RDS instance identifier"
  value       = aws_db_instance.main.identifier
}
