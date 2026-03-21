package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

type CostExplorerTool struct{}

func NewCostExplorerTool() *CostExplorerTool { return &CostExplorerTool{} }

func (t *CostExplorerTool) Name() string { return "aws_cost_explorer" }
func (t *CostExplorerTool) Description() string {
	return "Query AWS Cost Explorer for spend analysis. Returns cost breakdown by service or region."
}
func (t *CostExplorerTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"period":   {Type: "string", Description: "Time period: last_7_days, last_30_days, this_month, last_month", Required: false},
		"group_by": {Type: "string", Description: "Group by: SERVICE, REGION, LINKED_ACCOUNT", Required: false},
	}
}

func (t *CostExplorerTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	period := input["period"]
	groupBy := input["group_by"]

	if period == "" {
		period = "last_7_days"
	}
	if groupBy == "" {
		groupBy = "SERVICE"
	}

	now := time.Now()
	var startDate, endDate string

	switch period {
	case "last_7_days":
		startDate = now.AddDate(0, 0, -7).Format("2006-01-02")
		endDate = now.Format("2006-01-02")
	case "last_30_days":
		startDate = now.AddDate(0, 0, -30).Format("2006-01-02")
		endDate = now.Format("2006-01-02")
	case "this_month":
		startDate = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
		endDate = now.Format("2006-01-02")
	case "last_month":
		lastMonth := now.AddDate(0, -1, 0)
		startDate = time.Date(lastMonth.Year(), lastMonth.Month(), 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
		endDate = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
	default:
		startDate = now.AddDate(0, 0, -7).Format("2006-01-02")
		endDate = now.Format("2006-01-02")
	}

	cmd := fmt.Sprintf(`aws ce get-cost-and-usage --time-period Start=%s,End=%s --granularity MONTHLY --metrics "UnblendedCost" --group-by Type=DIMENSION,Key=%s --output json`, startDate, endDate, groupBy)

	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	execCmd := exec.CommandContext(execCtx, "bash", "-c", cmd)
	var stdout, stderr bytes.Buffer
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr

	if err := execCmd.Run(); err != nil {
		return stdout.String() + stderr.String(), fmt.Errorf("cost explorer: %w", err)
	}

	output := stdout.String()
	if len(output) > 65536 {
		output = output[:65536] + "\n... (truncated)"
	}
	return output, nil
}
