package collector

import "zenith/pkg/db"

type LogEntry struct {
	Timestamp    string `json:"timestamp"`
	ProcessID    int    `json:"processID"`
	ProcessName  string `json:"processName"`
	Subsystem    string `json:"subsystem"`
	Category     string `json:"category"`
	LogLevel     string `json:"messageType"`
	EventMessage string `json:"eventMessage"`
}

type Collector interface {
	CollectLogs(database *db.VictoriaDB, duration string) error
	CollectMetrics(database *db.VictoriaDB) error
}
