#
# ECS upgrade
#
data "aws_ecs_cluster" "ecs-upgrade" {
  cluster_name = var.ECS_CLUSTER
}

data "template_file" "ecs-upgrade" {
  template = file("${path.module}/templates/ecs-upgrade.json")
  vars = {
    AWS_REGION  = var.AWS_REGION
    ECS_CLUSTER = var.ECS_CLUSTER
    ECS_ASG     = var.ECS_ASG
    IMAGE       = var.IMAGE
  }
}
resource "aws_ecs_task_definition" "ecs-upgrade" {
  family                   = "ecs-upgrade"
  container_definitions    = data.template_file.ecs-upgrade.rendered
  task_role_arn            = aws_iam_role.ecs-upgrade.arn
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  execution_role_arn       = aws_iam_role.ecs_execution_role.arn
  cpu                      = 256
  memory                   = 512
}
#
# IAM role & policy
#
resource "aws_iam_role" "ecs-upgrade" {
  name               = "ecs-upgrade"
  assume_role_policy = <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Action": "sts:AssumeRole",
      "Principal": {
        "Service": "ecs-tasks.amazonaws.com"
      },
      "Effect": "Allow",
      "Sid": ""
    }
  ]
}
EOF
}
resource "aws_iam_role_policy" "ecs-upgrade-policy" {
  name   = "ecs-upgrade-policy"
  role   = aws_iam_role.ecs-upgrade.id
  policy = <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "ecs:Describe*",
        "ecs:List*",
        "ecs:Update*",
        "ec2:Describe*",
        "autoscaling:Describe*",
        "elasticloadbalancing:Describe*",
        "autoscaling:CreateLaunchConfiguration",
        "autoscaling:UpdateAutoScalingGroup",
        "autoscaling:DeleteLaunchConfiguration"
      ],
      "Resource": "*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "iam:PassRole"
      ],
      "Resource": "${var.EC2_IAM_ROLE_ARN}"
    }
  ]
}
EOF
}

# ecs fargate execution role
resource "aws_iam_role" "ecs_execution_role" {
  name               = "ecs-execution-role"
  assume_role_policy = <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Action": "sts:AssumeRole",
      "Principal": {
        "Service": "ecs-tasks.amazonaws.com"
      },
      "Effect": "Allow",
      "Sid": ""
    }
  ]
}
EOF
}
resource "aws_iam_role_policy" "ecs_execution_role_policy" {
  name   = "ecs-execution-role"
  role   = aws_iam_role.ecs_execution_role.id
  policy = <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "ecr:GetAuthorizationToken",
        "ecr:BatchCheckLayerAvailability",
        "ecr:GetDownloadUrlForLayer",
        "ecr:BatchGetImage",
        "logs:CreateLogStream",
        "logs:PutLogEvents"
      ],
      "Resource": "*"
    }
  ]
}
EOF
}

resource "aws_iam_role" "ecs_events_role" {
  name               = "ecs-events-role"
  assume_role_policy = <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "",
      "Effect": "Allow",
      "Principal": {
        "Service": "events.amazonaws.com"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}
EOF
}

resource "aws_iam_role_policy" "ecs_events_policy" {
  name   = "ecs-events-policy"
  role   = aws_iam_role.ecs_events_role.name
  policy = <<EOF
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "ecs:RunTask"
            ],
            "Resource": [
                "*"
            ]
        },
        {
            "Effect": "Allow",
            "Action": "iam:PassRole",
            "Resource": [
                "*"
            ],
            "Condition": {
                "StringLike": {
                    "iam:PassedToService": "ecs-tasks.amazonaws.com"
                }
            }
        }
    ]
}
EOF
}

# cloudwatch log group
resource "aws_cloudwatch_log_group" "ecs-upgrade" {
  name = "ecs-upgrade"
}
# scheduling
resource "aws_cloudwatch_event_rule" "ecs-upgrade" {
  name                = "RunEcsUpgrade"
  description         = "runs ecs upgrade"
  schedule_expression = "rate(7 days)"
}

resource "aws_security_group" "ecs-upgrade" {
  name        = "ecs-upgrade ECS task"
  vpc_id      = var.ECS_VPC_ID
  description = "ecs-upgrade ECS task"
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = [var.EGRESS_CIDR]
  }
}

resource "aws_cloudwatch_event_target" "ecs-upgrade" {
  rule      = aws_cloudwatch_event_rule.ecs-upgrade.name
  target_id = "RunEcsUpgrade"
  arn       = data.aws_ecs_cluster.ecs-upgrade.id
  role_arn  = aws_iam_role.ecs_events_role.arn
  ecs_target {
    group               = "ecs-upgrade"
    launch_type         = "FARGATE"
    task_count          = 1
    task_definition_arn = aws_ecs_task_definition.ecs-upgrade.arn
    network_configuration {
      subnets          = var.ECS_SUBNETS
      security_groups  = [aws_security_group.ecs-upgrade.id]
      assign_public_ip = true
    }
  }
}
