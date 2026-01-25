package schgo

import (
	"database/sql"
)

// Schema manages database schema migrations
type Schema struct {
	db      *sql.DB
	adapter Adapter
	tables  []*Table
}

// NewSchema creates a new schema manager and auto-detects the database type
func NewSchema(db *sql.DB) (*Schema, error) {
	adapter, err := DetectAdapter(db)
	if err != nil {
		return nil, err
	}

	return &Schema{
		db:      db,
		adapter: adapter,
		tables:  make([]*Table, 0),
	}, nil
}

// NewSchemaWithAdapter creates a schema with a specific adapter
func NewSchemaWithAdapter(db *sql.DB, adapter Adapter) *Schema {
	return &Schema{
		db:      db,
		adapter: adapter,
		tables:  make([]*Table, 0),
	}
}

// Table creates or retrieves a table definition
func (s *Schema) Table(name string) *Table {
	for _, t := range s.tables {
		if t.Name == name {
			return t
		}
	}

	t := &Table{
		Name:    name,
		Columns: make([]*Column, 0),
		Indices: make([]*Index, 0),
	}

	s.tables = append(s.tables, t)

	return t
}

// Apply synchronizes the database schema with the defined schema
func (s *Schema) Apply() error {
	tables, err := s.adapter.GetTables(s.db)
	if err != nil {
		return err
	}

	existing := make(map[string]*TableInfo)

	for _, t := range tables {
		existing[t.Name] = t
	}

	defined := make(map[string]*Table)

	for _, t := range s.tables {
		defined[t.Name] = t
	}

	for _, table := range s.tables {
		existingTable, exists := existing[table.Name]
		if !exists {
			query := s.adapter.GenerateCreateTable(table)

			_, err = s.db.Exec(query)
			if err != nil {
				return err
			}

			for _, idx := range table.Indices {
				query := s.adapter.GenerateCreateIndex(table.Name, idx)

				_, err = s.db.Exec(query)
				if err != nil {
					return err
				}
			}

			continue
		}

		diff := s.computeTableDiff(table, existingTable)

		queries, err := s.adapter.GenerateAlterTable(table.Name, diff)
		if err != nil {
			return err
		}

		for _, query := range queries {
			_, err := s.db.Exec(query)
			if err != nil {
				return err
			}
		}

		queries = s.generateIndexChanges(table, existingTable)

		for _, query := range queries {
			_, err := s.db.Exec(query)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Schema) computeTableDiff(defined *Table, existing *TableInfo) *TableDiff {
	diff := &TableDiff{
		Add:    make([]*Column, 0),
		Drop:   make([]string, 0),
		Modify: make([]*ColumnChange, 0),
	}

	existingCols := make(map[string]*ColumnInfo)

	for _, col := range existing.Columns {
		existingCols[col.Name] = col
	}

	definedCols := make(map[string]*Column)

	for _, col := range defined.Columns {
		definedCols[col.Name] = col
	}

	for _, col := range defined.Columns {
		existingCol, exists := existingCols[col.Name]
		if !exists {
			diff.Add = append(diff.Add, col)

			continue
		}

		if s.adapter.NeedsModification(col, existingCol) {
			diff.Modify = append(diff.Modify, &ColumnChange{
				Column: col,
				Old:    existingCol,
			})
		}
	}

	for name := range existingCols {
		if _, exists := definedCols[name]; !exists {
			diff.Drop = append(diff.Drop, name)
		}
	}

	return diff
}

func (s *Schema) generateIndexChanges(defined *Table, existing *TableInfo) []string {
	var queries []string

	existingIdx := make(map[string]*IndexInfo)

	for _, idx := range existing.Indices {
		existingIdx[idx.Name] = idx
	}

	definedIdx := make(map[string]*Index)

	for _, idx := range defined.Indices {
		definedIdx[idx.Name] = idx
	}

	for name, idx := range existingIdx {
		defIdx, exists := definedIdx[name]
		if !exists || !s.indexMatches(defIdx, idx) {
			queries = append(queries, s.adapter.GenerateDropIndex(defined.Name, name))
		}
	}

	for name, idx := range definedIdx {
		existIdx, exists := existingIdx[name]
		if !exists || !s.indexMatches(idx, existIdx) {
			queries = append(queries, s.adapter.GenerateCreateIndex(defined.Name, idx))
		}
	}

	return queries
}

func (s *Schema) indexMatches(defined *Index, existing *IndexInfo) bool {
	if defined.Unique != existing.Unique {
		return false
	}

	if len(defined.Columns) != len(existing.Columns) {
		return false
	}

	for i, col := range defined.Columns {
		if col != existing.Columns[i] {
			return false
		}
	}

	return true
}
