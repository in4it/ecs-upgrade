package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/juju/loggo"
)

// logging
var lbLogger = loggo.GetLogger("lb")

type LB struct{}

func (l *LB) getTargets() ([]string, error) {
	var targets []string

	svc := elbv2.New(session.New())
	input := &elbv2.DescribeTargetGroupsInput{}

	pageNum := 0
	err := svc.DescribeTargetGroupsPages(input,
		func(page *elbv2.DescribeTargetGroupsOutput, lastPage bool) bool {
			pageNum++
			for _, target := range page.TargetGroups {
				targets = append(targets, aws.StringValue(target.TargetGroupArn))
			}
			return pageNum <= 20
		})

	if err != nil {
		autoscalingLogger.Errorf(err.Error())
	}

	return targets, nil
}
func (l *LB) getTargetHealth(targetGroupArn string) (map[string]string, error) {
	targetHealth := make(map[string]string)
	svc := elbv2.New(session.New())
	input := &elbv2.DescribeTargetHealthInput{
		TargetGroupArn: aws.String(targetGroupArn),
	}
	result, err := svc.DescribeTargetHealth(input)
	if err != nil {
		lbLogger.Errorf("%v", err.Error())
		return targetHealth, err
	}
	for _, target := range result.TargetHealthDescriptions {
		targetHealth[aws.StringValue(target.Target.Id)] = aws.StringValue(target.TargetHealth.State)
	}
	return targetHealth, nil
}
