package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

type AWSTool struct{}

func NewAWSTool() *AWSTool { return &AWSTool{} }

func (t *AWSTool) Name() string { return "aws_cross_account" }
func (t *AWSTool) Description() string {
	return "Execute AWS CLI commands across different accounts using STS AssumeRole. Provides temporary credentials for the target account."
}
func (t *AWSTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"account_id": {Type: "string", Description: "Target AWS account ID", Required: true},
		"role_name":  {Type: "string", Description: "IAM role name to assume (default: ReadOnlyAccess)", Required: false},
		"command":    {Type: "string", Description: "AWS CLI command to execute in the target account", Required: true},
		"region":     {Type: "string", Description: "AWS region (default: us-east-1)", Required: false},
	}
}

func (t *AWSTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	accountID := input["account_id"]
	roleName := input["role_name"]
	command := input["command"]
	region := input["region"]

	if accountID == "" || command == "" {
		return "", fmt.Errorf("account_id and command are required")
	}
	if roleName == "" {
		roleName = "ReadOnlyAccess"
	}
	if region == "" {
		region = "us-east-1"
	}

	roleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", accountID, roleName)

	// Assume role
	assumeCmd := fmt.Sprintf("aws sts assume-role --role-arn %s --role-session-name cyntr-session --output json", roleARN)

	assumeCtx, assumeCancel := context.WithTimeout(ctx, 30*time.Second)
	defer assumeCancel()

	cmd := exec.CommandContext(assumeCtx, "bash", "-c", assumeCmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("assume role failed: %s %s", err.Error(), stderr.String())
	}

	var creds struct {
		Credentials struct {
			AccessKeyId     string
			SecretAccessKey string
			SessionToken    string
		}
	}
	if err := json.Unmarshal(stdout.Bytes(), &creds); err != nil {
		return "", fmt.Errorf("parse credentials: %w", err)
	}

	// Execute command with assumed role credentials
	envPrefix := fmt.Sprintf("AWS_ACCESS_KEY_ID=%s AWS_SECRET_ACCESS_KEY=%s AWS_SESSION_TOKEN=%s AWS_DEFAULT_REGION=%s",
		creds.Credentials.AccessKeyId, creds.Credentials.SecretAccessKey, creds.Credentials.SessionToken, region)

	fullCmd := fmt.Sprintf("%s %s", envPrefix, command)

	execCtx, execCancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer execCancel()

	execCmd := exec.CommandContext(execCtx, "bash", "-c", fullCmd)
	var execOut, execErr bytes.Buffer
	execCmd.Stdout = &execOut
	execCmd.Stderr = &execErr

	if err := execCmd.Run(); err != nil {
		output := execOut.String() + execErr.String()
		return output, fmt.Errorf("command failed: %w", err)
	}

	output := execOut.String()
	if execErr.Len() > 0 {
		output += "\n" + execErr.String()
	}
	if len(output) > 65536 {
		output = output[:65536] + "\n... (truncated)"
	}
	return output, nil
}
