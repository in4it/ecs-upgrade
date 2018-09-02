package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/juju/loggo"
)

var ecsLogger = loggo.GetLogger("ecs")

type ECS struct{}

func (e *ECS) listContainerInstances(clusterName string) ([]string, error) {
	var instanceArns []string
	svc := ecs.New(session.New())
	input := &ecs.ListContainerInstancesInput{
		Cluster: aws.String(clusterName),
	}
	result, err := svc.ListContainerInstances(input)
	if err != nil {
		ecsLogger.Errorf("%v", err.Error())
		return instanceArns, err
	}
	for _, instance := range result.ContainerInstanceArns {
		instanceArns = append(instanceArns, aws.StringValue(instance))
	}
	return instanceArns, nil
}
func (e *ECS) describeContainerInstances(clusterName string, instanceArns []string) (map[string]string, error) {
	instances := make(map[string]string)
	svc := ecs.New(session.New())
	input := &ecs.DescribeContainerInstancesInput{
		Cluster:            aws.String(clusterName),
		ContainerInstances: aws.StringSlice(instanceArns),
	}
	result, err := svc.DescribeContainerInstances(input)
	if err != nil {
		ecsLogger.Errorf("%v", err.Error())
		return instances, err
	}
	for _, instance := range result.ContainerInstances {
		instances[aws.StringValue(instance.Ec2InstanceId)] = aws.StringValue(instance.ContainerInstanceArn)
	}
	return instances, nil
}
func (e *ECS) drainNode(clusterName, instance string) error {
	svc := ecs.New(session.New())
	input := &ecs.UpdateContainerInstancesStateInput{
		Cluster:            aws.String(clusterName),
		ContainerInstances: aws.StringSlice([]string{instance}),
		Status:             aws.String("DRAINING"),
	}
	_, err := svc.UpdateContainerInstancesState(input)
	if err != nil {
		ecsLogger.Errorf("%v", err.Error())
		return err
	}
	return nil
}
