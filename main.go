package main

import (
	"fmt"
	"github.com/juju/loggo"
	"os"
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
	a := Autoscaling{}
	err := a.newLaunchConfigFromExisting(launchConfig)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return 1
	}
	return 0
}
