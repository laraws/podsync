package db

// Config configures the SQL database used for storing feeds and episodes.
// Both SQLite and MySQL are supported via GORM.
type Config struct {
	// Type is the database driver: "sqlite" or "mysql".
	Type string `toml:"type"`
	// DSN is the data source name / connection string.
	// For SQLite this is a file path (e.g. "/app/db/podsync.db").
	// For MySQL this is a standard MySQL DSN
	// (e.g. "user:pass@tcp(127.0.0.1:3306)/podsync?charset=utf8mb4&parseTime=True").
	DSN string `toml:"dsn"`
	// Dir is an optional directory used to derive the default SQLite path.
	// When Type is "sqlite" and DSN is empty, the database file is placed at
	// "<Dir>/podsync.db".
	Dir string `toml:"dir"`
	// MaxOpenConns limits the maximum number of open connections (0 = unlimited).
	MaxOpenConns int `toml:"max_open_conns"`
	// MaxIdleConns limits the maximum number of idle connections (0 = default).
	MaxIdleConns int `toml:"max_idle_conns"`
}
