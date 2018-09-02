package main

import (
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
	a := Autoscaling{}
	// get asg
	asg, err := a.describeAutoscalingGroup(asgName)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return 1
	}
	// create new launch config
	newLaunchConfigName, err := a.newLaunchConfigFromExisting(asg.LaunchConfigurationName)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return 1
	}
	if newLaunchConfigName == "" {
		return 0
	}
	// update autoscaling group
	err = a.updateAutoscalingLaunchConfig(asgName, newLaunchConfigName)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return 1
	}
	// scale
	err = a.scaleAutoscalingGroup(asgName, asg.DesiredCapacity*2)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return 1
	}
	// wait until new instances are healthy
	var healthy bool
	var waited int64
	var instances []AutoscalingInstance
	for i := 0; !healthy && i < 25; i++ {
		instances, err := a.getAutoscalingInstanceHealth(asgName)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return 1
		}
		var healthyInstances int64
		for _, instance := range instances {
			if instance.LaunchConfig == newLaunchConfigName {
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
			waited += 30
		}
	}
	// drain
	err = drain(clusterName, instances, newLaunchConfigName)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return 1
	}
	// check target health
	err = checkTargetHealth(instances, newLaunchConfigName)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return 1
	}
	// wait for cooldown period
	// scale down
	err = a.scaleAutoscalingGroup(asgName, asg.DesiredCapacity)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return 1
	}
	// delete old launchconfig

	return 0
}

func drain(clusterName string, instances []AutoscalingInstance, newLaunchConfig string) error {
	var instancesToDrain []string
	e := ECS{}
	for _, instance := range instances {
		if instance.LaunchConfig != newLaunchConfig {
			instancesToDrain = append(instancesToDrain, instance.InstanceId)
		}
	}
	if float64(len(instancesToDrain)) > math.Ceil(float64(len(instances)/2)) {
		return fmt.Errorf("Going to drain %d instances out of %d, which is more than 50%", len(instancesToDrain), len(instances))
	}
	containerInstanceArns, err := e.listContainerInstances(clusterName)
	if err != nil {
		return err
	}
	containerInstances, err := e.describeContainerInstances(clusterName, containerInstanceArns)
	if err != nil {
		return err
	}
	for _, instanceId := range instancesToDrain {
		if containerId, ok := containerInstances[instanceId]; ok {
			err = e.drainNode(clusterName, containerId)
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("Couldn't drain instance %s", instanceId)
		}
	}
	return nil
}
func checkTargetHealth(instances []AutoscalingInstance, newLaunchConfig string) error {
	lb := LB{}
	targetGroups, err := lb.getTargets()
	if err != nil {
		return err
	}
	var allHealthy bool
	for i := 0; !allHealthy && i < 25; i++ {
		var unhealthy, healthy int64
		for _, targetGroup := range targetGroups {
			targetsHealth, err := lb.getTargetHealth(targetGroup)
			if err != nil {
				return err
			}
			for instanceId, targetHealth := range targetsHealth {
				for _, instance := range instances {
					if instance.InstanceId == instanceId && instance.LaunchConfig == newLaunchConfig {
						mainLogger.Debugf("Found instance %s in target group %s with health %s", instanceId, targetGroup, targetHealth)
						if targetHealth == "healthy" {
							healthy += 1
						} else {
							unhealthy += 1
						}
					}
				}
			}
		}
		if (healthy - unhealthy) == 0 {
			allHealthy = true
		} else {
			mainLogger.Debugf("Checking loadbalancer target instances health: Waiting 30s (healthy: %d, unhealthy: %d", healthy, unhealthy)
			time.Sleep(30 * time.Second)
		}
	}
	return nil
}
