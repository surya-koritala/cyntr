package tools

// Platform-aware shell selection (C16). The single Go binary cross-compiles to
// Windows, Android/Termux, macOS and Linux already (the whole stack is pure
// Go); the one POSIX assumption that bites is the shell. On native Windows
// there is no bash, so the in-process backend must invoke PowerShell instead.

// shellInvocation returns the executable and args used to run a shell command
// on the given GOOS. Kept as a pure function of goos so it is testable for
// every target without running on that OS.
func shellInvocation(goos, command string) (string, []string) {
	if goos == "windows" {
		// PowerShell ships on every supported Windows; -Command runs the
		// string and exits, mirroring `bash -c`.
		return "powershell", []string{"-NoProfile", "-NonInteractive", "-Command", command}
	}
	// Linux, macOS, Android/Termux: bash -c, as before.
	return "bash", []string{"-c", command}
}
