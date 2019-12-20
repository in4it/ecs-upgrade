package main

import (
	"math"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecs"
	ecslib "github.com/in4it/ecs-deploy/provider/ecs"
	"github.com/juju/loggo"

	"time"
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

func (e *ECS) describeContainerInstancesReverseMap(clusterName string, instanceArns []string) (map[string]string, error) {
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
		instances[aws.StringValue(instance.ContainerInstanceArn)] = aws.StringValue(instance.Ec2InstanceId)
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
func (e *ECS) waitForDrainedNode(clusterName string, drainedContainerArns []string) error {
	var tasksDrained bool
	ecsLib := ecslib.ECS{}
	for i := 0; i < 80 && !tasksDrained; i++ {
		cis, err := ecsLib.DescribeContainerInstances(clusterName, drainedContainerArns)
		if err != nil || len(cis) == 0 {
			ecsLogger.Errorf("waitForDrainedNode: %v", err.Error())
			return err
		}
		var runningTasksCount int64
		for _, ci := range cis {
			runningTasksCount += ci.RunningTasksCount
		}
		if runningTasksCount == 0 {
			tasksDrained = true
		} else {
			ecsLogger.Infof("launchWaitForDrainedNode(s): still %d tasks running", runningTasksCount)
		}
		time.Sleep(15 * time.Second)
	}
	if !tasksDrained {
		ecsLogger.Errorf("waitForDrainedNode(s): Not able to drain tasks: timeout of 20m reached")
	}
	ecsLogger.Infof("waitForDrainedNode(s): Node drained, completed lifecycle action")
	return nil
}
func (e *ECS) waitForNewNodes(clusterName string, asgInstancesCount int) error {
	var newInstancesOnline bool
	var containerInstanceArns []string
	var err error
	// waiting for new nodes to come online
	for i := 0; i < 80 && !newInstancesOnline; i++ {
		containerInstanceArns, err = e.listContainerInstances(clusterName)
		if err != nil {
			ecsLogger.Errorf("waitNewnodes: %v", err.Error())
			return err
		}
		if len(containerInstanceArns) == asgInstancesCount {
			ecsLogger.Debugf("waitForNewNodes: new instances online")
			newInstancesOnline = true
		} else {
			ecsLogger.Debugf("waitForNewNodes: waiting for instances to come online: sleeping 15s (%d/%d online)", len(containerInstanceArns), asgInstancesCount)
			time.Sleep(15 * time.Second)
		}
	}
	// waiting for new nodes to have ACTIVE status
	ecsLib := ecslib.ECS{}
	var newInstancesActive bool
	for i := 0; i < 80 && !newInstancesActive; i++ {
		cis, err := ecsLib.DescribeContainerInstances(clusterName, containerInstanceArns)
		if err != nil || len(cis) == 0 {
			ecsLogger.Errorf("waitForNewNodes: %v", err.Error())
			return err
		}
		var notActive int64
		for _, ci := range cis {
			if ci.Status != "ACTIVE" {
				notActive++
			}
		}
		if notActive == 0 {
			ecsLogger.Debugf("waitForNewNodes: All nodes have ACTIVE status")
			newInstancesActive = true
		} else {
			ecsLogger.Debugf("waitForNewNodes: New nodes online, but not active: sleeping 15s (%d not active)", notActive)
			time.Sleep(15 * time.Second)
		}
	}
	return nil
}

func (e *ECS) ListTasks(clusterName, desiredStatus string) ([]string, error) {
	svc := ecs.New(session.New())
	var tasks []*string

	input := &ecs.ListTasksInput{
		Cluster:       aws.String(clusterName),
		DesiredStatus: aws.String(desiredStatus),
	}

	pageNum := 0
	err := svc.ListTasksPages(input,
		func(page *ecs.ListTasksOutput, lastPage bool) bool {
			pageNum++
			tasks = append(tasks, page.TaskArns...)
			return pageNum <= 100
		})

	if err != nil {
		ecsLogger.Errorf(err.Error())
	}
	return aws.StringValueSlice(tasks), err
}

func (e *ECS) getTaskIPsPerContainerInstance(clusterName string, tasks []string) (map[string][]string, error) {

	result := make(map[string][]string)
	svc := ecs.New(session.New())

	// fetch per 100
	var y float64 = float64(len(tasks)) / 100
	for i := 0; i < int(math.Ceil(y)); i++ {

		f := i * 100
		t := int(math.Min(float64(100+100*i), float64(len(tasks))))

		input := &ecs.DescribeTasksInput{
			Cluster: aws.String(clusterName),
			Tasks:   aws.StringSlice(tasks[f:t]),
		}

		tasks, err := svc.DescribeTasks(input)
		if err != nil {
			ecsLogger.Errorf(err.Error())
			return result, err
		}
		for _, task := range tasks.Tasks {
			for _, attachment := range task.Attachments {
				for _, detail := range attachment.Details {
					if aws.StringValue(detail.Name) == "privateIPv4Address" {
						result[aws.StringValue(task.ContainerInstanceArn)] = append(result[aws.StringValue(task.ContainerInstanceArn)], aws.StringValue(detail.Value))
					}

				}
			}

		}
	}
	return result, nil
}
