package schgo

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func openSQLite(t *testing.T) *sql.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	return db
}

func TestSQLiteAdapter_NameAndQuote(t *testing.T) {
	a := &SQLiteAdapter{}

	if a.Name() != "sqlite" {
		t.Fatalf("unexpected name: %s", a.Name())
	}

	if got := a.QuoteIdentifier("tbl"); got != "`tbl`" {
		t.Fatalf("unexpected quote: %s", got)
	}
}

func TestSQLiteAdapter_GenerateCreateTable(t *testing.T) {
	a := &SQLiteAdapter{}

	tbl := &Table{Name: "users"}

	tbl.Primary("id", "INTEGER")
	tbl.Column("email", "TEXT").NotNull().Unique()
	tbl.Column("age", "INTEGER").Default("18")

	sql := a.GenerateCreateTable(tbl)

	if !strings.Contains(sql, "CREATE TABLE `users`") {
		t.Fatalf("bad create sql: %s", sql)
	}

	if !strings.Contains(sql, "`id` INTEGER PRIMARY KEY AUTOINCREMENT") {
		t.Fatalf("missing primary key: %s", sql)
	}

	if !strings.Contains(sql, "`email` TEXT NOT NULL UNIQUE") {
		t.Fatalf("missing email definition: %s", sql)
	}

	if !strings.Contains(sql, "`age` INTEGER NOT NULL DEFAULT 18") {
		t.Fatalf("missing default: %s", sql)
	}
}

func TestSQLiteAdapter_GenerateAlterTable_AddDrop(t *testing.T) {
	a := &SQLiteAdapter{}

	diff := &TableDiff{
		Add: []*Column{
			{Name: "name", Type: "TEXT"},
		},
		Drop: []string{"old"},
	}

	sqls, err := a.GenerateAlterTable("t", diff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sqls) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(sqls))
	}

	if !strings.Contains(sqls[0], "ADD COLUMN") {
		t.Fatalf("expected ADD COLUMN, got %s", sqls[0])
	}

	if !strings.Contains(sqls[1], "DROP COLUMN") {
		t.Fatalf("expected DROP COLUMN, got %s", sqls[1])
	}
}

func TestSQLiteAdapter_GenerateAlterTable_ModifyError(t *testing.T) {
	a := &SQLiteAdapter{}

	diff := &TableDiff{
		Modify: []*ColumnChange{
			{
				Column: &Column{Name: "c", Type: "TEXT"},
				Old:    &ColumnInfo{Name: "c", Type: "INTEGER"},
			},
		},
	}

	_, err := a.GenerateAlterTable("t", diff)
	if err == nil {
		t.Fatal("expected error for modify")
	}
}

func TestSQLiteAdapter_GenerateIndexSQL(t *testing.T) {
	a := &SQLiteAdapter{}

	idx := &Index{
		Name:    "idx_users_email",
		Columns: []string{"email"},
		Unique:  true,
	}

	create := a.GenerateCreateIndex("users", idx)
	if !strings.Contains(create, "CREATE UNIQUE INDEX") {
		t.Fatalf("bad create index: %s", create)
	}

	drop := a.GenerateDropIndex("users", "idx_users_email")
	if drop != "DROP INDEX IF EXISTS `idx_users_email`" {
		t.Fatalf("bad drop index: %s", drop)
	}
}

func TestSQLiteAdapter_NeedsModification(t *testing.T) {
	a := &SQLiteAdapter{}

	base := &ColumnInfo{
		Name:     "c",
		Type:     "TEXT",
		Nullable: true,
	}

	tests := []struct {
		name string
		col  *Column
		old  *ColumnInfo
		want bool
	}{
		{
			"type change",
			&Column{Name: "c", Type: "INTEGER", Nullable: true},
			base,
			true,
		},
		{
			"nullability change",
			&Column{Name: "c", Type: "TEXT", Nullable: false},
			base,
			true,
		},
		{
			"default added",
			&Column{Name: "c", Type: "TEXT", Nullable: true, Def: "1"},
			base,
			true,
		},
		{
			"default match",
			&Column{Name: "c", Type: "TEXT", Nullable: true, Def: "1"},
			&ColumnInfo{
				Name:     "c",
				Type:     "TEXT",
				Nullable: true,
				Default:  sqlNull("1"),
			},
			false,
		},
		{
			"no change",
			&Column{Name: "c", Type: "TEXT", Nullable: true},
			base,
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := a.NeedsModification(tt.col, tt.old); got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestSQLiteAdapter_GetTables_Introspection(t *testing.T) {
	db := openSQLite(t)
	defer db.Close()

	_, err := db.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL UNIQUE
		);
		CREATE INDEX idx_users_email ON users(email);
	`)
	if err != nil {
		t.Fatalf("setup error: %v", err)
	}

	a := &SQLiteAdapter{}

	tables, err := a.GetTables(db)
	if err != nil {
		t.Fatalf("GetTables error: %v", err)
	}

	if len(tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(tables))
	}

	tbl := tables[0]

	if tbl.Name != "users" {
		t.Fatalf("unexpected table name: %s", tbl.Name)
	}

	if len(tbl.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(tbl.Columns))
	}

	if len(tbl.Indices) != 1 {
		t.Fatalf("expected 1 index, got %d", len(tbl.Indices))
	}

	if tbl.Indices[0].Name != "idx_users_email" {
		t.Fatalf("unexpected index name: %s", tbl.Indices[0].Name)
	}
}

func sqlNull(s string) sql.NullString {
	return sql.NullString{String: s, Valid: true}
}
