package organization

// DepartmentAncestryQueryForTest shares the actual statement with the external plan test.
func DepartmentAncestryQueryForTest() (string, int) {
	return departmentAncestryQuery, departmentReferenceMaxDepth
}
