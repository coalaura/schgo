# schgo

**schgo** is a lightweight, declarative database schema management library for Go.

## Features

- **Declarative Schema**: Define tables, columns, and indices using Go methods.
- **Auto-Migration**: Automatically detects differences and applies changes (Create Table, Add/Drop/Modify Column, Index management).
- **SQLite Support**: Only supports SQLite at the moment but extendable via adapters.
- **Fluent API**: clean, chainable syntax for defining columns.

## Installation

```bash
go get -u github.com/coalaura/schgo
```

## Quick Start

```go
package main

import (
	"database/sql"
	"log"

	"github.com/coalaura/schgo"
	_ "modernc.org/sqlite" // or your driver of choice
)

func main() {
	// 1. Connect to your database
	db, err := sql.Open("sqlite", "file:data.db")
	if err != nil {
		log.Fatal(err)
	}

	defer db.Close()

	// 2. Initialize the schema manager (auto-detects driver)
	schema, err := schgo.NewSchema(db)
	if err != nil {
		log.Fatal(err)
	}

	// 3. Define your tables
	users := schema.Table("users")

	users.Primary("id", "INTEGER")
	users.Column("email", "TEXT").NotNull().Unique()
	users.Column("full_name", "TEXT").Default("Anonymous")
	users.Column("age", "INTEGER").Null()
	
	// Add indices
	users.Index("idx_users_name", "full_name")

	// 4. Apply changes
	// This will generate CREATE TABLE if it doesn't exist,
	// or ALTER TABLE if columns have changed.
	err = schema.Apply()
	if err != nil {
		log.Fatalf("Migration failed: %v", err)
	}

	log.Println("Schema synchronized successfully!")
}
```

## Supported Operations

| Operation | SQLite | MySQL | PostgreSQL |
|-----------|:------:|:-----:|:----------:|
| Create Table | ✅ | ✅ | ✅ |
| Add Column | ✅ | ✅ | ✅ |
| Drop Column | ✅ | ✅ | ✅ |
| Modify Column | ❌ | ✅ | ✅ |
| Indices | ✅ | ✅ | ✅ |

*> Note: SQLite support is limited by the database engine's inability to modify existing column types/constraints efficiently.*
