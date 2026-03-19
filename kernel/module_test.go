package kernel

import "testing"

func TestModuleStateString(t *testing.T) {
	tests := []struct {
		state ModuleState
		want  string
	}{
		{ModuleStateRegistered, "registered"},
		{ModuleStateInitialized, "initialized"},
		{ModuleStateRunning, "running"},
		{ModuleStateStopped, "stopped"},
		{ModuleStateFailed, "failed"},
		{ModuleState(99), "unknown(99)"},
	}

	for _, tt := range tests {
		got := tt.state.String()
		if got != tt.want {
			t.Errorf("ModuleState(%d).String() = %q, want %q", int(tt.state), got, tt.want)
		}
	}
}

func TestHealthStatusDefaults(t *testing.T) {
	h := HealthStatus{}
	if h.Healthy {
		t.Error("default HealthStatus should not be healthy")
	}
	if h.Message != "" {
		t.Error("default HealthStatus message should be empty")
	}
}
