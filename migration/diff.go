package migration

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// TableDef represents a parsed CREATE TABLE statement.
type TableDef struct {
	Name    string
	Columns []ColumnDef
	Indexes []string // raw CREATE INDEX statements
}

// ColumnDef represents a column within a CREATE TABLE statement.
type ColumnDef struct {
	Name       string
	Definition string // full column definition (type + constraints)
}

// DiffSchemas compares an old and new schema DDL and generates ALTER TABLE statements
// to migrate from old to new. It handles added columns, dropped columns, and added/dropped indexes.
// It does NOT handle column type changes or renames (those require manual migration).
func DiffSchemas(oldDDL, newDDL string) (upSQL string, downSQL string) {
	oldTables := parseTables(oldDDL)
	newTables := parseTables(newDDL)

	oldIndexes := parseIndexes(oldDDL)
	newIndexes := parseIndexes(newDDL)

	var upParts, downParts []string

	// Find added or modified tables.
	for name, newTable := range newTables {
		oldTable, exists := oldTables[name]
		if !exists {
			// Entire table is new.
			upParts = append(upParts, buildCreateTable(newTable))
			downParts = append(downParts, fmt.Sprintf("DROP TABLE IF EXISTS %s;", name))
			continue
		}

		// Compare columns.
		oldCols := columnMap(oldTable.Columns)
		newCols := columnMap(newTable.Columns)

		for colName, colDef := range newCols {
			if _, exists := oldCols[colName]; !exists {
				upParts = append(upParts, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;", name, colName, colDef))
				downParts = append(downParts, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", name, colName))
			}
		}

		for colName, colDef := range oldCols {
			if _, exists := newCols[colName]; !exists {
				upParts = append(upParts, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", name, colName))
				downParts = append(downParts, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;", name, colName, colDef))
			}
		}
	}

	// Find dropped tables.
	for name, oldTable := range oldTables {
		if _, exists := newTables[name]; !exists {
			upParts = append(upParts, fmt.Sprintf("DROP TABLE IF EXISTS %s;", name))
			downParts = append(downParts, buildCreateTable(oldTable))
		}
	}

	// Compare indexes.
	for idxName, idxSQL := range newIndexes {
		if _, exists := oldIndexes[idxName]; !exists {
			upParts = append(upParts, idxSQL+";")
			downParts = append(downParts, fmt.Sprintf("DROP INDEX IF EXISTS %s;", idxName))
		}
	}

	for idxName, idxSQL := range oldIndexes {
		if _, exists := newIndexes[idxName]; !exists {
			upParts = append(upParts, fmt.Sprintf("DROP INDEX IF EXISTS %s;", idxName))
			downParts = append(downParts, idxSQL+";")
		}
	}

	sort.Strings(upParts)
	sort.Strings(downParts)

	return strings.Join(upParts, "\n"), strings.Join(downParts, "\n")
}

var createTableRe = regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(\w+)\s*\(([\s\S]*?)\)\s*;`)
var createIndexRe = regexp.MustCompile(`(?i)(CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?(\w+)\s+ON\s+\w+\s*\([^)]+\))\s*;?`)

func parseTables(ddl string) map[string]TableDef {
	tables := make(map[string]TableDef)
	matches := createTableRe.FindAllStringSubmatch(ddl, -1)
	for _, m := range matches {
		name := m[1]
		body := m[2]
		columns := parseColumns(body)
		tables[name] = TableDef{
			Name:    name,
			Columns: columns,
		}
	}
	return tables
}

func parseColumns(body string) []ColumnDef {
	var cols []ColumnDef
	lines := strings.Split(body, ",")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip constraints (PRIMARY KEY, FOREIGN KEY, CHECK, UNIQUE, CONSTRAINT).
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "PRIMARY KEY") ||
			strings.HasPrefix(upper, "FOREIGN KEY") ||
			strings.HasPrefix(upper, "CHECK") ||
			strings.HasPrefix(upper, "UNIQUE") ||
			strings.HasPrefix(upper, "CONSTRAINT") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		cols = append(cols, ColumnDef{
			Name:       parts[0],
			Definition: strings.Join(parts[1:], " "),
		})
	}
	return cols
}

func parseIndexes(ddl string) map[string]string {
	indexes := make(map[string]string)
	matches := createIndexRe.FindAllStringSubmatch(ddl, -1)
	for _, m := range matches {
		fullStmt := strings.TrimSpace(m[1])
		name := m[2]
		indexes[name] = fullStmt
	}
	return indexes
}

func columnMap(cols []ColumnDef) map[string]string {
	m := make(map[string]string, len(cols))
	for _, c := range cols {
		m[c.Name] = c.Definition
	}
	return m
}

func buildCreateTable(t TableDef) string {
	var colDefs []string
	for _, c := range t.Columns {
		colDefs = append(colDefs, fmt.Sprintf("    %s %s", c.Name, c.Definition))
	}
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n%s\n);", t.Name, strings.Join(colDefs, ",\n"))
}
