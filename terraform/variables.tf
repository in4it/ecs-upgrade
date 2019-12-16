variable "AWS_REGION" {}

variable "ECS_CLUSTER" {}

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
