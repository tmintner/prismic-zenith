package db

import (
	"testing"
)

func TestVictoriaDB_InsertMetric(t *testing.T) {
	v := NewVictoriaDB("http://localhost:8428")
	labels := map[string]string{"test": "true", "host": "localhost"}
	err := v.InsertMetric("test_metric", 42.0, labels)
	if err != nil {
		t.Fatalf("Failed to insert metric: %v", err)
	}
}

func TestVictoriaDB_QueryMetrics(t *testing.T) {
	v := NewVictoriaDB("http://localhost:8428")
	// Query the metric we just inserted (or any other)
	res, err := v.QueryMetrics("test_metric")
	if err != nil {
		t.Fatalf("Failed to query metrics: %v", err)
	}
	if res == "" {
		t.Log("Query returned no results, which might be expected if VM hasn't flushed yet")
	} else {
		t.Logf("Query results: %s", res)
	}
}
