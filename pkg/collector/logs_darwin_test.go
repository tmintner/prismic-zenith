package collector

import (
	"testing"
	"zenith/pkg/db"
)

func TestCollectLogsDarwin(t *testing.T) {
	// Dummy database (won't actually be used for network calls in this test)
	database := db.NewVictoriaDB("http://localhost:8428", "http://localhost:9428")

	// Test collection for the last 1 minute
	err := CollectLogs(database, "1m")
	if err != nil {
		t.Fatalf("CollectLogs failed: %v", err)
	}

	t.Log("Successfully called CollectLogs without error")
}
