package constraint

import (
	"fmt"

	"github.com/consensys/gnark-crypto/ecc/bls12-377/fr"
	"github.com/consensys/go-corset/pkg/schema"
	sc "github.com/consensys/go-corset/pkg/schema"
	"github.com/consensys/go-corset/pkg/trace"
	"github.com/consensys/go-corset/pkg/util/sexp"
)

// RangeFailure provides structural information about a failing type constraint.
type RangeFailure struct {
	// Handle of the failing constraint
	Handle string
	// Constraint expression
	Expr sc.Evaluable
	// Row on which the constraint failed
	Row uint
}

// Message provides a suitable error message
func (p *RangeFailure) Message() string {
	// Construct useful error message
	return fmt.Sprintf("expression \"%s\" out-of-bounds (row %d)", p.Handle, p.Row)
}

func (p *RangeFailure) String() string {
	return p.Message()
}

// RangeConstraint restricts all values for a given expression to be within a
// range [0..n) for some bound n.  Any bound is supported, and the system will
// choose the best underlying implementation as needed.
type RangeConstraint[E sc.Evaluable] struct {
	// A unique identifier for this constraint.  This is primarily useful for
	// debugging.
	Handle string
	// Evaluation Context for this constraint which must match that of the
	// constrained expression itself.
	Context trace.Context
	// The expression whose values are being constrained to within the given
	// bound.
	Expr E
	// The upper Bound for this constraint.  Specifically, every evaluation of
	// the expression should produce a value strictly below this Bound.  NOTE:
	// an fr.Element is used here to store the Bound simply to make the
	// necessary comparison against table data more direct.
	Bound fr.Element
}

// NewRangeConstraint constructs a new Range constraint!
func NewRangeConstraint[E sc.Evaluable](handle string, context trace.Context,
	expr E, bound fr.Element) *RangeConstraint[E] {
	return &RangeConstraint[E]{handle, context, expr, bound}
}

// BoundedAtMost determines whether the bound for this constraint is at most a given bound.
func (p *RangeConstraint[E]) BoundedAtMost(bound uint) bool {
	var n fr.Element = fr.NewElement(uint64(bound))
	return p.Bound.Cmp(&n) <= 0
}

// Accepts checks whether a range constraint holds on every row of a table. If so, return
// nil otherwise return an error.
//
//nolint:revive
func (p *RangeConstraint[E]) Accepts(tr trace.Trace) schema.Failure {
	// Determine height of enclosing module
	height := tr.Height(p.Context)
	// Iterate every row
	for k := 0; k < int(height); k++ {
		// Get the value on the kth row
		kth := p.Expr.EvalAt(k, tr)
		// Perform the range check
		if kth.Cmp(&p.Bound) >= 0 {
			// Evaluation failure
			return &RangeFailure{p.Handle, p.Expr, uint(k)}
		}
	}
	// All good
	return nil
}

// Lisp converts this schema element into a simple S-Expression, for example so
// it can be printed.
//
//nolint:revive
func (p *RangeConstraint[E]) Lisp(schema sc.Schema) sexp.SExp {
	//
	return sexp.NewList([]sexp.SExp{
		sexp.NewSymbol("definrange"),
		p.Expr.Lisp(schema),
		sexp.NewSymbol(p.Bound.String()),
	})
}
