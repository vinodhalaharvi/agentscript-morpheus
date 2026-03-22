package cloudrun

import (
	"context"
	"fmt"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Plugin exposes Cloud Run deployment commands to the AgentScript DSL.
//
// Commands:
//
//	gcp_check                     — validate credentials + permissions
//	deploy "name" "script.as"     — build + push + create Cloud Run Job
//	schedule "name" "0 9 * * *"   — create Cloud Scheduler trigger
//	undeploy "name"               — delete job + scheduler
type Plugin struct {
	client *Client
}

// NewPlugin creates a Cloud Run plugin.
func NewPlugin(verbose bool) *Plugin {
	return &Plugin{client: NewClient(verbose)}
}

func (p *Plugin) Name() string { return "cloudrun" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"gcp_check": p.gcpCheck,
		"deploy":    p.deploy,
		"schedule":  p.schedule,
		"undeploy":  p.undeploy,
	}
}

// gcp_check — validate all GCP prerequisites before deploying
// Usage: gcp_check
func (p *Plugin) gcpCheck(ctx context.Context, args []string, input string) (string, error) {
	return p.client.CheckAll(ctx)
}

// deploy — build Docker image, push to GCR, create Cloud Run Job
// Usage: deploy "job-name" "path/to/script.as"
func (p *Plugin) deploy(ctx context.Context, args []string, input string) (string, error) {
	jobName := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if jobName == "" {
		return "", fmt.Errorf(`deploy requires a job name and script path

  Usage:
    deploy "ssl-monitor" "examples/ssl-monitor.as"

  Then schedule it:
    schedule "ssl-monitor" "0 9 * * *"`)
	}

	scriptPath, err := plugin.RequireArg(args, 1, "script path")
	if err != nil {
		return "", fmt.Errorf(`deploy requires a script path as second argument

  Usage:
    deploy "ssl-monitor" "examples/ssl-monitor.as"`)
	}

	return p.client.Deploy(ctx, jobName, scriptPath)
}

// schedule — create a Cloud Scheduler trigger for a deployed Cloud Run Job
// Usage: schedule "job-name" "0 9 * * *"
func (p *Plugin) schedule(ctx context.Context, args []string, input string) (string, error) {
	// Support piped input from deploy — extract job name from deploy output
	jobName := plugin.Arg(args, 0)
	if jobName == "" {
		// Try to extract from piped deploy output
		for _, line := range strings.Split(input, "\n") {
			if strings.HasPrefix(line, "Job name:") {
				jobName = strings.TrimSpace(strings.TrimPrefix(line, "Job name:"))
				break
			}
		}
	}
	if jobName == "" {
		return "", fmt.Errorf(`schedule requires a job name

  Usage:
    schedule "ssl-monitor" "0 9 * * *"

  Common schedules:
    "*/5 * * * *"   every 5 minutes
    "0 9 * * *"     daily at 9am UTC
    "0 9 * * MON"   every Monday at 9am UTC
    "*/15 * * * *"  every 15 minutes`)
	}

	cronExpr := plugin.Arg(args, 1)
	if cronExpr == "" {
		return "", fmt.Errorf(`schedule requires a cron expression

  Usage:
    schedule "%s" "0 9 * * *"

  Common schedules:
    "*/5 * * * *"   every 5 minutes
    "0 9 * * *"     daily at 9am UTC
    "0 9 * * MON"   every Monday at 9am UTC
    "*/15 * * * *"  every 15 minutes`, jobName)
	}

	return p.client.Schedule(ctx, jobName, cronExpr)
}

// undeploy — delete a Cloud Run Job and its Cloud Scheduler trigger
// Usage: undeploy "job-name"
func (p *Plugin) undeploy(ctx context.Context, args []string, input string) (string, error) {
	jobName := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if jobName == "" {
		return "", fmt.Errorf(`undeploy requires a job name

  Usage:
    undeploy "ssl-monitor"`)
	}
	return p.client.Undeploy(ctx, jobName)
}
