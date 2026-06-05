package tools

import "testing"

func TestShellInvocation(t *testing.T) {
	// Windows uses PowerShell; the command is the last arg.
	name, args := shellInvocation("windows", "echo hi")
	if name != "powershell" {
		t.Fatalf("windows shell = %q, want powershell", name)
	}
	if args[len(args)-1] != "echo hi" {
		t.Fatalf("windows args = %v", args)
	}

	// POSIX targets use bash -c.
	for _, goos := range []string{"linux", "darwin", "android"} {
		name, args := shellInvocation(goos, "echo hi")
		if name != "bash" || len(args) != 2 || args[0] != "-c" || args[1] != "echo hi" {
			t.Fatalf("%s shell = %q args=%v, want bash -c", goos, name, args)
		}
	}
}
