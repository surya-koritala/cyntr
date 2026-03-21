package tools

import (
	"context"
	"testing"
)

func TestAWSToolName(t *testing.T) {
	tool := NewAWSTool()
	if tool.Name() != "aws_cross_account" {
		t.Fatalf("expected aws_cross_account, got %q", tool.Name())
	}
}

func TestAWSToolDescription(t *testing.T) {
	tool := NewAWSTool()
	if tool.Description() == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestAWSToolParameters(t *testing.T) {
	tool := NewAWSTool()
	params := tool.Parameters()
	if _, ok := params["account_id"]; !ok {
		t.Fatal("missing account_id parameter")
	}
	if _, ok := params["command"]; !ok {
		t.Fatal("missing command parameter")
	}
	if !params["account_id"].Required {
		t.Fatal("account_id should be required")
	}
	if !params["command"].Required {
		t.Fatal("command should be required")
	}
}

func TestAWSToolMissingParams(t *testing.T) {
	tool := NewAWSTool()
	_, err := tool.Execute(context.Background(), map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing params")
	}
}

func TestAWSToolMissingAccountID(t *testing.T) {
	tool := NewAWSTool()
	_, err := tool.Execute(context.Background(), map[string]string{"command": "aws s3 ls"})
	if err == nil {
		t.Fatal("expected error for missing account_id")
	}
}

func TestAWSToolMissingCommand(t *testing.T) {
	tool := NewAWSTool()
	_, err := tool.Execute(context.Background(), map[string]string{"account_id": "123456789012"})
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestCostExplorerToolName(t *testing.T) {
	tool := NewCostExplorerTool()
	if tool.Name() != "aws_cost_explorer" {
		t.Fatalf("expected aws_cost_explorer, got %q", tool.Name())
	}
}

func TestCostExplorerToolDescription(t *testing.T) {
	tool := NewCostExplorerTool()
	if tool.Description() == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestCostExplorerToolParameters(t *testing.T) {
	tool := NewCostExplorerTool()
	params := tool.Parameters()
	if _, ok := params["period"]; !ok {
		t.Fatal("missing period parameter")
	}
	if _, ok := params["group_by"]; !ok {
		t.Fatal("missing group_by parameter")
	}
}
