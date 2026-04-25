package module

func ScanIaCStateRowsForTest(rows iacStateRows) ([]*IaCState, error) {
	return scanIaCStateRows(rows)
}

func MigrateStatementsForExistingColumnsForTest(columns []string) []string {
	existing := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		existing[column] = struct{}{}
	}
	return migrateStatementsForExistingColumns(existing)
}
