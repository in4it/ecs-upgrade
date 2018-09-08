# ECS Upgrade

* Create new Launch Configuration based on existing launch config with latest ECS optimized AMI
* Autoscale to double the instances
* Check whether instances are still healthy
* Autoscale back to instance count before scaling event
* Cleanup

# AWS Configuration
* Autoscaling group with termination policies: OldestLaunchConfiguration, OldestInstance

# Run
Manual docker command:
```
docker run -it -e AWS_ACCESS_KEY_ID=... -e AWS_SECRET_ACCESS_KEY=... -e AWS_REGION=... -e ECS_ASG=your-asg -e ECS_CLUSTER=yourcluster in4it/ecs-upgrade
```

