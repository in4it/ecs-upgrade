package main

import (
	"strconv"

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

type Autoscaling struct {
	svcAutoscaling *autoscaling.AutoScaling
}

type AutoscalingInstance struct {
	InstanceId            string
	IPs                   []string
	LaunchConfig          string
	LaunchTemplateName    string
	LaunchTemplateVersion string
	HealthStatus          string
}
type AutoscalingGroup struct {
	AutoscalingGroupName    string
	LaunchConfigurationName string
	LaunchTemplateName      string
	DesiredCapacity         int64
	MinSize                 int64
	MaxSize                 int64
}

func NewAutoscaling() Autoscaling {
	return Autoscaling{
		svcAutoscaling: autoscaling.New(session.New()),
	}
}

func (a *Autoscaling) newLaunchTemplateVersion(launchTemplateName string) (string, string, string, error) {
	lt, err := a.getLatestLaunchTemplate(launchTemplateName)
	if err != nil {
		return "", "", "", err
	}
	imageId, err := a.getECSAMI()
	if err != nil {
		return "", "", "", err
	}
	if strings.Compare(imageId, aws.StringValue(lt.LaunchTemplateData.ImageId)) == 0 {
		autoscalingLogger.Infof("ECS Cluster already running latest AMI")
		return "", "", "", nil
	}

	return a.createLaunchTemplateVersion(launchTemplateName, lt, imageId)
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
	var newLaunchConfigName string
	if strings.Index(launchConfig, "-ecsupgrade") > 0 {
		newLaunchConfigName = launchConfig[0:strings.Index(launchConfig, "-ecsupgrade")] + "-ecsupgrade" + time.Now().UTC().Format("20060102150405")
	} else {
		newLaunchConfigName = launchConfig + "-ecsupgrade" + time.Now().UTC().Format("20060102150405")
	}

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
	autoscalingLogger.Debugf("created LaunchConfiguration")
	_, err := a.svcAutoscaling.CreateLaunchConfiguration(input)
	return newLaunchConfigName, err
}

func (a *Autoscaling) getLaunchConfig(launchConfig string) (autoscaling.LaunchConfiguration, error) {
	input := &autoscaling.DescribeLaunchConfigurationsInput{
		LaunchConfigurationNames: aws.StringSlice([]string{launchConfig}),
	}

	var result autoscaling.LaunchConfiguration

	pageNum := 0
	err := a.svcAutoscaling.DescribeLaunchConfigurationsPages(input,
		func(page *autoscaling.DescribeLaunchConfigurationsOutput, lastPage bool) bool {
			pageNum++
			for _, lc := range page.LaunchConfigurations {
				autoscalingLogger.Debugf("Found launch configuration: %s", aws.StringValue(lc.LaunchConfigurationName))
				result = *lc
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
func (a *Autoscaling) createLaunchTemplateVersion(launchTemplateName string, lt ec2.LaunchTemplateVersion, imageId string) (string, string, string, error) {
	svc := ec2.New(session.New())

	input := &ec2.CreateLaunchTemplateVersionInput{
		LaunchTemplateName: aws.String(launchTemplateName),
		LaunchTemplateData: &ec2.RequestLaunchTemplateData{
			ImageId: aws.String(imageId),
		},
		SourceVersion: aws.String(strconv.FormatInt(aws.Int64Value(lt.VersionNumber), 10)),
	}

	autoscalingLogger.Debugf("creating new LaunchTemplateVersion")

	result, err := svc.CreateLaunchTemplateVersion(input)
	return aws.StringValue(result.LaunchTemplateVersion.LaunchTemplateId), aws.StringValue(result.LaunchTemplateVersion.LaunchTemplateName), strconv.FormatInt(aws.Int64Value(result.LaunchTemplateVersion.VersionNumber), 10), err
}

func (e *Autoscaling) getLatestLaunchTemplate(launchTemplateName string) (ec2.LaunchTemplateVersion, error) {
	svc := ec2.New(session.New())

	input := &ec2.DescribeLaunchTemplateVersionsInput{
		LaunchTemplateName: aws.String(launchTemplateName),
		Versions:           aws.StringSlice([]string{"$Latest"}),
	}

	var result ec2.LaunchTemplateVersion

	pageNum := 0
	err := svc.DescribeLaunchTemplateVersionsPages(input,
		func(page *ec2.DescribeLaunchTemplateVersionsOutput, lastPage bool) bool {
			pageNum++
			for _, lt := range page.LaunchTemplateVersions {
				autoscalingLogger.Debugf("Found launch configuration: %s", aws.StringValue(lt.LaunchTemplateName))
				result = *lt
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
			{Name: aws.String("name"), Values: []*string{aws.String("amzn2-ami-ecs-*")}},
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
	input := &autoscaling.UpdateAutoScalingGroupInput{
		AutoScalingGroupName: aws.String(autoScalingGroupName),
		DesiredCapacity:      aws.Int64(desired),
	}
	_, err := a.svcAutoscaling.UpdateAutoScalingGroup(input)
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
	input := &autoscaling.UpdateAutoScalingGroupInput{
		AutoScalingGroupName:    aws.String(autoscalingGroupName),
		LaunchConfigurationName: aws.String(launchConfig),
	}
	_, err := a.svcAutoscaling.UpdateAutoScalingGroup(input)
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

func (a *Autoscaling) updateAutoscalingLaunchTemplate(autoscalingGroupName, launchTemplateName string, launchTemplateVersion string) error {
	input := &autoscaling.UpdateAutoScalingGroupInput{
		AutoScalingGroupName: aws.String(autoscalingGroupName),
		LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
			LaunchTemplateName: aws.String(launchTemplateName),
			Version:            aws.String(launchTemplateVersion),
		},
	}
	_, err := a.svcAutoscaling.UpdateAutoScalingGroup(input)
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
	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{aws.String(autoScalingGroupName)},
	}
	result, err := a.svcAutoscaling.DescribeAutoScalingGroups(input)
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
		AutoscalingGroupName:    autoScalingGroupName,
		MinSize:                 aws.Int64Value(result.AutoScalingGroups[0].MinSize),
		DesiredCapacity:         aws.Int64Value(result.AutoScalingGroups[0].DesiredCapacity),
		MaxSize:                 aws.Int64Value(result.AutoScalingGroups[0].MaxSize),
		LaunchConfigurationName: aws.StringValue(result.AutoScalingGroups[0].LaunchConfigurationName),
		LaunchTemplateName:      aws.StringValue(result.AutoScalingGroups[0].LaunchTemplate.LaunchTemplateName),
	}

	return asg, nil
}

func (a *Autoscaling) getAutoscalingInstanceHealth(autoScalingGroupName string) ([]AutoscalingInstance, error) {
	var instances []AutoscalingInstance

	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{aws.String(autoScalingGroupName)},
	}
	result, err := a.svcAutoscaling.DescribeAutoScalingGroups(input)
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
	err = a.svcAutoscaling.DescribeAutoScalingInstancesPages(input2,
		func(page *autoscaling.DescribeAutoScalingInstancesOutput, lastPage bool) bool {
			pageNum++
			for _, instance := range page.AutoScalingInstances {
				instances = append(instances, AutoscalingInstance{
					InstanceId:            aws.StringValue(instance.InstanceId),
					LaunchConfig:          aws.StringValue(instance.LaunchConfigurationName),
					LaunchTemplateName:    aws.StringValue(instance.LaunchTemplate.LaunchTemplateName),
					LaunchTemplateVersion: aws.StringValue(instance.LaunchTemplate.Version),
					HealthStatus:          aws.StringValue(instance.HealthStatus),
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

	// get IPs
	instancesIPs, err := a.getInstancesIPs(instanceIds)
	if err != nil {
		autoscalingLogger.Errorf("Could not determine instance IPs")
	}
	for instanceID, IPs := range instancesIPs {
		for k, instance := range instances {
			if instance.InstanceId == instanceID {
				instances[k].IPs = IPs
			}
		}
	}

	return instances, nil
}

func (a *Autoscaling) deleteLaunchConfig(launchConfigName string) error {
	input := &autoscaling.DeleteLaunchConfigurationInput{
		LaunchConfigurationName: aws.String(launchConfigName),
	}
	_, err := a.svcAutoscaling.DeleteLaunchConfiguration(input)
	return err
}

func (a *Autoscaling) getInstancesIPs(instanceIds []string) (map[string][]string, error) {
	instances := make(map[string][]string)

	svc := ec2.New(session.New())

	// describe instances
	input := &ec2.DescribeInstancesInput{
		InstanceIds: aws.StringSlice(instanceIds),
	}

	pageNum := 0
	err := svc.DescribeInstancesPages(input,
		func(page *ec2.DescribeInstancesOutput, lastPage bool) bool {
			pageNum++
			for _, reservation := range page.Reservations {
				for _, instance := range reservation.Instances {
					var IPs []string
					for _, networkInterface := range instance.NetworkInterfaces {
						IPs = append(IPs, aws.StringValue(networkInterface.PrivateIpAddress))
					}
					instances[aws.StringValue(instance.InstanceId)] = IPs
				}
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
