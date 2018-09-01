package main

import (
	"github.com/juju/loggo"

	"fmt"
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
	launchConfig := os.Getenv("ECS_LAUNCHCONFIG")
	if len(launchConfig) == 0 {
		fmt.Printf("ECS_LAUNCHCONFIG not set\n")
		return 1
	}
	asg := os.Getenv("ECS_ASG")
	if len(asg) == 0 {
		fmt.Printf("ECS_ASG not set\n")
		return 1
	}
	a := Autoscaling{}
	// create new launch config
	newLaunchConfigName, err := a.newLaunchConfigFromExisting(launchConfig)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return 1
	}
	if newLaunchConfigName == "" {
		return 0
	}
	// update autoscaling group
	err = a.updateAutoscalingLaunchConfig(asg, newLaunchConfigName)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return 1
	}
	// get asg size
	_, desired, _, err := a.getAutoscalingGroupSize(asg)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return 1
	}
	// scale
	err = a.scaleAutoscalingGroup(asg, desired*2)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return 1
	}
	// wait until new instances are healthy
	var healthy bool
	var waited int64
	for i := 0; !healthy && i < 25; i++ {
		instances, err := a.getAutoscalingInstanceHealth(asg)
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
		if healthyInstances >= desired {
			healthy = true
		} else {
			mainLogger.Debugf("Checking autoscaling instances health: Waiting 30s")
			time.Sleep(30 * time.Second)
			waited += 30
		}
	}
	// wait for cooldown period
	// scale down
	err = a.scaleAutoscalingGroup(asg, desired)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return 1
	}
	// delete old launchconfig

	return 0
}
