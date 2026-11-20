package config_test

import (
	"errors"
	"sync"
	"testing"

	"cyberstrike-ai/internal/config"
)

func TestStoragePolicyConcurrentSnapshotsAndRollback(t *testing.T) {
	initial := 5
	cfg := &config.Config{Storage: config.StorageConfig{IntervalMinutes: &initial}}
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 50; j++ {
				if err := cfg.UpdateStoragePolicy(func(current config.StorageConfig) (config.StorageConfig, error) {
					next := *current.IntervalMinutes + 1
					current.IntervalMinutes = &next
					return current, nil
				}); err != nil {
					t.Error(err)
				}
				snapshot := cfg.StorageSnapshot()
				*snapshot.IntervalMinutes = -1 // Must not mutate live pointers.
			}
		}()
	}
	workers.Wait()
	if got := cfg.StorageSnapshot().IntervalMinutesEffective(); got != 405 {
		t.Fatalf("lost update: %d", got)
	}
	failure := errors.New("persistence failed")
	err := cfg.UpdateStoragePolicy(func(current config.StorageConfig) (config.StorageConfig, error) {
		*current.IntervalMinutes = 7
		return current, failure
	})
	if !errors.Is(err, failure) || cfg.StorageSnapshot().IntervalMinutesEffective() != 405 {
		t.Fatal("failed transaction changed policy")
	}
	days := 90
	if err := cfg.UpdateStoragePolicy(func(current config.StorageConfig) (config.StorageConfig, error) {
		current.Categories = map[string]config.StorageCategoryConfig{config.StorageCategoryChatUploads: {RetentionDays: &days}}
		return current, nil
	}); err != nil {
		t.Fatal(err)
	}
	days = 0
	snapshot := cfg.StorageSnapshot()
	*snapshot.Categories[config.StorageCategoryChatUploads].RetentionDays = 1
	if got := cfg.StorageSnapshot().CategoryRetentionDays(config.StorageCategoryChatUploads); got != 90 {
		t.Fatalf("aliased category: %d", got)
	}
}
