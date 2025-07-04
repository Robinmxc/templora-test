package main

import (
	"log"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/Robinmxc/templora-test/cicd"
)

func main() {
	// The client and worker are heavyweight objects that should be created once per process.
	c, err := client.Dial(client.Options{
		HostPort:  "10.68.91.64:7233",
		Namespace: "default",
	})
	if err != nil {
		log.Fatalln("Unable to create client", err)
	}
	defer c.Close()

	// 创建 Worker
	w := worker.New(c, "ci-cd-queue", worker.Options{})
	w.RegisterWorkflow(cicd.CDWorkflow)
	w.RegisterActivity(cicd.BuildDockerImage)
	w.RegisterActivity(cicd.CleanupFailedDeployment)
	w.RegisterActivity(cicd.RunTests)
	w.RegisterActivity(cicd.DeployToProd)
	w.RegisterActivity(cicd.MonitorProduction)
	w.RegisterActivity(cicd.RollbackDeployment)

	err = w.Run(worker.InterruptCh())
	if err != nil {
		log.Fatalln("Unable to start worker", err)
	}
}
