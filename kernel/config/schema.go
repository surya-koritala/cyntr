package config

import "time"

type CyntrConfig struct {
	Version           string                  `yaml:"version"`
	Listen            ListenConfig            `yaml:"listen"`
	Tenants           map[string]TenantConfig `yaml:"tenants"`
	Auth              AuthConfig              `yaml:"auth"`
	Audit             AuditConfig             `yaml:"audit"`
	Federation        FederationConfig        `yaml:"federation"`
	ShellExecPolicies []ShellExecPolicyConfig `yaml:"shell_exec_policies"`
	// Packs gates opt-in tool packs (e.g. "loomfeed"). A pack is enabled when
	// its key is mapped to true here, or when the equivalent CYNTR_PACK_<NAME>=1
	// env var is set. Default: all packs disabled — the platform ships core
	// tools only; vertical packs are opt-in.
	Packs map[string]bool `yaml:"packs,omitempty"`
}

// ShellExecPolicyConfig declares which backend the shell_exec tool should use
// for a given tenant. Backend is "inprocess" (default, runs bash -c on host)
// or "docker" (runs inside an isolated tenant.DockerSandbox container with
// --network none, read-only filesystem, /tmp tmpfs, 256m memory, 0.5 CPU).
// Image and Timeout only apply when Backend == "docker".
type ShellExecPolicyConfig struct {
	Tenant  string        `yaml:"tenant"`
	Backend string        `yaml:"backend"`
	Image   string        `yaml:"image"`
	Timeout time.Duration `yaml:"timeout"`
}

type ListenConfig struct {
	Address string `yaml:"address"`
	WebUI   string `yaml:"webui"`
}

type TenantConfig struct {
	Isolation string       `yaml:"isolation"`
	Cgroup    CgroupConfig `yaml:"cgroup"`
	Policy    string       `yaml:"policy"`
}

type CgroupConfig struct {
	MemoryLimit string `yaml:"memory_limit"`
	CPUShares   int    `yaml:"cpu_shares"`
}

type AuthConfig struct {
	Provider string `yaml:"provider"`
	Issuer   string `yaml:"issuer"`
	ClientID string `yaml:"client_id"`
}

type AuditConfig struct {
	StoragePath string `yaml:"storage_path"`
	Retention   string `yaml:"retention"`
}

type FederationConfig struct {
	Enabled bool         `yaml:"enabled"`
	Peers   []PeerConfig `yaml:"peers"`
}

type PeerConfig struct {
	Name        string `yaml:"name"`
	Endpoint    string `yaml:"endpoint"`
	Fingerprint string `yaml:"fingerprint"`
}
