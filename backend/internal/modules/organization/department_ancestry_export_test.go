package organization

// DepartmentAncestryQueryForTest shares the actual statement with the external plan test.
func DepartmentAncestryQueryForTest() (string, int) {
	return departmentAncestryQuery, departmentReferenceMaxDepth
}

// DepartmentSubtreeQueryForTest shares the actual discovery statement with the external plan test.
func DepartmentSubtreeQueryForTest() string {
	return departmentSubtreeQuery
}
