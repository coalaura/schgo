package schgo

import (
	"database/sql"
	"fmt"
	"strings"
)

func init() {
	RegisterAdapter("mysql", func() Adapter {
		return &MySQLAdapter{}
	})
}

// MySQLAdapter implements Adapter for MySQL/MariaDB
type MySQLAdapter struct{}

func (a *MySQLAdapter) Name() string {
	return "mysql"
}

func (a *MySQLAdapter) QuoteIdentifier(name string) string {
	return fmt.Sprintf("`%s`", name)
}

func (a *MySQLAdapter) GetTables(db *sql.DB) ([]*TableInfo, error) {
	rows, err := db.Query("SHOW TABLES")
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

func (a *MySQLAdapter) getTableInfo(db *sql.DB, tableName string) (*TableInfo, error) {
	info := &TableInfo{
		Name:    tableName,
		Columns: make([]*ColumnInfo, 0),
		Indices: make([]*IndexInfo, 0),
	}

	rows, err := db.Query(fmt.Sprintf("SHOW COLUMNS FROM %s", a.QuoteIdentifier(tableName)))
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	for rows.Next() {
		var (
			field   string
			colType string
			null    string
			key     string
			def     sql.NullString
			extra   string
		)

		err = rows.Scan(&field, &colType, &null, &key, &def, &extra)
		if err != nil {
			return nil, err
		}

		info.Columns = append(info.Columns, &ColumnInfo{
			Name:       field,
			Type:       colType,
			Nullable:   null == "YES",
			Default:    def,
			PrimaryKey: key == "PRI",
		})
	}

	err = rows.Err()
	if err != nil {
		return nil, err
	}

	idxRows, err := db.Query(fmt.Sprintf("SHOW INDEX FROM %s", a.QuoteIdentifier(tableName)))
	if err != nil {
		return nil, err
	}

	defer idxRows.Close()

	indexMap := make(map[string]*IndexInfo)

	for idxRows.Next() {
		var (
			table        string
			keyName      string
			colName      string
			collation    string
			indexType    string
			comment      string
			indexComment string
			nonUnique    int
			seqInIndex   int
			cardinality  int
			subPart      sql.NullString
			packed       sql.NullString
			null         sql.NullString
			visible      string
		)

		cols, err := idxRows.Columns()
		if err != nil {
			return nil, err
		}

		var scanDest []any

		if len(cols) >= 15 {
			scanDest = []any{
				&table, &nonUnique, &keyName, &seqInIndex, &colName,
				&collation, &cardinality, &subPart, &packed, &null,
				&indexType, &comment, &indexComment, &visible, new(string),
			}
		} else {
			scanDest = []any{
				&table, &nonUnique, &keyName, &seqInIndex, &colName,
				&collation, &cardinality, &subPart, &packed, &null,
				&indexType, &comment, &indexComment,
			}
		}

		err = idxRows.Scan(scanDest[:len(cols)]...)
		if err != nil {
			return nil, err
		}

		// no primary key index
		if keyName == "PRIMARY" {
			continue
		}

		if idx, ok := indexMap[keyName]; ok {
			idx.Columns = append(idx.Columns, colName)
		} else {
			indexMap[keyName] = &IndexInfo{
				Name:    keyName,
				Columns: []string{colName},
				Unique:  nonUnique == 0,
			}
		}
	}

	for _, idx := range indexMap {
		info.Indices = append(info.Indices, idx)
	}

	return info, idxRows.Err()
}

func (a *MySQLAdapter) GenerateCreateTable(table *Table) string {
	parts := make([]string, 0, len(table.Columns))

	var primaryKey string

	for _, col := range table.Columns {
		parts = append(parts, a.columnDefinition(col))

		if col.PrimaryKey {
			primaryKey = col.Name
		}
	}

	if primaryKey != "" {
		parts = append(parts, fmt.Sprintf("PRIMARY KEY (%s)", a.QuoteIdentifier(primaryKey)))
	}

	return fmt.Sprintf(
		"CREATE TABLE %s (%s) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
		a.QuoteIdentifier(table.Name),
		strings.Join(parts, ", "),
	)
}

func (a *MySQLAdapter) columnDefinition(col *Column) string {
	parts := make([]string, 0, 2)

	parts = append(parts, a.QuoteIdentifier(col.Name))
	parts = append(parts, col.Type)

	if col.AutoIncr {
		parts = append(parts, "AUTO_INCREMENT")
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

func (a *MySQLAdapter) GenerateAlterTable(tableName string, diff *TableDiff) ([]string, error) {
	queries := make([]string, 0, len(diff.Add)+len(diff.Modify)+len(diff.Drop))

	for _, col := range diff.Add {
		queries = append(queries, fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN %s",
			a.QuoteIdentifier(tableName),
			a.columnDefinition(col),
		))
	}

	for _, change := range diff.Modify {
		queries = append(queries, fmt.Sprintf(
			"ALTER TABLE %s MODIFY COLUMN %s",
			a.QuoteIdentifier(tableName),
			a.columnDefinition(change.Column),
		))
	}

	for _, colName := range diff.Drop {
		queries = append(queries, fmt.Sprintf(
			"ALTER TABLE %s DROP COLUMN %s",
			a.QuoteIdentifier(tableName),
			a.QuoteIdentifier(colName),
		))
	}

	return queries, nil
}

func (a *MySQLAdapter) GenerateCreateIndex(tableName string, index *Index) string {
	var unique string

	if index.Unique {
		unique = "UNIQUE "
	}

	quotedCols := make([]string, len(index.Columns))

	for i, col := range index.Columns {
		quotedCols[i] = a.QuoteIdentifier(col)
	}

	return fmt.Sprintf(
		"CREATE %sINDEX %s ON %s (%s)",
		unique,
		a.QuoteIdentifier(index.Name),
		a.QuoteIdentifier(tableName),
		strings.Join(quotedCols, ", "),
	)
}

func (a *MySQLAdapter) GenerateDropIndex(tableName, indexName string) string {
	return fmt.Sprintf("DROP INDEX %s ON %s", a.QuoteIdentifier(indexName), a.QuoteIdentifier(tableName))
}

func (a *MySQLAdapter) NeedsModification(defined *Column, existing *ColumnInfo) bool {
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
