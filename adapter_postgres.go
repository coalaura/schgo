package schgo

import (
	"database/sql"
	"fmt"
	"strings"
)

func init() {
	RegisterAdapter("postgres", func() Adapter {
		return &PostgresAdapter{}
	})
}

// PostgresAdapter implements Adapter for PostgreSQL
type PostgresAdapter struct{}

func (a *PostgresAdapter) Name() string {
	return "postgres"
}

func (a *PostgresAdapter) QuoteIdentifier(name string) string {
	return fmt.Sprintf(`"%s"`, name)
}

func (a *PostgresAdapter) GetTables(db *sql.DB) ([]*TableInfo, error) {
	rows, err := db.Query("SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE'")
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

func (a *PostgresAdapter) getTableInfo(db *sql.DB, tableName string) (*TableInfo, error) {
	info := &TableInfo{
		Name:    tableName,
		Columns: make([]*ColumnInfo, 0),
		Indices: make([]*IndexInfo, 0),
	}

	rows, err := db.Query(`
		SELECT 
			c.column_name,
			c.data_type,
			c.is_nullable,
			c.column_default,
			CASE WHEN pk.column_name IS NOT NULL THEN true ELSE false END as is_primary
		FROM information_schema.columns c
		LEFT JOIN (
			SELECT ku.column_name
			FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage ku
				ON tc.constraint_name = ku.constraint_name
			WHERE tc.table_name = $1 AND tc.constraint_type = 'PRIMARY KEY'
		) pk ON c.column_name = pk.column_name
		WHERE c.table_name = $1 AND c.table_schema = 'public'
		ORDER BY c.ordinal_position
	`, tableName)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	for rows.Next() {
		var (
			name       string
			dataType   string
			isNullable string
			def        sql.NullString
			isPrimary  bool
		)

		err = rows.Scan(&name, &dataType, &isNullable, &def, &isPrimary)
		if err != nil {
			return nil, err
		}

		info.Columns = append(info.Columns, &ColumnInfo{
			Name:       name,
			Type:       dataType,
			Nullable:   isNullable == "YES",
			Default:    def,
			PrimaryKey: isPrimary,
		})
	}

	err = rows.Err()
	if err != nil {
		return nil, err
	}

	idxRows, err := db.Query(`
		SELECT
			i.relname as index_name,
			array_agg(a.attname ORDER BY array_position(ix.indkey, a.attnum)) as columns,
			ix.indisunique as is_unique
		FROM pg_index ix
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN pg_class t ON t.oid = ix.indrelid
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY(ix.indkey)
		WHERE t.relname = $1
			AND NOT ix.indisprimary
			AND t.relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = 'public')
		GROUP BY i.relname, ix.indisunique
	`, tableName)
	if err != nil {
		return nil, err
	}

	defer idxRows.Close()

	for idxRows.Next() {
		var (
			name    string
			columns []string
			unique  bool
		)

		err = idxRows.Scan(&name, &columns, &unique)
		if err != nil {
			return nil, err
		}

		info.Indices = append(info.Indices, &IndexInfo{
			Name:    name,
			Columns: columns,
			Unique:  unique,
		})
	}

	return info, idxRows.Err()
}

func (a *PostgresAdapter) GenerateCreateTable(table *Table) string {
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

	return fmt.Sprintf("CREATE TABLE %s (%s)", a.QuoteIdentifier(table.Name), strings.Join(parts, ", "))
}

func (a *PostgresAdapter) columnDefinition(col *Column) string {
	parts := make([]string, 0, 2)

	parts = append(parts, a.QuoteIdentifier(col.Name))

	if col.AutoIncr && col.PrimaryKey {
		switch strings.ToUpper(col.Type) {
		case "INTEGER", "INT":
			parts = append(parts, "SERIAL")
		case "BIGINT":
			parts = append(parts, "BIGSERIAL")
		case "SMALLINT":
			parts = append(parts, "SMALLSERIAL")
		default:
			parts = append(parts, col.Type)
		}
	} else {
		parts = append(parts, col.Type)
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

func (a *PostgresAdapter) GenerateAlterTable(tableName string, diff *TableDiff) ([]string, error) {
	queries := make([]string, 0, len(diff.Add)+len(diff.Modify)+len(diff.Drop))

	for _, col := range diff.Add {
		queries = append(queries, fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN %s",
			a.QuoteIdentifier(tableName),
			a.columnDefinition(col),
		))
	}

	for _, change := range diff.Modify {
		col := change.Column

		queries = append(queries, fmt.Sprintf(
			"ALTER TABLE %s ALTER COLUMN %s TYPE %s",
			a.QuoteIdentifier(tableName),
			a.QuoteIdentifier(col.Name),
			col.Type,
		))

		if col.Nullable {
			queries = append(queries, fmt.Sprintf(
				"ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL",
				a.QuoteIdentifier(tableName),
				a.QuoteIdentifier(col.Name),
			))
		} else {
			queries = append(queries, fmt.Sprintf(
				"ALTER TABLE %s ALTER COLUMN %s SET NOT NULL",
				a.QuoteIdentifier(tableName),
				a.QuoteIdentifier(col.Name),
			))
		}

		if col.Def != "" {
			queries = append(queries, fmt.Sprintf(
				"ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s",
				a.QuoteIdentifier(tableName),
				a.QuoteIdentifier(col.Name),
				col.Def,
			))
		} else {
			queries = append(queries, fmt.Sprintf(
				"ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT",
				a.QuoteIdentifier(tableName),
				a.QuoteIdentifier(col.Name),
			))
		}
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

func (a *PostgresAdapter) GenerateCreateIndex(tableName string, index *Index) string {
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

func (a *PostgresAdapter) GenerateDropIndex(tableName, indexName string) string {
	return fmt.Sprintf("DROP INDEX IF EXISTS %s", a.QuoteIdentifier(indexName))
}

func (a *PostgresAdapter) NeedsModification(defined *Column, existing *ColumnInfo) bool {
	if !strings.EqualFold(defined.Type, existing.Type) {
		return true
	}

	if defined.Nullable != existing.Nullable {
		return true
	}

	if defined.Def != "" && existing.Default.Valid {
		existingDefault := existing.Default.String

		idx := strings.Index(existingDefault, "::")
		if idx > 0 {
			existingDefault = existingDefault[:idx]
		}

		if defined.Def != existingDefault {
			return true
		}
	} else if defined.Def != "" || existing.Default.Valid {
		return true
	}

	return false
}
