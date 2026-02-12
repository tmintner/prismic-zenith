package collector

import "zenith/pkg/db"

type Collector interface {
	CollectLogs(database *db.VictoriaDB, duration string) error
	CollectMetrics(database *db.VictoriaDB) error
}
