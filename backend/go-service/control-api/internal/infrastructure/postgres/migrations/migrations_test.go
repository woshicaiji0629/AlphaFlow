package migrations

import (
	"strings"
	"testing"
)

func TestEmbeddedMigrationsAreOrderedAndNonEmpty(t *testing.T) {
	items, err := load()
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	if len(items) == 0 {
		t.Fatal("load() returned no migrations")
	}
	for index, item := range items {
		if strings.TrimSpace(item.query) == "" {
			t.Fatalf("migration %d is empty", item.version)
		}
		if index > 0 && items[index-1].version >= item.version {
			t.Fatalf("migrations are not ordered: %#v", items)
		}
	}
}

func TestStrategyVersionsMigrationRemovesCodeOnlyUniqueness(t *testing.T) {
	items, err := load()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.name != "0004_strategy_versions.sql" {
			continue
		}
		if !strings.Contains(item.query, "DROP CONSTRAINT IF EXISTS strategies_code_key") {
			t.Fatalf("strategy versions migration does not remove code-only constraint: %q", item.query)
		}
		return
	}
	t.Fatal("strategy versions migration is not embedded")
}
