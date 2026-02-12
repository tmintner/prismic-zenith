package db

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVictoriaDB_InsertMetric(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/write" {
			t.Errorf("Expected path /write, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	v := NewVictoriaDB(server.URL, server.URL)
	labels := map[string]string{"test": "true", "host": "localhost"}
	err := v.InsertMetric("test_metric", 42.0, labels)
	if err != nil {
		t.Fatalf("Failed to insert metric: %v", err)
	}
}

func TestVictoriaDB_InsertLogs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/insert/jsonline" {
			t.Errorf("Expected path /insert/jsonline, got %s", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "test-process") {
			t.Errorf("Expected body to contain test-process, got %s", string(body))
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	v := NewVictoriaDB(server.URL, server.URL)
	entries := []LogEntry{
		{
			Timestamp:    "2024-01-01T00:00:01Z",
			ProcessName:  "test-process",
			EventMessage: "batch message 1",
		},
	}
	err := v.InsertLogs(entries)
	if err != nil {
		t.Fatalf("Failed to insert logs: %v", err)
	}
}

func TestVictoriaDB_QueryLogs(t *testing.T) {
	mockResponse := `{"processName":"wifid","eventMessage":"connected"}` + "\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/select/logsql/query" {
			t.Errorf("Expected path /select/logsql/query, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("query") != "processName:\"wifid\"" {
			t.Errorf("Expected query param processName:\"wifid\", got %s", r.URL.Query().Get("query"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResponse))
	}))
	defer server.Close()

	v := NewVictoriaDB(server.URL, server.URL)
	res, err := v.QueryLogs("processName:\"wifid\"")
	if err != nil {
		t.Fatalf("Failed to query logs: %v", err)
	}

	if !strings.Contains(res, "wifid") {
		t.Fatalf("Expected results to contain wifid, got: %s", res)
	}
}
