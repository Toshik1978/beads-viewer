package repocheck_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

// TestRepocheck is the package's one entry point, per this repository's
// one-entry-point-per-package rule, and it wires suites only.
//
// It lives in a file of its own rather than in any one suite's, which is a
// change from when this package was called `licensing` and the licensing
// sweep was the only thing in it. There are four suites now, and hosting the
// entry point inside one of them implied a seniority among them that has not
// been true for some time — the misreading that let the size caps go
// unenforced for six releases (bv-dyt) started as exactly that kind of
// mislabel.
func TestRepocheck(t *testing.T) {
	suite.Run(t, new(licensingTestSuite))
	suite.Run(t, new(workflowsTestSuite))
	suite.Run(t, new(sizesTestSuite))
	suite.Run(t, new(fieldsTestSuite))
}
