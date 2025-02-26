package hir

import (
	"fmt"

	"github.com/consensys/gnark-crypto/ecc/bls12-377/fr"
	"github.com/consensys/go-corset/pkg/mir"
	sc "github.com/consensys/go-corset/pkg/schema"
)

// LowerToMir lowers (or refines) an HIR table into an MIR schema.  That means
// lowering all the columns and constraints, whilst adding additional columns /
// constraints as necessary to preserve the original semantics.
func (p *Schema) LowerToMir() *mir.Schema {
	mirSchema := mir.EmptySchema()
	// Copy modules
	for _, mod := range p.modules {
		mirSchema.AddModule(mod.Name)
	}
	// Lower columns
	for _, input := range p.inputs {
		col := input.(DataColumn)
		mirSchema.AddDataColumn(col.Context(), col.Name(), col.Type())
	}
	// Lower assignments (nothing to do here)
	for _, a := range p.assignments {
		mirSchema.AddAssignment(a)
	}
	// Lower constraints
	for _, c := range p.constraints {
		lowerConstraintToMir(c, mirSchema)
	}
	// Copy property assertions.  Observe, these do not require lowering
	// because they are already MIR-level expressions.
	for _, c := range p.assertions {
		properties := c.Property.Expr.LowerTo(mirSchema)
		for _, p := range properties {
			mirSchema.AddPropertyAssertion(c.Handle, c.Context, p)
		}
	}
	//
	return mirSchema
}

func lowerConstraintToMir(c sc.Constraint, schema *mir.Schema) {
	// Check what kind of constraint we have
	if v, ok := c.(LookupConstraint); ok {
		lowerLookupConstraint(v, schema)
	} else if v, ok := c.(VanishingConstraint); ok {
		mir_exprs := v.Constraint.Expr.LowerTo(schema)
		// Add individual constraints arising
		for _, mir_expr := range mir_exprs {
			schema.AddVanishingConstraint(v.Handle, v.Context, v.Domain, mir_expr)
		}
	} else if v, ok := c.(RangeConstraint); ok {
		mir_exprs := v.Expr.LowerTo(schema)
		// Add individual constraints arising
		for _, mir_expr := range mir_exprs {
			schema.AddRangeConstraint(v.Handle, v.Context, mir_expr, v.Bound)
		}
	} else {
		// Should be unreachable as no other constraint types can be added to a
		// schema.
		panic("unreachable")
	}
}

func lowerLookupConstraint(c LookupConstraint, schema *mir.Schema) {
	from := make([]mir.Expr, len(c.Sources))
	into := make([]mir.Expr, len(c.Targets))
	// Convert general expressions into unit expressions.
	for i := 0; i < len(from); i++ {
		from[i] = lowerUnitTo(c.Sources[i], schema)
		into[i] = lowerUnitTo(c.Targets[i], schema)
	}
	//
	schema.AddLookupConstraint(c.Handle, c.SourceContext, c.TargetContext, from, into)
}

// Lower an expression which is expected to lower into a single expression.
// This will panic if the unit expression is malformed (i.e. does not lower
// into a single expression).
func lowerUnitTo(e UnitExpr, schema *mir.Schema) mir.Expr {
	exprs := lowerTo(e.Expr, schema)

	if len(exprs) != 1 {
		panic("invalid unitary expression")
	}

	return exprs[0]
}

// LowerTo lowers a sum expression to the MIR level.  This requires expanding
// the arguments, then lowering them.  Furthermore, conditionals are "lifted" to
// the top.
func (e *Add) LowerTo(schema *mir.Schema) []mir.Expr {
	return lowerTo(e, schema)
}

// LowerTo lowers a constant to the MIR level.   This requires expanding the
// arguments, then lowering them.  Furthermore, conditionals are "lifted" to the
// top.
func (e *Constant) LowerTo(schema *mir.Schema) []mir.Expr {
	return lowerTo(e, schema)
}

// LowerTo lowers a column access to the MIR level.  This requires expanding
// the arguments, then lowering them.  Furthermore, conditionals are "lifted" to
// the top.
func (e *ColumnAccess) LowerTo(schema *mir.Schema) []mir.Expr {
	return lowerTo(e, schema)
}

// LowerTo lowers an exponent expression to the MIR level.  This requires expanding
// the argument andn lowering it.  Furthermore, conditionals are "lifted" to
// the top.
func (e *Exp) LowerTo(schema *mir.Schema) []mir.Expr {
	return lowerTo(e, schema)
}

// LowerTo lowers a product expression to the MIR level.  This requires expanding
// the arguments, then lowering them.  Furthermore, conditionals are "lifted" to
// the top.
func (e *Mul) LowerTo(schema *mir.Schema) []mir.Expr {
	return lowerTo(e, schema)
}

// LowerTo lowers a list expression to the MIR level by eliminating it
// altogether.  This still requires expanding the arguments, then lowering them.
// Furthermore, conditionals are "lifted" to the top..
func (e *List) LowerTo(schema *mir.Schema) []mir.Expr {
	return lowerTo(e, schema)
}

// LowerTo lowers a normalise expression to the MIR level.  This requires
// expanding the arguments, then lowering them.  Furthermore, conditionals are
// "lifted" to the top..
func (e *Normalise) LowerTo(schema *mir.Schema) []mir.Expr {
	return lowerTo(e, schema)
}

// LowerTo lowers an if expression to the MIR level by "compiling out" the
// expression using normalisation at the MIR level.  This also requires
// expanding the arguments, then lowering them.  Furthermore, conditionals are
// "lifted" to the top.
func (e *IfZero) LowerTo(schema *mir.Schema) []mir.Expr {
	return lowerTo(e, schema)
}

// LowerTo lowers a subtract expression to the MIR level. This also requires
// expanding the arguments, then lowering them.  Furthermore, conditionals are
// "lifted" to the top.
func (e *Sub) LowerTo(schema *mir.Schema) []mir.Expr {
	return lowerTo(e, schema)
}

// ============================================================================
// lowerTo
// ============================================================================

// Lowers a given expression to the MIR level.  The expression is first expanded
// into one or more target expressions. Furthermore, conditions must be "lifted"
// to the root.
func lowerTo(e Expr, schema *mir.Schema) []mir.Expr {
	// First expand expression
	es := expand(e, schema)
	// Now lower each one (carefully)
	mes := make([]mir.Expr, len(es))
	//
	for i, e := range es {
		c := extractCondition(e, schema)
		b := extractBody(e, schema)
		mes[i] = mul2(c, b)
	}
	// Done
	return mes
}

// Extract the "condition" of an expression.  Every expression can be view as a
// conditional constraint of the form "if c then e", where "c" is the condition.
// This is allowed to return nil if the body is unconditional.
func extractCondition(e Expr, schema *mir.Schema) mir.Expr {
	if p, ok := e.(*Add); ok {
		return extractConditions(p.Args, schema)
	} else if _, ok := e.(*Constant); ok {
		return nil
	} else if _, ok := e.(*ColumnAccess); ok {
		return nil
	} else if p, ok := e.(*Mul); ok {
		return extractConditions(p.Args, schema)
	} else if p, ok := e.(*Normalise); ok {
		return extractCondition(p.Arg, schema)
	} else if p, ok := e.(*Exp); ok {
		return extractCondition(p.Arg, schema)
	} else if p, ok := e.(*IfZero); ok {
		return extractIfZeroCondition(p, schema)
	} else if p, ok := e.(*Sub); ok {
		return extractConditions(p.Args, schema)
	}
	// Should be unreachable
	panic(fmt.Sprintf("unknown expression: %s", e.Lisp(schema)))
}

func extractConditions(es []Expr, schema *mir.Schema) mir.Expr {
	var r mir.Expr = nil
	for _, e := range es {
		r = mul2(r, extractCondition(e, schema))
	}

	return r
}

// Extracting from conditional expressions is slightly more complex than others,
// so it gets a case of its own.
func extractIfZeroCondition(e *IfZero, schema *mir.Schema) mir.Expr {
	var bc mir.Expr
	// Lower condition
	cc := extractCondition(e.Condition, schema)
	cb := extractBody(e.Condition, schema)
	// Add conditions arising
	if e.TrueBranch != nil && e.FalseBranch != nil {
		// Expansion should ensure this case does not exist.  This is necessary
		// to ensure exactly one expression is generated from this expression.
		panic(fmt.Sprintf("unexpanded expression (%s)", e.Lisp(schema)))
	} else if e.TrueBranch != nil {
		// (1 - NORM(cb)) for true branch
		normBody := &mir.Normalise{Arg: cb}
		oneMinusNormBody := &mir.Sub{
			Args: []mir.Expr{
				&mir.Constant{Value: fr.One()},
				normBody,
			},
		}

		cb = oneMinusNormBody
		// Lower conditional's arising from body
		bc = extractCondition(e.TrueBranch, schema)
	} else {
		// Lower conditional's arising from body
		bc = extractCondition(e.FalseBranch, schema)
	}
	//
	return mul3(cc, cb, bc)
}

// Translate the "body" of an expression.  Every expression can be view as a
// conditional constraint of the form "if c then e", where "e" is the
// constraint.
func extractBody(e Expr, schema *mir.Schema) mir.Expr {
	if p, ok := e.(*Add); ok {
		return &mir.Add{Args: extractBodies(p.Args, schema)}
	} else if p, ok := e.(*Constant); ok {
		return &mir.Constant{Value: p.Val}
	} else if p, ok := e.(*ColumnAccess); ok {
		return &mir.ColumnAccess{Column: p.Column, Shift: p.Shift}
	} else if p, ok := e.(*Mul); ok {
		return &mir.Mul{Args: extractBodies(p.Args, schema)}
	} else if p, ok := e.(*Exp); ok {
		return &mir.Exp{Arg: extractBody(p.Arg, schema), Pow: p.Pow}
	} else if p, ok := e.(*Normalise); ok {
		return &mir.Normalise{Arg: extractBody(p.Arg, schema)}
	} else if p, ok := e.(*IfZero); ok {
		if p.TrueBranch != nil && p.FalseBranch != nil {
			// Expansion should ensure this case does not exist.  This is necessary
			// to ensure exactly one expression is generated from this expression.
			panic(fmt.Sprintf("unexpanded expression (%s)", e.Lisp(schema)))
		} else if p.TrueBranch != nil {
			return extractBody(p.TrueBranch, schema)
		}
		// Done
		return extractBody(p.FalseBranch, schema)
	} else if p, ok := e.(*Sub); ok {
		return &mir.Sub{Args: extractBodies(p.Args, schema)}
	}
	// Should be unreachable
	panic(fmt.Sprintf("unknown expression: %s", e.Lisp(schema)))
}

// Extract a vector of expanded expressions to the MIR level.
func extractBodies(es []Expr, schema *mir.Schema) []mir.Expr {
	rs := make([]mir.Expr, len(es))
	for i, e := range es {
		rs[i] = extractBody(e, schema)
	}

	return rs
}

// ============================================================================
// expand
// ============================================================================

// Expand an expression into one or more expressions by eliminating lists and
// breaking down conditions.  For example, a list such as say "(begin (- X Y) (-
// Y Z))" is broken down into two distinct expressions "(- X Y)" and "(- Y Z)".
// Likewise, a condition such as "(if X Y Z)" is broken down into two
// expressions "(if X Y)" and "(ifnot X Z)".  These are necessary steps for the
// conversion into a lower-level form.
func expand(e Expr, schema sc.Schema) []Expr {
	if p, ok := e.(*Add); ok {
		return expandWithNaryConstructor(p.Args, func(nargs []Expr) Expr {
			var args []Expr
			// Flatten nested sums
			for _, e := range nargs {
				if a, ok := e.(*Add); ok {
					args = append(args, a.Args...)
				} else {
					args = append(args, e)
				}
			}
			// Done
			return &Add{Args: args}
		}, schema)
	} else if _, ok := e.(*Constant); ok {
		return []Expr{e}
	} else if _, ok := e.(*ColumnAccess); ok {
		return []Expr{e}
	} else if p, ok := e.(*Mul); ok {
		return expandWithNaryConstructor(p.Args, func(nargs []Expr) Expr {
			var args []Expr
			// Flatten nested products
			for _, e := range nargs {
				if a, ok := e.(*Mul); ok {
					args = append(args, a.Args...)
				} else {
					args = append(args, e)
				}
			}
			// Done
			return &Mul{Args: args}
		}, schema)
	} else if p, ok := e.(*List); ok {
		ees := make([]Expr, 0)
		for _, arg := range p.Args {
			ees = append(ees, expand(arg, schema)...)
		}

		return ees
	} else if p, ok := e.(*Exp); ok {
		ees := expand(p.Arg, schema)
		for i, ee := range ees {
			ees[i] = &Exp{ee, p.Pow}
		}

		return ees
	} else if p, ok := e.(*Normalise); ok {
		ees := expand(p.Arg, schema)
		for i, ee := range ees {
			ees[i] = &Normalise{ee}
		}

		return ees
	} else if p, ok := e.(*IfZero); ok {
		ees := make([]Expr, 0)
		if p.TrueBranch != nil {
			// Expand true branch with condition
			ees = expandWithBinaryConstructor(p.Condition, p.TrueBranch, func(c Expr, tb Expr) Expr {
				return &IfZero{c, tb, nil}
			}, schema)
		}

		if p.FalseBranch != nil {
			// Expand false branch with condition
			fes := expandWithBinaryConstructor(p.Condition, p.FalseBranch, func(c Expr, fb Expr) Expr {
				return &IfZero{c, nil, fb}
			}, schema)
			ees = append(ees, fes...)
		}
		// Done
		return ees
	} else if p, ok := e.(*Sub); ok {
		return expandWithNaryConstructor(p.Args, func(nargs []Expr) Expr {
			return &Sub{Args: nargs}
		}, schema)
	}
	// Should be unreachable
	panic(fmt.Sprintf("unknown expression: %s", e.Lisp(schema)))
}

type binaryConstructor func(Expr, Expr) Expr
type naryConstructor func([]Expr) Expr

// LowerWithBinaryConstructor is a generic mechanism for lowering down to a binary expression.
func expandWithBinaryConstructor(lhs Expr, rhs Expr, create binaryConstructor, schema sc.Schema) []Expr {
	var res []Expr
	// Lower all three expressions
	is := expand(lhs, schema)
	js := expand(rhs, schema)

	// Now construct
	for i := 0; i < len(is); i++ {
		for j := 0; j < len(js); j++ {
			// Construct binary expression
			expr := create(is[i], js[j])
			// Append to the end
			res = append(res, expr)
		}
	}

	return res
}

// LowerWithNaryConstructor performs the cross-product expansion of an nary HIR
// expression. This is necessary because each argument of that expression will
// itself turn into one or more MIR expressions.  For example, consider lowering
// the following HIR expression:
//
// > (if X Y Z) + 10
//
// Here, (if X Y Z) will lower into two MIR expressions: (1-NORM(X))*Y and X*Z.
// Thus, we need to generate two MIR expressions for our example:
//
// > ((1 - NORM(X)) * Y) + 10 > (X * Y) + 10
//
// Finally, consider an expression such as the following:
//
// > (if X Y Z) + (if A B C)
//
// This will expand into *four* MIR expressions (i.e. the cross product of the
// left and right ifs).
func expandWithNaryConstructor(args []Expr, constructor naryConstructor, schema sc.Schema) []Expr {
	// Accumulator is initially empty
	acc := make([]Expr, len(args))
	// Start from the first argument
	return expandWithNaryConstructorHelper(0, acc, args, constructor, schema)
}

// LowerWithNaryConstructorHelper manages progress through the cross-product expansion.
// Specifically, "i" determines how much of args has been lowered thus
// far, whilst "acc" represents the current array being generated.
func expandWithNaryConstructorHelper(i int, acc []Expr, args []Expr,
	constructor naryConstructor, schema sc.Schema) []Expr {
	if i == len(acc) {
		// Base Case
		nacc := make([]Expr, len(acc))
		// Clone the slice because it is used as a temporary
		// working storage during the expansion.
		copy(nacc, acc)
		// Apply the constructor to produce the appropriate
		// mir.Expr.
		return []Expr{constructor(nacc)}
	}

	// Recursive Case
	var nargs []Expr

	for _, ith := range expand(args[i], schema) {
		acc[i] = ith
		iths := expandWithNaryConstructorHelper(i+1, acc, args, constructor, schema)
		nargs = append(nargs, iths...)
	}

	return nargs
}

// Multiply three expressions together, any of which could be nil.
func mul3(lhs mir.Expr, mhs mir.Expr, rhs mir.Expr) mir.Expr {
	return mul2(lhs, mul2(mhs, rhs))
}

// Multiply two expressions together, where either could be nil.  This attempts
// to a little clever in that it combines products together.
func mul2(lhs mir.Expr, rhs mir.Expr) mir.Expr {
	// Check for short-circuit
	if lhs == nil {
		return rhs
	} else if rhs == nil {
		return lhs
	}
	// Look for optimisation
	l, lok := lhs.(*mir.Mul)
	r, rok := rhs.(*mir.Mul)
	//
	if lok && rok {
		l.Args = append(l.Args, r.Args...)
		return l
	} else if lok {
		l.Args = append(l.Args, rhs)
		return l
	} else if rok {
		r.Args = append(r.Args, lhs)
		return r
	}
	// Fall back
	return &mir.Mul{Args: []mir.Expr{lhs, rhs}}
}
