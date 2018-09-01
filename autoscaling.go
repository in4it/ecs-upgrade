package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/juju/loggo"

	"errors"
	"strings"
	"time"
)

// logging
var autoscalingLogger = loggo.GetLogger("autoscaling")

type Autoscaling struct{}

type AutoscalingInstance struct {
	InstanceId   string
	LaunchConfig string
	HealthStatus string
}
type AutoscalingGroup struct {
	LaunchConfigurationName string
	DesiredCapacity         int64
	MinSize                 int64
	MaxSize                 int64
}

func (a *Autoscaling) newLaunchConfigFromExisting(launchConfig string) (string, error) {
	lc, err := a.getLaunchConfig(launchConfig)
	if err != nil {
		return "", err
	}
	imageId, err := a.getECSAMI()
	if err != nil {
		return "", err
	}
	if strings.Compare(imageId, aws.StringValue(lc.ImageId)) == 0 {
		autoscalingLogger.Infof("ECS Cluster already running latest AMI")
		return "", nil
	}

	return a.createLaunchConfig(launchConfig, lc, imageId)
}

func (a *Autoscaling) createLaunchConfig(launchConfig string, lc autoscaling.LaunchConfiguration, imageId string) (string, error) {
	newLaunchConfigName := launchConfig + time.Now().UTC().Format("20060102150405") + "-ecsupgrade"
	svc := autoscaling.New(session.New())

	input := &autoscaling.CreateLaunchConfigurationInput{
		AssociatePublicIpAddress:     lc.AssociatePublicIpAddress,
		BlockDeviceMappings:          lc.BlockDeviceMappings,
		ClassicLinkVPCId:             lc.ClassicLinkVPCId,
		ClassicLinkVPCSecurityGroups: lc.ClassicLinkVPCSecurityGroups,
		EbsOptimized:                 lc.EbsOptimized,
		IamInstanceProfile:           lc.IamInstanceProfile,
		ImageId:                      aws.String(imageId),
		InstanceMonitoring:           lc.InstanceMonitoring,
		InstanceType:                 lc.InstanceType,
		KeyName:                      lc.KeyName,
		LaunchConfigurationName:      aws.String(newLaunchConfigName),
		PlacementTenancy:             lc.PlacementTenancy,
		SecurityGroups:               lc.SecurityGroups,
		SpotPrice:                    lc.SpotPrice,
		UserData:                     lc.UserData,
	}
	if aws.StringValue(lc.KernelId) != "" {
		input.SetKernelId(aws.StringValue(lc.KernelId))
	}
	if aws.StringValue(lc.RamdiskId) != "" {
		input.SetRamdiskId(aws.StringValue(lc.RamdiskId))
	}
	autoscalingLogger.Debugf("createLaunchConfiguration with: %+v", input)
	_, err := svc.CreateLaunchConfiguration(input)
	return newLaunchConfigName, err
}
func (e *Autoscaling) getLaunchConfig(launchConfig string) (autoscaling.LaunchConfiguration, error) {
	svc := autoscaling.New(session.New())

	input := &autoscaling.DescribeLaunchConfigurationsInput{
		LaunchConfigurationNames: aws.StringSlice([]string{}),
	}

	var result autoscaling.LaunchConfiguration

	pageNum := 0
	err := svc.DescribeLaunchConfigurationsPages(input,
		func(page *autoscaling.DescribeLaunchConfigurationsOutput, lastPage bool) bool {
			pageNum++
			for _, lc := range page.LaunchConfigurations {
				lcName := aws.StringValue(lc.LaunchConfigurationName)
				if len(lcName) >= len(launchConfig) {
					if strings.Compare(launchConfig, lcName[0:len(launchConfig)]) == 0 {
						autoscalingLogger.Debugf("Found launch configuration: %s", lcName)
						result = *lc
					}
				}
			}
			return pageNum <= 5
		})

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			autoscalingLogger.Errorf(aerr.Error())
		} else {
			autoscalingLogger.Errorf(err.Error())
		}
	}
	return result, err
}

func (a *Autoscaling) getECSAMI() (string, error) {
	var amiId string
	svc := ec2.New(session.New())
	input := &ec2.DescribeImagesInput{
		Owners: []*string{aws.String("591542846629")}, // AWS
		Filters: []*ec2.Filter{
			{Name: aws.String("name"), Values: []*string{aws.String("amzn-ami-*-amazon-ecs-optimized")}},
			{Name: aws.String("virtualization-type"), Values: []*string{aws.String("hvm")}},
		},
	}
	result, err := svc.DescribeImages(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			autoscalingLogger.Errorf("%v", aerr.Error())
		} else {
			autoscalingLogger.Errorf("%v", err.Error())
		}
		return amiId, err
	}
	if len(result.Images) == 0 {
		return amiId, errors.New("No ECS AMI found")
	}
	layout := "2006-01-02T15:04:05.000Z"
	var lastTime time.Time
	for _, v := range result.Images {
		t, err := time.Parse(layout, *v.CreationDate)
		if err != nil {
			return amiId, err
		}
		if t.After(lastTime) {
			lastTime = t
			amiId = *v.ImageId
		}
	}
	return amiId, nil
}

func (a *Autoscaling) scaleAutoscalingGroup(autoScalingGroupName string, desired int64) error {
	svc := autoscaling.New(session.New())
	input := &autoscaling.UpdateAutoScalingGroupInput{
		AutoScalingGroupName: aws.String(autoScalingGroupName),
		DesiredCapacity:      aws.Int64(desired),
	}
	_, err := svc.UpdateAutoScalingGroup(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			autoscalingLogger.Errorf("%v", aerr.Error())
		} else {
			autoscalingLogger.Errorf("%v", err.Error())
		}
		return err
	}
	return nil
}

func (a *Autoscaling) updateAutoscalingLaunchConfig(autoscalingGroupName, launchConfig string) error {
	svc := autoscaling.New(session.New())
	input := &autoscaling.UpdateAutoScalingGroupInput{
		AutoScalingGroupName:    aws.String(autoscalingGroupName),
		LaunchConfigurationName: aws.String(launchConfig),
	}
	_, err := svc.UpdateAutoScalingGroup(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			autoscalingLogger.Errorf("%v", aerr.Error())
		} else {
			autoscalingLogger.Errorf("%v", err.Error())
		}
		return err
	}
	return nil
}

func (a *Autoscaling) describeAutoscalingGroup(autoScalingGroupName string) (AutoscalingGroup, error) {
	svc := autoscaling.New(session.New())
	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{aws.String(autoScalingGroupName)},
	}
	result, err := svc.DescribeAutoScalingGroups(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			autoscalingLogger.Errorf("%v", aerr.Error())
		} else {
			autoscalingLogger.Errorf("%v", err.Error())
		}
		return AutoscalingGroup{}, err
	}
	if len(result.AutoScalingGroups) == 0 {
		return AutoscalingGroup{}, errors.New("No autoscaling groups returned")
	}

	asg := AutoscalingGroup{
		MinSize:                 aws.Int64Value(result.AutoScalingGroups[0].MinSize),
		DesiredCapacity:         aws.Int64Value(result.AutoScalingGroups[0].DesiredCapacity),
		MaxSize:                 aws.Int64Value(result.AutoScalingGroups[0].MaxSize),
		LaunchConfigurationName: aws.StringValue(result.AutoScalingGroups[0].LaunchConfigurationName),
	}

	return asg, nil
}

func (a *Autoscaling) getAutoscalingInstanceHealth(autoScalingGroupName string) ([]AutoscalingInstance, error) {
	var instances []AutoscalingInstance

	svc := autoscaling.New(session.New())
	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{aws.String(autoScalingGroupName)},
	}
	result, err := svc.DescribeAutoScalingGroups(input)
	if err != nil {
		autoscalingLogger.Errorf("%v", err.Error())
		return instances, err
	}
	if len(result.AutoScalingGroups) == 0 {
		return instances, errors.New("No autoscaling groups returned")
	}

	var instanceIds []string
	for _, instance := range result.AutoScalingGroups[0].Instances {
		instanceIds = append(instanceIds, aws.StringValue(instance.InstanceId))
	}

	// describe autoscaling instances
	input2 := &autoscaling.DescribeAutoScalingInstancesInput{
		InstanceIds: aws.StringSlice(instanceIds),
	}

	pageNum := 0
	err = svc.DescribeAutoScalingInstancesPages(input2,
		func(page *autoscaling.DescribeAutoScalingInstancesOutput, lastPage bool) bool {
			pageNum++
			for _, instance := range page.AutoScalingInstances {
				instances = append(instances, AutoscalingInstance{
					InstanceId:   aws.StringValue(instance.InstanceId),
					LaunchConfig: aws.StringValue(instance.LaunchConfigurationName),
					HealthStatus: aws.StringValue(instance.HealthStatus),
				})
			}
			return pageNum <= 10
		})

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			autoscalingLogger.Errorf(aerr.Error())
		} else {
			autoscalingLogger.Errorf(err.Error())
		}
	}

	return instances, nil
}
