output "dashboard_name" {
  description = "Name of the CloudWatch dashboard"
  value       = aws_cloudwatch_dashboard.main.dashboard_name
}

output "sns_topic_arn" {
  description = "ARN of the SNS alerts topic"
  value       = aws_sns_topic.alerts.arn
}

output "cpu_alarm_name" {
  description = "Name of the CPU utilization alarm"
  value       = aws_cloudwatch_metric_alarm.cpu_high.alarm_name
}

output "memory_alarm_name" {
  description = "Name of the memory utilization alarm"
  value       = aws_cloudwatch_metric_alarm.memory_high.alarm_name
}
