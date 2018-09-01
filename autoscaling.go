package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/juju/loggo"

	"strings"
	"time"
)

// logging
var autoscalingLogger = loggo.GetLogger("autoscaling")

type Autoscaling struct{}

func (a *Autoscaling) newLaunchConfigFromExisting(launchConfig string) error {
	lc, err := a.getLaunchConfig(launchConfig)
	if err != nil {
		return err
	}
	return a.createLaunchConfig(launchConfig, lc)
}

func (a *Autoscaling) createLaunchConfig(launchConfig string, lc autoscaling.LaunchConfiguration) error {
	svc := autoscaling.New(session.New())

	input := &autoscaling.CreateLaunchConfigurationInput{
		AssociatePublicIpAddress:     lc.AssociatePublicIpAddress,
		BlockDeviceMappings:          lc.BlockDeviceMappings,
		ClassicLinkVPCId:             lc.ClassicLinkVPCId,
		ClassicLinkVPCSecurityGroups: lc.ClassicLinkVPCSecurityGroups,
		EbsOptimized:                 lc.EbsOptimized,
		IamInstanceProfile:           lc.IamInstanceProfile,
		ImageId:                      lc.ImageId,
		InstanceMonitoring:           lc.InstanceMonitoring,
		InstanceType:                 lc.InstanceType,
		KernelId:                     lc.KernelId,
		KeyName:                      lc.KeyName,
		LaunchConfigurationName:      aws.String(launchConfig + time.Now().UTC().Format("20060102150405999999")),
		PlacementTenancy:             lc.PlacementTenancy,
		RamdiskId:                    lc.RamdiskId,
		SecurityGroups:               lc.SecurityGroups,
		SpotPrice:                    lc.SpotPrice,
		UserData:                     lc.UserData,
	}
	autoscalingLogger.Debugf("debug: %v %v", svc, input)
	return nil
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
