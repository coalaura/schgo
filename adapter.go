package schgo

import (
	"database/sql"
	"errors"
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

	// EscapeString sql escapes a string for this database
	EscapeString(s string) string
}

// AdapterInfo contains adapter factory and detection hints
type AdapterInfo struct {
	Factory  func() Adapter
	Matchers []string
	Probe    func(db *sql.DB) bool
}

// TableInfo represents existing table information
type TableInfo struct {
	Name    string
	Columns []*ColumnInfo
	Indices []*IndexInfo
}

// ColumnInfo represents existing column information
type ColumnInfo struct {
	Default    sql.NullString
	Name       string
	Type       string
	Nullable   bool
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
var adapters = make(map[string]*AdapterInfo)

// RegisterAdapter registers an adapter with detection hints
func RegisterAdapter(name string, info *AdapterInfo) {
	adapters[name] = info
}

// GetAdapter returns an adapter by name
func GetAdapter(name string) (Adapter, error) {
	info, ok := adapters[name]
	if !ok {
		return nil, fmt.Errorf("unknown adapter: %s", name)
	}

	return info.Factory(), nil
}

// DetectAdapter auto-detects the database type and returns the appropriate adapter
func DetectAdapter(db *sql.DB) (Adapter, error) {
	driverType := reflect.TypeOf(db.Driver()).String()

	for name, info := range adapters {
		for _, pattern := range info.Matchers {
			if strings.Contains(driverType, pattern) {
				return GetAdapter(name)
			}
		}
	}

	for name, info := range adapters {
		if info.Probe != nil && info.Probe(db) {
			return GetAdapter(name)
		}
	}

	return nil, errors.New("could not detect database type")
}
