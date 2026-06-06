package tools

import (
	"context"
	"testing"
)

func TestSSRFGuardRejectsInternal(t *testing.T) {
	for _, u := range []string{
		"http://127.0.0.1/admin",
		"http://169.254.169.254/latest/meta-data/",
		"http://localhost:8080/",
		"file:///etc/passwd",
		"http://10.0.0.5/",
	} {
		if err := ValidatePublicURL(u); err == nil {
			t.Errorf("expected %q to be rejected", u)
		}
	}
	// A public host must pass.
	if err := ValidatePublicURL("https://example.com"); err != nil {
		t.Errorf("public URL should pass: %v", err)
	}
}

func TestHTTPToolBlocksSSRF(t *testing.T) {
	out, err := NewHTTPTool().Execute(context.Background(), map[string]string{"url": "http://169.254.169.254/latest/meta-data/"})
	if err == nil {
		t.Fatalf("metadata endpoint should be blocked, got %q", out)
	}
}

func TestFileToolConfinement(t *testing.T) {
	t.Setenv("CYNTR_FILE_TOOL_ROOT", t.TempDir())
	// Absolute escape and ../ traversal both rejected.
	for _, p := range []string{"/etc/passwd", "../../../../etc/passwd"} {
		if _, err := (&FileReadTool{}).Execute(context.Background(), map[string]string{"path": p}); err == nil {
			t.Errorf("expected %q to be rejected", p)
		}
	}
}

func TestAWSToolRejectsInjection(t *testing.T) {
	// A non-numeric account id (injection attempt) is rejected before any exec.
	_, err := (&AWSTool{}).Execute(context.Background(), map[string]string{
		"account_id": "123;rm -rf /", "command": "aws s3 ls",
	})
	if err == nil {
		t.Fatal("expected invalid account_id to be rejected")
	}
}

func TestAWSCostRejectsBadGroupBy(t *testing.T) {
	_, err := (&CostExplorerTool{}).Execute(context.Background(), map[string]string{
		"group_by": "SERVICE; curl evil",
	})
	if err == nil {
		t.Fatal("expected invalid group_by to be rejected")
	}
}
