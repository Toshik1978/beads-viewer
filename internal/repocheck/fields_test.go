package repocheck_test

// This file enforces CLAUDE.md's field cap, the tripwire closest to the
// regression every size limit in this project is a proxy for: the terminal
// viewer this one replaced had a root model of roughly 100 fields, reached one
// field at a time, with no single commit that looked wrong.
//
// Model's own 12-field cap has had TestModelStaysSmall all along. Everything
// else in the tree had nothing, which is the position the line caps were in
// until bv-dyt.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/repocheck"
)

// maxTypeFields is CLAUDE.md's cap. The number lives here, as the line caps'
// do in sizes_test.go, and CLAUDE.md keeps the reasoning.
const maxTypeFields = 20

// exemptType is the one type CLAUDE.md excuses, and the reason is why it can
// be a name rather than a rule: beads.Issue is the JSONL decode target, so its
// 26 fields are br's wire format rather than a design this project chose.
// Deleting a field to satisfy a cap would mean dropping data the viewer is
// supposed to render.
//
// bv-vpv proposed deriving this instead — "a behavioural type is one that
// declares at least one method" — and the tree refutes it: Issue declares
// IsTombstone, so the derived rule would flag the very type it was meant to
// excuse while leaving tui.KeyMap, 17 fields and no methods at all, entirely
// unchecked. A rule that has to be corrected by the list it was meant to
// replace is worse than the list.
const exemptType = "beads.Issue"

// typeFields is one struct declaration's field count.
type typeFields struct {
	// name is package-qualified, e.g. "beads.Issue", using the package clause
	// rather than the directory: they agree everywhere in this tree, and the
	// clause is what a reader writing the exemption above would type.
	name  string
	file  string
	count int
}

type fieldsTestSuite struct {
	suite.Suite

	structs []typeFields
}

func (s *fieldsTestSuite) SetupSuite() {
	root, err := repocheck.RepoRoot(s.T().Context())
	s.Require().NoError(err)

	tracked, err := repocheck.TrackedFiles(s.T().Context(), root)
	s.Require().NoError(err)

	for _, name := range tracked {
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		s.structs = append(s.structs, s.structsIn(root, name)...)
	}

	// Sorted so a failure lists offenders in a stable order rather than in
	// whatever order git ls-files happened to hand back.
	slices.SortFunc(s.structs, func(a, b typeFields) int { return strings.Compare(a.name, b.name) })

	s.Require().NotEmpty(s.structs,
		"no struct declarations found — the parse is broken, not the tree empty")
}

// TestNoTypeExceedsTheFieldCap is the check. Every struct in the non-test
// tree, not only those with methods: the cap exists to catch a type quietly
// absorbing responsibilities, and a type does that whether or not it has
// grown methods yet. KeyMap is the case in point — 17 fields, no methods, and
// the closest thing in the tree to the limit.
func (s *fieldsTestSuite) TestNoTypeExceedsTheFieldCap() {
	for _, st := range s.structs {
		if st.name == exemptType {
			continue
		}
		s.LessOrEqual(st.count, maxTypeFields,
			st.name+" ("+st.file+") is over the field cap — give the type's "+
				"responsibilities somewhere else to live rather than raising it")
	}
}

// TestTheExemptTypeStillNeedsExempting keeps the exemption honest. An
// exemption that has stopped being load-bearing is worse than none: it reads
// as a live carve-out, so the next person to see the cap bind assumes theirs
// can be waved through the same way. If Issue ever falls under the cap on its
// own, this fails and the name goes.
func (s *fieldsTestSuite) TestTheExemptTypeStillNeedsExempting() {
	for _, st := range s.structs {
		if st.name != exemptType {
			continue
		}
		s.Greater(st.count, maxTypeFields,
			exemptType+" is within the cap now — delete the exemption rather than leaving a dead one")

		return
	}
	s.Fail(exemptType + " was not found — the exemption names a type that no longer exists")
}

// structsIn parses one file and returns every struct type it declares.
//
// Parsed rather than reflected over, because reflection would need the types
// linked into this test binary and this package deliberately imports none of
// them — it sits outside the runtime import graph entirely (see the package
// doc). The syntax tree is enough: a field count is a syntactic fact.
func (s *fieldsTestSuite) structsIn(root, name string) []typeFields {
	src, err := os.ReadFile(filepath.Join(root, name))
	s.Require().NoError(err)

	file, err := parser.ParseFile(token.NewFileSet(), name, src, 0)
	s.Require().NoError(err, name)

	var found []typeFields
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			spec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			structType, ok := spec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			found = append(found, typeFields{
				name:  file.Name.Name + "." + spec.Name.Name,
				file:  name,
				count: countFields(structType),
			})
		}
	}

	return found
}

// countFields counts declared fields, not lines: `a, b int` is two. An
// embedded field has no names and counts as one, which is the honest answer
// for a cap on how much one type carries — embedding is a way of acquiring
// fields, and counting it as zero would make it the obvious way around this.
func countFields(structType *ast.StructType) int {
	count := 0
	for _, field := range structType.Fields.List {
		if len(field.Names) == 0 {
			count++

			continue
		}
		count += len(field.Names)
	}

	return count
}
