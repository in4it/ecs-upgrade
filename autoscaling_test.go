package main

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
)

type autoscalingMock struct {
	autoscalingiface.AutoScalingAPI
	DescribeAutoScalingGroupsOutput    *autoscaling.DescribeAutoScalingGroupsOutput
	DescribeAutoScalingInstancesOutput *autoscaling.DescribeAutoScalingInstancesOutput
}

type ec2Mock struct {
	ec2iface.EC2API
	DescribeInstancesOutput *ec2.DescribeInstancesOutput
}

func (a autoscalingMock) DescribeAutoScalingGroups(*autoscaling.DescribeAutoScalingGroupsInput) (*autoscaling.DescribeAutoScalingGroupsOutput, error) {
	return a.DescribeAutoScalingGroupsOutput, nil
}

func (a autoscalingMock) DescribeAutoScalingInstancesPages(instances *autoscaling.DescribeAutoScalingInstancesInput, f func(*autoscaling.DescribeAutoScalingInstancesOutput, bool) bool) error {
	if len(instances.InstanceIds) > 50 {
		return fmt.Errorf("ValidationError: The number of instance ids that may be passed in is limited to 50")
	}
	instancesToReturn := []*autoscaling.InstanceDetails{}
	for _, v := range a.DescribeAutoScalingInstancesOutput.AutoScalingInstances {
		for _, v2 := range instances.InstanceIds {
			if *v2 == *v.InstanceId {
				instancesToReturn = append(instancesToReturn, v)
			}
		}
	}
	f(&autoscaling.DescribeAutoScalingInstancesOutput{
		AutoScalingInstances: instancesToReturn,
	}, false)
	return nil
}
func (e ec2Mock) DescribeInstancesPages(input *ec2.DescribeInstancesInput, f func(page *ec2.DescribeInstancesOutput, lastPage bool) bool) error {
	f(e.DescribeInstancesOutput, false)
	return nil
}

func TestGetAutoscalingInstanceHealth(t *testing.T) {
	for _, maxSize := range []int{40, 50, 100} { // test with 40 and 100 items
		instances := make([]*autoscaling.Instance, maxSize)
		instanceDetails := make([]*autoscaling.InstanceDetails, maxSize)
		ec2Instances := make([]*ec2.Instance, maxSize)
		for i := 0; i < maxSize; i++ {
			instances[i] = &autoscaling.Instance{
				InstanceId: aws.String("i-" + strconv.Itoa(i)),
			}
		}
		for i := 0; i < maxSize; i++ {
			instanceDetails[i] = &autoscaling.InstanceDetails{
				InstanceId:              aws.String("i-" + strconv.Itoa(i)),
				LaunchConfigurationName: aws.String("launchconfig"),
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
					LaunchTemplateName: aws.String("launchTemplateName"),
					Version:            aws.String("1"),
				},
				HealthStatus: aws.String("Healthy"),
			}
		}
		for i := 0; i < maxSize; i++ {
			ec2Instances[i] = &ec2.Instance{
				InstanceId:       aws.String("i-" + strconv.Itoa(i)),
				PrivateIpAddress: aws.String("10.0.0." + strconv.Itoa(i)),
			}
		}
		ec2Mock := ec2Mock{
			DescribeInstancesOutput: &ec2.DescribeInstancesOutput{
				Reservations: []*ec2.Reservation{
					{
						Instances: ec2Instances,
					},
				},
			},
		}
		autoscalingMock := autoscalingMock{
			DescribeAutoScalingGroupsOutput: &autoscaling.DescribeAutoScalingGroupsOutput{
				AutoScalingGroups: []*autoscaling.Group{
					{
						Instances: instances,
					},
				},
			},
			DescribeAutoScalingInstancesOutput: &autoscaling.DescribeAutoScalingInstancesOutput{
				AutoScalingInstances: instanceDetails,
			},
		}
		a := Autoscaling{
			svcAutoscaling: autoscalingMock,
			svcEC2:         ec2Mock,
		}
		res, err := a.getAutoscalingInstanceHealth("asg-test")
		if err != nil {
			t.Errorf("getAutoscalingInstanceHealth error: %s", err)
			return
		}
		if len(res) == 0 {
			t.Errorf("result was empty")
		}
		if len(res) != maxSize {
			t.Errorf("result was not equal to 100 (got %d)", len(res))
		}
	}
}
