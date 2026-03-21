package tools

import (
	"context"
	"os/exec"
	"testing"
)

func TestCodeInterpreterName(t *testing.T) {
	if NewCodeInterpreterTool().Name() != "code_interpreter" {
		t.Fatal()
	}
}

func TestCodeInterpreterPython(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	tool := NewCodeInterpreterTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"language": "python", "code": "print(2 + 2)",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !containsStr(result, "4") {
		t.Fatalf("got %q", result)
	}
}

func TestCodeInterpreterPythonMultiline(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	tool := NewCodeInterpreterTool()
	code := `
data = [1, 2, 3, 4, 5]
total = sum(data)
avg = total / len(data)
print(f"Sum: {total}, Average: {avg}")
`
	result, _ := tool.Execute(context.Background(), map[string]string{"language": "python", "code": code})
	if !containsStr(result, "Sum: 15") {
		t.Fatalf("got %q", result)
	}
	if !containsStr(result, "Average: 3.0") {
		t.Fatalf("got %q", result)
	}
}

func TestCodeInterpreterJavaScript(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	tool := NewCodeInterpreterTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"language": "javascript", "code": "console.log(JSON.stringify({result: 42}))",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !containsStr(result, "42") {
		t.Fatalf("got %q", result)
	}
}

func TestCodeInterpreterUnsupportedLang(t *testing.T) {
	tool := NewCodeInterpreterTool()
	_, err := tool.Execute(context.Background(), map[string]string{"language": "rust", "code": "fn main() {}"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCodeInterpreterMissingParams(t *testing.T) {
	tool := NewCodeInterpreterTool()
	_, err := tool.Execute(context.Background(), map[string]string{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCodeInterpreterSyntaxError(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	tool := NewCodeInterpreterTool()
	_, err := tool.Execute(context.Background(), map[string]string{
		"language": "python", "code": "def broken(",
	})
	if err == nil {
		t.Fatal("expected error for syntax error")
	}
}
