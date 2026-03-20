package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Migration describes a config schema upgrade.
type Migration struct {
	FromVersion string
	ToVersion   string
	Migrate     func(cfg *CyntrConfig) error
}

// Migrator handles config schema upgrades.
type Migrator struct {
	migrations []Migration
}

// NewMigrator creates a migrator with registered migrations.
func NewMigrator() *Migrator {
	m := &Migrator{}
	m.registerMigrations()
	return m
}

func (m *Migrator) registerMigrations() {
	// Version 1 -> 2: add default audit config
	m.migrations = append(m.migrations, Migration{
		FromVersion: "1",
		ToVersion:   "2",
		Migrate: func(cfg *CyntrConfig) error {
			if cfg.Audit.StoragePath == "" {
				cfg.Audit.StoragePath = "~/.cyntr/audit"
			}
			if cfg.Audit.Retention == "" {
				cfg.Audit.Retention = "365d"
			}
			cfg.Version = "2"
			return nil
		},
	})
}

// NeedsMigration checks if a config needs upgrading.
func (m *Migrator) NeedsMigration(cfg CyntrConfig, targetVersion string) bool {
	return cfg.Version != targetVersion
}

// Migrate applies all necessary migrations to bring config to target version.
func (m *Migrator) Migrate(cfg *CyntrConfig, targetVersion string) error {
	for _, mig := range m.migrations {
		if cfg.Version == mig.FromVersion {
			if err := mig.Migrate(cfg); err != nil {
				return fmt.Errorf("migration %s->%s: %w", mig.FromVersion, mig.ToVersion, err)
			}
		}
		if cfg.Version == targetVersion {
			return nil
		}
	}

	if cfg.Version != targetVersion {
		return fmt.Errorf("no migration path from version %q to %q", cfg.Version, targetVersion)
	}
	return nil
}

// MigrateFile reads, migrates, and writes a config file in place.
func (m *Migrator) MigrateFile(path, targetVersion string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	var cfg CyntrConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	if !m.NeedsMigration(cfg, targetVersion) {
		return nil // already at target
	}

	if err := m.Migrate(&cfg, targetVersion); err != nil {
		return err
	}

	output, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	return os.WriteFile(path, output, 0644)
}
