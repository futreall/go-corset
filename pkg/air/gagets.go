package air

import (
	"fmt"

	"github.com/consensys/gnark-crypto/ecc/bls12-377/fr"
	"github.com/consensys/go-corset/pkg/table"
)

// ApplyBinaryGadget adds a binarity constraint for a given column in the schema
// which enforces that all values in the given column are either 0 or 1. For a
// column X, this corresponds to the vanishing constraint X * (X-1) == 0.
func ApplyBinaryGadget(col string, schema *Schema) {
	one := fr.NewElement(1)
	// Construct X
	X := &ColumnAccess{Column: col, Shift: 0}
	// Construct X-1
	X_m1 := &Sub{Args: []Expr{X, &Constant{Value: &one}}}
	// Construct X * (X-1)
	X_X_m1 := &Mul{Args: []Expr{X, X, X_m1}}
	// Done!
	schema.AddVanishingConstraint(col, nil, X_X_m1)
}

// ApplyBitwidthGadget ensures all values in a given column fit within a given
// number of bits.  This is implemented using a *byte decomposition* which adds
// n columns and a vanishing constraint (where n*8 >= nbits).
func ApplyBitwidthGadget(col string, nbits uint, schema *Schema) {
	if nbits%8 != 0 {
		panic("asymetric bitwidth constraints not yet supported")
	} else if nbits == 0 {
		panic("zero bitwidth constraint encountered")
	}
	// Calculate how many bytes required.
	n := nbits / 8
	es := make([]Expr, n)
	fr256 := fr.NewElement(256)
	coefficient := fr.NewElement(1)
	// Construct Columns
	for i := uint(0); i < n; i++ {
		// Determine name for the ith byte column
		colName := fmt.Sprintf("%s:%d", col, i)
		// Create Column + Constraint
		schema.AddColumn(colName, true)
		schema.AddRangeConstraint(colName, &fr256)
		es[i] = NewColumnAccess(colName, 0).Mul(NewConstantCopy(&coefficient))
		// Update coefficient
		coefficient.Mul(&coefficient, &fr256)
	}
	// Construct (X:0 * 1) + ... + (X:n * 2^n)
	sum := &Add{Args: es}
	// Construct X == (X:0 * 1) + ... + (X:n * 2^n)
	X := &ColumnAccess{Column: col, Shift: 0}
	eq := &Sub{Args: []Expr{X, sum}}
	schema.AddVanishingConstraint(col, nil, eq)
	// Finally, add the necessary byte decomposition computation.
	schema.AddComputation(table.NewByteDecomposition(col, nbits))
}

// ApplyColumnSortingGadget Add sorting constraints for a column where the
// difference between any two rows (i.e. the delta) is constrained to fit within
// a given bitwidth.  The target column is assumed to have an appropriate
// (enforced) bitwidth to ensure overflow cannot arise.  The sorting constraint
// is either ascending (positively signed) or descending (negatively signed).  A
// delta column is added along with bitwidth constraints (where necessary) to
// ensure the delta is within the given width.
func ApplyColumnSortingGadget(column string, sign bool, bitwidth uint, schema *Schema) {
	var deltaName string
	// Configure computation
	Xk := NewColumnAccess(column, 0)
	Xkm1 := NewColumnAccess(column, -1)
	// Account for sign
	var Xdiff Expr
	if sign {
		Xdiff = Xk.Sub(Xkm1)
		deltaName = fmt.Sprintf("+%s", column)
	} else {
		Xdiff = Xkm1.Sub(Xk)
		deltaName = fmt.Sprintf("-%s", column)
	}
	// Add delta column
	schema.AddColumn(deltaName, true)
	// Add diff computation
	schema.AddComputation(table.NewComputedColumn(deltaName, Xdiff))
	// Add necessary bitwidth constraints
	ApplyBitwidthGadget(deltaName, bitwidth, schema)
	// Configure constraint: Delta[k] = X[k] - X[k-1]
	Dk := NewColumnAccess(deltaName, 0)
	schema.AddVanishingConstraint(deltaName, nil, Dk.Equate(Xdiff))
}
