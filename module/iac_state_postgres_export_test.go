package module

func ScanIaCStateRowsForTest(rows iacStateRows) ([]*IaCState, error) {
	return scanIaCStateRows(rows)
}
