package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

var (
	awsAccountRe = regexp.MustCompile(`^[0-9]{12}$`)
	awsRoleRe    = regexp.MustCompile(`^[A-Za-z0-9+=,.@_/-]{1,128}$`)
	awsRegionRe  = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)
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

	// Validate the identifiers that go into the role ARN. Without this they are
	// interpolated into a shell command — a command-injection vector.
	if !awsAccountRe.MatchString(accountID) {
		return "", fmt.Errorf("invalid account_id (want 12 digits)")
	}
	if !awsRoleRe.MatchString(roleName) {
		return "", fmt.Errorf("invalid role_name")
	}
	if !awsRegionRe.MatchString(region) {
		return "", fmt.Errorf("invalid region")
	}

	roleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", accountID, roleName)

	assumeCtx, assumeCancel := context.WithTimeout(ctx, 30*time.Second)
	defer assumeCancel()

	// Assume role via discrete argv (no shell) so the ARN cannot inject.
	cmd := exec.CommandContext(assumeCtx, "aws", "sts", "assume-role",
		"--role-arn", roleARN, "--role-session-name", "cyntr-session", "--output", "json")
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

	execCtx, execCancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer execCancel()

	// Run the operator-supplied AWS CLI command with the assumed-role
	// credentials passed via the environment (not string-interpolated into the
	// shell, which previously let the session token break out of the command).
	execCmd := exec.CommandContext(execCtx, "bash", "-c", command)
	execCmd.Env = append(execCmd.Environ(),
		"AWS_ACCESS_KEY_ID="+creds.Credentials.AccessKeyId,
		"AWS_SECRET_ACCESS_KEY="+creds.Credentials.SecretAccessKey,
		"AWS_SESSION_TOKEN="+creds.Credentials.SessionToken,
		"AWS_DEFAULT_REGION="+region,
	)
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
