package schgo

import "strings"

var autoIncrTypes = []string{"INT", "INTEGER", "BIGINT", "SMALLINT", "TINYINT", "MEDIUMINT", "SERIAL", "BIGSERIAL"}

// Table represents a database table definition
type Table struct {
	Name    string
	Columns []*Column
	Indices []*Index
}

// Column represents a column definition
type Column struct {
	Name       string
	Type       string
	Def        string
	Nullable   bool
	PrimaryKey bool
	AutoIncr   bool
	Uniq       bool
}

// Index represents an index definition
type Index struct {
	Name    string
	Columns []string
	Unique  bool
}

// Primary adds a primary key column
func (t *Table) Primary(name, typ string) *Column {
	col := &Column{
		Name:       name,
		Type:       typ,
		PrimaryKey: true,
		AutoIncr:   canAutoIncrement(typ),
	}

	t.Columns = append(t.Columns, col)

	return col
}

// Column adds a column to the table
func (t *Table) Column(name, typ string) *Column {
	col := &Column{
		Name: name,
		Type: typ,
	}

	t.Columns = append(t.Columns, col)

	return col
}

// NotNull sets the column as NOT NULL
func (c *Column) NotNull() *Column {
	c.Nullable = false

	return c
}

// Null sets the column as nullable
func (c *Column) Null() *Column {
	c.Nullable = true

	return c
}

// Default sets the default value
func (c *Column) Default(val string) *Column {
	c.Def = val

	return c
}

// Unique marks the column as unique
func (c *Column) Unique() *Column {
	c.Uniq = true

	return c
}

// AutoIncrement marks the column as auto-incrementing
func (c *Column) AutoIncrement() *Column {
	c.AutoIncr = true

	return c
}

// Index adds an index to the table
func (t *Table) Index(name string, columns ...string) *Index {
	idx := &Index{
		Name:    name,
		Columns: columns,
		Unique:  false,
	}

	t.Indices = append(t.Indices, idx)

	return idx
}

// UniqueIndex adds a unique index to the table
func (t *Table) UniqueIndex(name string, columns ...string) *Index {
	idx := &Index{
		Name:    name,
		Columns: columns,
		Unique:  true,
	}

	t.Indices = append(t.Indices, idx)

	return idx
}

func canAutoIncrement(typ string) bool {
	upper := strings.ToUpper(typ)

	for _, vt := range autoIncrTypes {
		if upper == vt {
			return true
		}

		if strings.HasPrefix(upper, vt+"(") || strings.HasPrefix(upper, vt+" ") {
			return true
		}
	}

	return false
}
