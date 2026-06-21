package schgo

import (
	"database/sql"
	"fmt"
	"strings"
)

func init() {
	RegisterAdapter("sqlite", &AdapterInfo{
		Factory: func() Adapter {
			return &SQLiteAdapter{}
		},
		Matchers: []string{"sqlite"},
		Probe: func(db *sql.DB) bool {
			_, err := db.Exec("SELECT sqlite_version()")

			return err == nil
		},
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

func (a *SQLiteAdapter) EscapeString(s string) string {
	return fmt.Sprintf("'%s'", strings.ReplaceAll(s, "'", "''"))
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

		cols, cond, err := a.getIndexColumnsAndCondition(db, name, partial == 1)
		if err != nil {
			return nil, err
		}

		info.Indices = append(info.Indices, &IndexInfo{
			Name:      name,
			Columns:   cols,
			Unique:    unique == 1,
			Condition: cond,
		})
	}

	return info, idxRows.Err()
}

func (a *SQLiteAdapter) getIndexColumnsAndCondition(db *sql.DB, indexName string, isPartial bool) ([]string, string, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA index_info(%s)", a.QuoteIdentifier(indexName)))
	if err != nil {
		return nil, "", err
	}

	defer rows.Close()

	var (
		columns       []string
		hasExpression bool
	)

	for rows.Next() {
		var (
			seqno int
			cid   int
			name  sql.NullString
		)

		err = rows.Scan(&seqno, &cid, &name)
		if err != nil {
			return nil, "", err
		}

		if !name.Valid || name.String == "" {
			hasExpression = true
		} else {
			columns = append(columns, name.String)
		}
	}

	err = rows.Err()
	if err != nil {
		return nil, "", err
	}

	var condition string

	if hasExpression || isPartial {
		var indexSQL string

		err = db.QueryRow("SELECT sql FROM sqlite_master WHERE type='index' AND name = ?", indexName).Scan(&indexSQL)
		if err == nil {
			if hasExpression {
				parsedCols := parseIndexColumnsFromSQL(indexSQL)
				if len(parsedCols) > 0 {
					columns = parsedCols
				}
			}

			if isPartial {
				condition = parseIndexConditionFromSQL(indexSQL)
			}
		}
	}

	return columns, condition, nil
}

func parseIndexConditionFromSQL(indexSQL string) string {
	upper := strings.ToUpper(indexSQL)

	idx := strings.LastIndex(upper, " WHERE ")
	if idx == -1 {
		return ""
	}

	return indexSQL[idx+7:]
}

func (a *SQLiteAdapter) GenerateCreateTable(table *Table) string {
	parts := make([]string, 0, len(table.Columns))

	for _, col := range table.Columns {
		parts = append(parts, a.columnDefinition(col))
	}

	return fmt.Sprintf("CREATE TABLE %s (%s)", a.QuoteIdentifier(table.Name), strings.Join(parts, ", "))
}

func (a *SQLiteAdapter) columnDefinition(col *Column) string {
	parts := make([]string, 0, 6)

	parts = append(parts, a.QuoteIdentifier(col.Name))
	parts = append(parts, col.Type)

	if col.PrimaryKey {
		parts = append(parts, "PRIMARY KEY")

		if col.AutoIncr {
			parts = append(parts, "AUTOINCREMENT")
		}
	}

	if col.Nullable != nil && !col.PrimaryKey {
		if *col.Nullable {
			parts = append(parts, "NULL")
		} else {
			parts = append(parts, "NOT NULL")
		}
	}

	if col.Uniq && !col.PrimaryKey {
		parts = append(parts, "UNIQUE")
	}

	if col.Def != nil {
		parts = append(parts, "DEFAULT", a.EscapeString(*col.Def))
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
		cols := make([]string, 0, len(diff.Modify))

		for _, m := range diff.Modify {
			cols = append(cols, m.Column.Name)
		}

		return nil, fmt.Errorf("sqlite cannot modify columns in table %q: %s", tableName, strings.Join(cols, ", "))
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
		if isExpression(col) {
			quotedCols[i] = col
		} else {
			quotedCols[i] = a.QuoteIdentifier(col)
		}
	}

	var where string

	if index.Condition != "" {
		where = " WHERE " + index.Condition
	}

	return fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)%s",
		unique,
		a.QuoteIdentifier(index.Name),
		a.QuoteIdentifier(tableName),
		strings.Join(quotedCols, ", "),
		where,
	)
}

func (a *SQLiteAdapter) GenerateDropIndex(tableName, indexName string) string {
	return fmt.Sprintf("DROP INDEX IF EXISTS %s", a.QuoteIdentifier(indexName))
}

func (a *SQLiteAdapter) NeedsModification(defined *Column, existing *ColumnInfo) bool {
	if !strings.EqualFold(defined.Type, existing.Type) {
		return true
	}

	if defined.Nullable != nil && *defined.Nullable != existing.Nullable {
		// SQLite reports PRIMARY KEY as nullable unless explicitly NOT NULL.
		// If both are PKs, ignore the mismatch to support existing tables.
		if !(defined.PrimaryKey && existing.PrimaryKey) {
			return true
		}
	}

	if !defaultsMatch(defined, existing) {
		return true
	}

	return false
}

func parseIndexColumnsFromSQL(indexSQL string) []string {
	if indexSQL == "" {
		return nil
	}

	start := strings.IndexByte(indexSQL, '(')
	if start == -1 {
		return nil
	}

	depth := 1
	end := -1

	for i := start + 1; i < len(indexSQL); i++ {
		if indexSQL[i] == '(' {
			depth++
		} else if indexSQL[i] == ')' {
			depth--
			if depth == 0 {
				end = i

				break
			}
		}
	}

	if end == -1 {
		return nil
	}

	colsContent := indexSQL[start+1 : end]

	var (
		cols       []string
		current    strings.Builder
		parenDepth int
	)

	for i := 0; i < len(colsContent); i++ {
		char := colsContent[i]

		if char == '(' {
			parenDepth++

			current.WriteByte(char)
		} else if char == ')' {
			parenDepth--

			current.WriteByte(char)
		} else if char == ',' && parenDepth == 0 {
			cols = append(cols, cleanIndexColumn(current.String()))

			current.Reset()
		} else {
			current.WriteByte(char)
		}
	}

	if current.Len() > 0 {
		cols = append(cols, cleanIndexColumn(current.String()))
	}

	return cols
}

func cleanIndexColumn(col string) string {
	col = strings.TrimSpace(col)
	col = strings.Trim(col, "`\"' ")

	return col
}
