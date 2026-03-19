package config

type CyntrConfig struct {
	Version    string                  `yaml:"version"`
	Listen     ListenConfig            `yaml:"listen"`
	Tenants    map[string]TenantConfig `yaml:"tenants"`
	Auth       AuthConfig              `yaml:"auth"`
	Audit      AuditConfig             `yaml:"audit"`
	Federation FederationConfig        `yaml:"federation"`
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
