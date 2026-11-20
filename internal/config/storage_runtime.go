package config

import "sync"

// storagePolicyMu protects live storage policy reads and updates across handlers
// and background cleanup. Configuration loading finishes before these readers start.
var storagePolicyMu sync.RWMutex

// StorageSnapshot returns an independent policy copy safe to use after unlocking.
// A nil configuration returns the default (zero) policy.
func (c *Config) StorageSnapshot() StorageConfig {
	if c == nil {
		return StorageConfig{}
	}
	storagePolicyMu.RLock()
	defer storagePolicyMu.RUnlock()
	return cloneStoragePolicy(c.Storage)
}

// UpdateStoragePolicy serializes a read/modify/persist transaction. The callback
// receives an independent copy; errors leave the live policy unchanged. It must
// not call StorageSnapshot or UpdateStoragePolicy while holding this lock.
func (c *Config) UpdateStoragePolicy(update func(StorageConfig) (StorageConfig, error)) error {
	storagePolicyMu.Lock()
	defer storagePolicyMu.Unlock()
	next, err := update(cloneStoragePolicy(c.Storage))
	if err != nil {
		return err
	}
	c.Storage = cloneStoragePolicy(next)
	return nil
}

func clonePolicyValue[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneStoragePolicy(policy StorageConfig) StorageConfig {
	policy.AutoClean = clonePolicyValue(policy.AutoClean)
	policy.IntervalMinutes = clonePolicyValue(policy.IntervalMinutes)
	policy.OrphanGraceDays = clonePolicyValue(policy.OrphanGraceDays)
	policy.ActiveGraceHours = clonePolicyValue(policy.ActiveGraceHours)
	if policy.Categories != nil {
		categories := make(map[string]StorageCategoryConfig, len(policy.Categories))
		for key, value := range policy.Categories {
			value.Enabled = clonePolicyValue(value.Enabled)
			value.RetentionDays = clonePolicyValue(value.RetentionDays)
			categories[key] = value
		}
		policy.Categories = categories
	}
	return policy
}
