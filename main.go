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
	asgName := os.Getenv("ECS_ASG")
	if len(asgName) == 0 {
		fmt.Printf("ECS_ASG not set\n")
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
