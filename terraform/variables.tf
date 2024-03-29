variable "AWS_REGION" {}

variable "ECS_CLUSTER" {}

variable "LAUNCH_TEMPLATES" {}

variable "DEBUG" {
  type    = string
  default = true
}

variable "ECS_ASG" {}

variable "IMAGE" {
  default = "in4it/ecs-upgrade:0.0.1"
}

variable "ECS_VPC_ID" {}

# List of subnets
variable "ECS_SUBNETS" {
  type = list(string)
}

variable "EC2_IAM_ROLE_ARN" {}

variable "EGRESS_CIDR" {
  default = "0.0.0.0/0"
}

variable "ASSIGN_PUBLIC_IP" {
  default = false
}

variable "cloudwatch_log_retention_period" {
  description = "cloudwatch retention period in days"
  default     = "0"
}