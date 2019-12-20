package main

import (
	"strings"

	"github.com/juju/loggo"

	"fmt"
	"math"
	"os"
	"time"
)

// logging
var mainLogger = loggo.GetLogger("ecs-upgrade")

func main() {
	if os.Getenv("DEBUG") == "true" {
		loggo.ConfigureLoggers(`<root>=DEBUG`)
	} else {
		loggo.ConfigureLoggers(`<root>=INFO`)
	}
	os.Exit(mainWithReturnCode())
}

func mainWithReturnCode() int {
	e := ECS{}
	var err error
	asgName := os.Getenv("ECS_ASG")
	if len(asgName) == 0 {
		fmt.Printf("ECS_ASG not set\n")
		return 1
	}
	clusterName := os.Getenv("ECS_CLUSTER")
	if len(clusterName) == 0 {
		fmt.Printf("ECS_CLUSTER not set\n")
		return 1
	}
	useLaunchTemplates := os.Getenv("LAUNCH_TEMPLATES")
	a := Autoscaling{}
	// get asg
	asg, err := a.describeAutoscalingGroup(asgName)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return 1
	}
	var newLaunchIdentifier string
	if useLaunchTemplates == "true" {
		newLaunchIdentifier, err = scaleWithLaunchTemplate(asg)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return 1
		}
	} else {
		newLaunchIdentifier, err = scaleWithLaunchConfig(asg)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return 1
		}
	}
	if newLaunchIdentifier == "" {
		fmt.Printf("Launch configuration is already at latest version")
		return 0
	}
	// wait until new instances are healthy
	var healthy bool
	var instances []AutoscalingInstance
	for i := 0; !healthy && i < 25; i++ {
		instances, err = a.getAutoscalingInstanceHealth(asgName)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return 1
		}
		var healthyInstances int64
		for _, instance := range instances {
			if checkInstanceLaunchConfigOrTemplate(useLaunchTemplates, instance, newLaunchIdentifier) {
				if instance.HealthStatus == "HEALTHY" {
					healthyInstances += 1
				} else {
					mainLogger.Debugf("Waiting for intance %s to become healthy (currently %s)", instance.InstanceId, instance.HealthStatus)
				}
			}
		}
		if healthyInstances >= asg.DesiredCapacity {
			healthy = true
		} else {
			mainLogger.Debugf("Checking autoscaling instances health: Waiting 30s")
			time.Sleep(30 * time.Second)
		}
	}
	// wait for new nodes to attach
	err = e.waitForNewNodes(clusterName, len(instances))
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return 1
	}
	// drain
	mainLogger.Debugf("Draining instances")
	drainedContainerArns, err := drain(clusterName, instances, newLaunchIdentifier, useLaunchTemplates)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return 1
	}
	// wait until nodes are drained
	mainLogger.Debugf("Wait for Drained instances")
	err = e.waitForDrainedNode(clusterName, drainedContainerArns)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return 1
	}
	// check target health
	mainLogger.Debugf("Checking targets health")
	err = checkTargetHealth(asgName, newLaunchIdentifier, useLaunchTemplates, clusterName)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return 1
	}
	// scale down
	mainLogger.Debugf("Scaling down")
	err = a.scaleAutoscalingGroup(asgName, asg.DesiredCapacity)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return 1
	}
	// delete old launchconfig
	if useLaunchTemplates != "true" {
		err = a.deleteLaunchConfig(asg.LaunchConfigurationName)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return 1
		}
	}

	fmt.Printf("Upgrade completed\n")
	return 0
}

func drain(clusterName string, instances []AutoscalingInstance, newLaunchIdentifier string, useLaunchTemplates string) ([]string, error) {
	var drainedContainerArns []string
	var instancesToDrain []string
	e := ECS{}
	for _, instance := range instances {
		if !checkInstanceLaunchConfigOrTemplate(useLaunchTemplates, instance, newLaunchIdentifier) {
			instancesToDrain = append(instancesToDrain, instance.InstanceId)
			mainLogger.Debugf("Going to drain %s", instance.InstanceId)
		}
	}
	if float64(len(instancesToDrain)) > math.Ceil(float64(len(instances)/2)) {
		return drainedContainerArns, fmt.Errorf("Going to drain %d instances out of %d, which is more than 50%", len(instancesToDrain), len(instances))
	}
	containerInstanceArns, err := e.listContainerInstances(clusterName)
	if err != nil {
		return drainedContainerArns, err
	}
	containerInstances, err := e.describeContainerInstances(clusterName, containerInstanceArns)
	if err != nil {
		return drainedContainerArns, err
	}
	for _, instanceId := range instancesToDrain {
		if containerId, ok := containerInstances[instanceId]; ok {
			err = e.drainNode(clusterName, containerId)
			if err != nil {
				return drainedContainerArns, err
			}
			drainedContainerArns = append(drainedContainerArns, containerId)
		} else {
			return drainedContainerArns, fmt.Errorf("Couldn't drain instance %s", instanceId)
		}
	}
	return drainedContainerArns, nil
}
func checkTargetHealth(asgName, newLaunchIdentifier, useLaunchTemplates, clusterName string) error {
	lb := LB{}
	a := Autoscaling{}
	e := ECS{}
	targetGroups, err := lb.getTargets()
	if err != nil {
		return err
	}
	var allHealthy bool

	// get container instances
	containerInstanceArns, err := e.listContainerInstances(clusterName)
	if err != nil {
		return err
	}
	containerInstances, err := e.describeContainerInstancesReverseMap(clusterName, containerInstanceArns)
	if err != nil {
		return err
	}

	for i := 0; !allHealthy && i < 25; i++ {
		// refresh instances
		instances, err := a.getAutoscalingInstanceHealth(asgName)
		if err != nil {
			return err
		}

		// get tasks
		tasks, err := e.ListTasks(clusterName, "RUNNING")
		if err != nil {
			return err
		}
		IPsPerContainerInstance, err := e.getTaskIPsPerContainerInstance(clusterName, tasks)

		// print instances
		for _, instance := range instances {
			if checkInstanceLaunchConfigOrTemplate(useLaunchTemplates, instance, newLaunchIdentifier) {
				instanceIPList := IPsPerContainerInstance[containerInstances[instance.InstanceId]]
				autoscalingLogger.Debugf("checkTargetHealth: retrieved instance %s with IPs (%s) and AWSVPC IPs (%s)", instance.InstanceId, strings.Join(instance.IPs, ","), strings.Join(instanceIPList, ","))
			}
		}

		// check health
		var unhealthy, healthy int64
		for _, targetGroup := range targetGroups {
			targetsHealth, err := lb.getTargetHealth(targetGroup)
			if err != nil {
				return err
			}
			for id, targetHealth := range targetsHealth {
				for _, instance := range instances {
					// id without awsvpc is instanceID, id with awsVPC is IP address. Let's compare both
					instanceIPList := IPsPerContainerInstance[containerInstances[instance.InstanceId]]
					if (instance.InstanceId == id || stringInSlice(id, instance.IPs) || stringInSlice(id, instanceIPList)) && checkInstanceLaunchConfigOrTemplate(useLaunchTemplates, instance, newLaunchIdentifier) {
						mainLogger.Debugf("Found instance %s in target group %s with health %s", id, targetGroup, targetHealth)
						if targetHealth == "healthy" {
							healthy++
						} else {
							unhealthy++
						}
					}
				}
			}
		}
		if healthy > 0 && unhealthy == 0 {
			mainLogger.Debugf("All instances of target groups are healthy")
			allHealthy = true
		} else {
			mainLogger.Debugf("Checking loadbalancer target instances health: Waiting 30s (healthy: %d, unhealthy: %d)", healthy, unhealthy)
			time.Sleep(30 * time.Second)
		}
	}
	return nil
}

func scaleWithLaunchConfig(asg AutoscalingGroup) (string, error) {
	a := Autoscaling{}

	// create new launch config
	newLaunchConfigName, err := a.newLaunchConfigFromExisting(asg.LaunchConfigurationName)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return "", err
	}
	if newLaunchConfigName == "" {
		return "", fmt.Errorf("New Launch config name is empty (previous launch config name: %s)", asg.LaunchConfigurationName)
	}
	// update autoscaling group
	err = a.updateAutoscalingLaunchConfig(asg.AutoscalingGroupName, newLaunchConfigName)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return "", err
	}
	// scale
	err = a.scaleAutoscalingGroup(asg.AutoscalingGroupName, asg.DesiredCapacity*2)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return "", err
	}
	return newLaunchConfigName, nil
}
func scaleWithLaunchTemplate(asg AutoscalingGroup) (string, error) {
	a := Autoscaling{}

	// create new launch config
	_, newLaunchTemplateName, newLaunchTemplateVersion, err := a.newLaunchTemplateVersion(asg.LaunchTemplateName)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return "", err
	}
	if newLaunchTemplateName == "" {
		return "", nil
	}
	// update autoscaling group
	err = a.updateAutoscalingLaunchTemplate(asg.AutoscalingGroupName, newLaunchTemplateName, newLaunchTemplateVersion)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return "", err
	}
	// scale
	err = a.scaleAutoscalingGroup(asg.AutoscalingGroupName, asg.DesiredCapacity*2)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return "", err
	}
	return newLaunchTemplateName + ":" + newLaunchTemplateVersion, nil
}

func checkInstanceLaunchConfigOrTemplate(useLaunchTemplates string, instance AutoscalingInstance, newName string) bool {
	if useLaunchTemplates == "true" {
		s := strings.Split(newName, ":")
		if len(s) != 2 {
			return false
		}
		if instance.LaunchTemplateName == s[0] && instance.LaunchTemplateVersion == s[1] {
			return true
		}
	} else {
		if instance.LaunchConfig == newName {
			return true
		}
	}
	return false

}
func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}
