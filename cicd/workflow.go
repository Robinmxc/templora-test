package cicd

import (
	"time"

	"github.com/pkg/errors"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// CDWorkflow 定义 CI/CD 工作流
func CDWorkflow(ctx workflow.Context, repoURL string, commitHash string) (bool, error) {
	// 重试策略
	activityRetryPolicy := &temporal.RetryPolicy{
		MaximumAttempts: 3,
	}

	// 1. 构建镜像
	var buildResult BuildResult
	err := workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: time.Minute * 10,
			RetryPolicy:         activityRetryPolicy,
		}),
		BuildDockerImage,
		repoURL, commitHash,
	).Get(ctx, &buildResult)
	if err != nil {
		workflow.ExecuteActivity(ctx, CleanupFailedDeployment).Get(ctx, nil) // 清理资源
		return false, err
	}

	// 2. 运行测试
	var testPassed bool
	err = workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: time.Minute * 15,
		}),
		RunTests,
		buildResult.ImageID,
	).Get(ctx, &testPassed)
	if !testPassed {
		return false, errors.Errorf("tests failed")
	}

	// 3. 等待人工审批（通过 Signal）
	var approvalSignal string
	signalChan := workflow.GetSignalChannel(ctx, "approval_signal")
	selector := workflow.NewSelector(ctx)
	selector.AddReceive(signalChan, func(c workflow.ReceiveChannel, _ bool) {
		c.Receive(ctx, &approvalSignal)
	})
	selector.Select(ctx) // 阻塞直到收到信号

	if approvalSignal != "approved" {
		return false, errors.Errorf("deployment rejected by manual approval")
	}

	// 4. 蓝绿部署
	var deploymentID string
	err = workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: time.Minute * 30,
		}),
		DeployToProd,
		buildResult.ImageID,
	).Get(ctx, &deploymentID)
	if err != nil {
		return false, err
	}

	// 5. 监控生产环境（带心跳检测）
	monitorOptions := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 2,
		HeartbeatTimeout:    time.Second * 30,
	})
	var isHealthy bool
	err = workflow.ExecuteActivity(
		monitorOptions,
		MonitorProduction,
		deploymentID,
	).Get(ctx, &isHealthy)
	if !isHealthy {
		workflow.ExecuteActivity(ctx, RollbackDeployment, deploymentID).Get(ctx, nil)
		return false, errors.Errorf("production health check failed")
	}

	return true, nil // 部署成功
}
