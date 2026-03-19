package config

import "fmt"

// Store manages configuration loading and access.
// Full implementation in a later task.
type Store struct{}

func Load(path string) (*Store, error) { return nil, fmt.Errorf("config not implemented") }
