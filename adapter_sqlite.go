package schgo

import (
	"database/sql"
	"fmt"
	"strings"
)

func init() {
	RegisterAdapter("sqlite", func() Adapter {
		return &SQLiteAdapter{}
	})
}

// SQLiteAdapter implements Adapter for SQLite
type SQLiteAdapter struct{}

func (a *SQLiteAdapter) Name() string {
	return "sqlite"
}

func (a *SQLiteAdapter) QuoteIdentifier(name string) string {
	return fmt.Sprintf("`%s`", name)
}

func (a *SQLiteAdapter) GetTables(db *sql.DB) ([]*TableInfo, error) {
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var tables []*TableInfo

	for rows.Next() {
		var name string

		err = rows.Scan(&name)
		if err != nil {
			return nil, err
		}

		table, err := a.getTableInfo(db, name)
		if err != nil {
			return nil, err
		}

		tables = append(tables, table)
	}

	return tables, rows.Err()
}

func (a *SQLiteAdapter) getTableInfo(db *sql.DB, tableName string) (*TableInfo, error) {
	info := &TableInfo{
		Name:    tableName,
		Columns: make([]*ColumnInfo, 0),
		Indices: make([]*IndexInfo, 0),
	}

	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", a.QuoteIdentifier(tableName)))
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	for rows.Next() {
		var (
			cid     int
			name    string
			typ     string
			notNull int
			pk      int
			def     sql.NullString
		)

		err = rows.Scan(&cid, &name, &typ, &notNull, &def, &pk)
		if err != nil {
			return nil, err
		}

		info.Columns = append(info.Columns, &ColumnInfo{
			Name:       name,
			Type:       typ,
			Nullable:   notNull == 0,
			Default:    def,
			PrimaryKey: pk == 1,
		})
	}

	err = rows.Err()
	if err != nil {
		return nil, err
	}

	idxRows, err := db.Query(fmt.Sprintf("PRAGMA index_list(%s)", a.QuoteIdentifier(tableName)))
	if err != nil {
		return nil, err
	}

	defer idxRows.Close()

	for idxRows.Next() {
		var (
			seq     int
			name    string
			origin  string
			unique  int
			partial int
		)

		err = idxRows.Scan(&seq, &name, &unique, &origin, &partial)
		if err != nil {
			return nil, err
		}

		// no auto-created indices
		if origin == "pk" || origin == "u" {
			continue
		}

		cols, err := a.getIndexColumns(db, name)
		if err != nil {
			return nil, err
		}

		info.Indices = append(info.Indices, &IndexInfo{
			Name:    name,
			Columns: cols,
			Unique:  unique == 1,
		})
	}

	return info, idxRows.Err()
}

func (a *SQLiteAdapter) getIndexColumns(db *sql.DB, indexName string) ([]string, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA index_info(%s)", a.QuoteIdentifier(indexName)))
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var columns []string

	for rows.Next() {
		var (
			seqno int
			cid   int
			name  string
		)

		err = rows.Scan(&seqno, &cid, &name)
		if err != nil {
			return nil, err
		}

		columns = append(columns, name)
	}

	return columns, rows.Err()
}

func (a *SQLiteAdapter) GenerateCreateTable(table *Table) string {
	parts := make([]string, 0, len(table.Columns))

	for _, col := range table.Columns {
		parts = append(parts, a.columnDefinition(col))
	}

	return fmt.Sprintf("CREATE TABLE %s (%s)", a.QuoteIdentifier(table.Name), strings.Join(parts, ", "))
}

func (a *SQLiteAdapter) columnDefinition(col *Column) string {
	parts := make([]string, 0, 2)

	parts = append(parts, a.QuoteIdentifier(col.Name))
	parts = append(parts, col.Type)

	if col.PrimaryKey {
		parts = append(parts, "PRIMARY KEY")

		if col.AutoIncr {
			parts = append(parts, "AUTOINCREMENT")
		}
	}

	if !col.Nullable && !col.PrimaryKey {
		parts = append(parts, "NOT NULL")
	}

	if col.Uniq && !col.PrimaryKey {
		parts = append(parts, "UNIQUE")
	}

	if col.Def != "" {
		parts = append(parts, "DEFAULT", col.Def)
	}

	return strings.Join(parts, " ")
}

func (a *SQLiteAdapter) GenerateAlterTable(tableName string, diff *TableDiff) ([]string, error) {
	queries := make([]string, 0, len(diff.Add)+len(diff.Drop))

	for _, col := range diff.Add {
		queries = append(queries, fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN %s",
			a.QuoteIdentifier(tableName),
			a.columnDefinition(col),
		))
	}

	for _, col := range diff.Drop {
		queries = append(queries, fmt.Sprintf(
			"ALTER TABLE %s DROP COLUMN %s",
			a.QuoteIdentifier(tableName),
			a.QuoteIdentifier(col),
		))
	}

	if len(diff.Modify) > 0 {
		return nil, fmt.Errorf("sqlite does not support modifying existing columns in table %q", tableName)
	}

	return queries, nil
}

func (a *SQLiteAdapter) GenerateCreateIndex(tableName string, index *Index) string {
	var unique string

	if index.Unique {
		unique = "UNIQUE "
	}

	quotedCols := make([]string, len(index.Columns))

	for i, col := range index.Columns {
		quotedCols[i] = a.QuoteIdentifier(col)
	}

	return fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)",
		unique,
		a.QuoteIdentifier(index.Name),
		a.QuoteIdentifier(tableName),
		strings.Join(quotedCols, ", "),
	)
}

func (a *SQLiteAdapter) GenerateDropIndex(tableName, indexName string) string {
	return fmt.Sprintf("DROP INDEX IF EXISTS %s", a.QuoteIdentifier(indexName))
}

func (a *SQLiteAdapter) NeedsModification(defined *Column, existing *ColumnInfo) bool {
	if !strings.EqualFold(defined.Type, existing.Type) {
		return true
	}

	if defined.Nullable != existing.Nullable {
		return true
	}

	if defined.Def != "" && existing.Default.Valid {
		if defined.Def != existing.Default.String {
			return true
		}
	} else if defined.Def != "" || existing.Default.Valid {
		return true
	}

	return false
}
