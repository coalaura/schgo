package schgo

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
)

// Adapter defines the interface for database-specific operations
type Adapter interface {
	// Name returns the adapter name
	Name() string

	// GetTables retrieves existing table information from the database
	GetTables(db *sql.DB) ([]*TableInfo, error)

	// GenerateCreateTable generates CREATE TABLE SQL
	GenerateCreateTable(table *Table) string

	// GenerateAlterTable generates ALTER TABLE SQL statements
	GenerateAlterTable(tableName string, diff *TableDiff) ([]string, error)

	// GenerateCreateIndex generates CREATE INDEX SQL
	GenerateCreateIndex(tableName string, index *Index) string

	// GenerateDropIndex generates DROP INDEX SQL
	GenerateDropIndex(tableName, indexName string) string

	// NeedsModification checks if a column needs to be altered
	NeedsModification(defined *Column, existing *ColumnInfo) bool

	// QuoteIdentifier quotes an identifier for this database
	QuoteIdentifier(name string) string
}

// TableInfo represents existing table information
type TableInfo struct {
	Name    string
	Columns []*ColumnInfo
	Indices []*IndexInfo
}

// ColumnInfo represents existing column information
type ColumnInfo struct {
	Name       string
	Type       string
	Nullable   bool
	Default    sql.NullString
	PrimaryKey bool
}

// IndexInfo represents existing index information
type IndexInfo struct {
	Name    string
	Columns []string
	Unique  bool
}

// TableDiff represents differences between defined and existing table
type TableDiff struct {
	Add    []*Column
	Drop   []string
	Modify []*ColumnChange
}

// ColumnChange represents a column modification
type ColumnChange struct {
	Column *Column
	Old    *ColumnInfo
}

// Registry of available adapters
var adapters = make(map[string]func() Adapter)

// RegisterAdapter registers an adapter factory
func RegisterAdapter(name string, factory func() Adapter) {
	adapters[name] = factory
}

// GetAdapter returns an adapter by name
func GetAdapter(name string) (Adapter, error) {
	factory, ok := adapters[name]
	if !ok {
		return nil, fmt.Errorf("unknown adapter: %s", name)
	}

	return factory(), nil
}

// DetectAdapter auto-detects the database type and returns the appropriate adapter
func DetectAdapter(db *sql.DB) (Adapter, error) {
	driverType := reflect.TypeOf(db.Driver()).String()

	switch {
	case strings.Contains(driverType, "sqlite"):
		return GetAdapter("sqlite")
	case strings.Contains(driverType, "mysql"):
		return GetAdapter("mysql")
	case strings.Contains(driverType, "postgres"), strings.Contains(driverType, "pq"):
		return GetAdapter("postgres")
	}

	_, err := db.Exec("SELECT sqlite_version()")
	if err == nil {
		return GetAdapter("sqlite")
	}

	_, err = db.Exec("SELECT version()")
	if err == nil {
		var version string

		err = db.QueryRow("SELECT @@version").Scan(&version)
		if err == nil {
			return GetAdapter("mysql")
		}

		return GetAdapter("postgres")
	}

	return nil, fmt.Errorf("could not detect database type")
}
