package types

import "fmt"

// Type is the interface that all types in the Splash type system must implement.
type Type interface {
	TypeName() string
	IsAssignableTo(other Type) bool
	Classification() Classification
}

// PrimitiveType represents built-in primitive types.
type PrimitiveType struct {
	name string
}

func (p *PrimitiveType) TypeName() string {
	return p.name
}

func (p *PrimitiveType) IsAssignableTo(other Type) bool {
	// Primitives are only assignable to themselves
	if opt, ok := other.(*OptionalType); ok {
		return p.IsAssignableTo(opt.Inner)
	}
	if other, ok := other.(*PrimitiveType); ok {
		return p.name == other.name
	}
	return false
}

func (p *PrimitiveType) Classification() Classification {
	return ClassPublic
}

// Package-level primitive type variables
var (
	String  = &PrimitiveType{name: "String"}
	Int     = &PrimitiveType{name: "Int"}
	Float   = &PrimitiveType{name: "Float"}
	Bool    = &PrimitiveType{name: "Bool"}
	Void    = &PrimitiveType{name: "Void"}
	Unknown = &PrimitiveType{name: "Unknown"}
)

// NamedType represents a user-defined named type.
type NamedType struct {
	Name                   string
	TypeArgs              []Type
	FieldClassifications  []Classification
}

func (n *NamedType) TypeName() string {
	if len(n.TypeArgs) == 0 {
		return n.Name
	}
	// Format: Name<T1, T2, ...>
	result := n.Name + "<"
	for i, arg := range n.TypeArgs {
		if i > 0 {
			result += ", "
		}
		result += arg.TypeName()
	}
	result += ">"
	return result
}

func (n *NamedType) IsAssignableTo(other Type) bool {
	// Named types are assignable only by name equality
	if opt, ok := other.(*OptionalType); ok {
		return n.IsAssignableTo(opt.Inner)
	}
	if other, ok := other.(*NamedType); ok {
		return n.Name == other.Name
	}
	return false
}

func (n *NamedType) Classification() Classification {
	if len(n.FieldClassifications) == 0 {
		return ClassPublic
	}
	// Return the maximum classification
	max := n.FieldClassifications[0]
	for _, c := range n.FieldClassifications[1:] {
		if c > max {
			max = c
		}
	}
	return max
}

// Named creates a new NamedType with the given name.
func Named(name string) *NamedType {
	return &NamedType{
		Name: name,
	}
}

// OptionalType represents a type that can be nil.
type OptionalType struct {
	Inner Type
}

func (o *OptionalType) TypeName() string {
	return o.Inner.TypeName() + "?"
}

func (o *OptionalType) IsAssignableTo(other Type) bool {
	// T? is only assignable to T? if inner types match
	otherOpt, ok := other.(*OptionalType)
	if !ok {
		return false
	}
	return o.Inner.IsAssignableTo(otherOpt.Inner)
}

func (o *OptionalType) Classification() Classification {
	return o.Inner.Classification()
}

// ResultType represents a Result type with Ok and Err variants.
type ResultType struct {
	Ok  Type
	Err Type
}

func (r *ResultType) TypeName() string {
	return fmt.Sprintf("Result<%s, %s>", r.Ok.TypeName(), r.Err.TypeName())
}

func (r *ResultType) IsAssignableTo(other Type) bool {
	// Result types are assignable only if both Ok and Err types match
	if opt, ok := other.(*OptionalType); ok {
		return r.IsAssignableTo(opt.Inner)
	}
	otherResult, ok := other.(*ResultType)
	if !ok {
		return false
	}
	return r.Ok.IsAssignableTo(otherResult.Ok) && r.Err.IsAssignableTo(otherResult.Err)
}

func (r *ResultType) Classification() Classification {
	// Classification is the max of Ok and Err
	okClass := r.Ok.Classification()
	errClass := r.Err.Classification()
	if okClass > errClass {
		return okClass
	}
	return errClass
}

// ListType represents a list of elements of a specific type.
type ListType struct {
	Element Type
}

func (l *ListType) TypeName() string {
	return fmt.Sprintf("List<%s>", l.Element.TypeName())
}

func (l *ListType) IsAssignableTo(other Type) bool {
	// List types are assignable only if element types match
	if opt, ok := other.(*OptionalType); ok {
		return l.IsAssignableTo(opt.Inner)
	}
	otherList, ok := other.(*ListType)
	if !ok {
		return false
	}
	return l.Element.IsAssignableTo(otherList.Element)
}

func (l *ListType) Classification() Classification {
	return l.Element.Classification()
}

// FunctionType represents a function type with parameters and a return type.
type FunctionType struct {
	Params []Type
	Return Type
}

func (f *FunctionType) TypeName() string {
	result := "("
	for i, param := range f.Params {
		if i > 0 {
			result += ", "
		}
		result += param.TypeName()
	}
	result += ") -> "
	if f.Return != nil {
		result += f.Return.TypeName()
	} else {
		result += "Void"
	}
	return result
}

func (f *FunctionType) IsAssignableTo(other Type) bool {
	// Function types are assignable only if signatures match exactly
	if opt, ok := other.(*OptionalType); ok {
		return f.IsAssignableTo(opt.Inner)
	}
	otherFunc, ok := other.(*FunctionType)
	if !ok {
		return false
	}
	if len(f.Params) != len(otherFunc.Params) {
		return false
	}
	for i, param := range f.Params {
		if !param.IsAssignableTo(otherFunc.Params[i]) {
			return false
		}
	}
	if f.Return == nil && otherFunc.Return == nil {
		return true
	}
	if f.Return == nil || otherFunc.Return == nil {
		return false
	}
	return f.Return.IsAssignableTo(otherFunc.Return)
}

func (f *FunctionType) Classification() Classification {
	max := ClassPublic
	for _, param := range f.Params {
		if paramClass := param.Classification(); paramClass > max {
			max = paramClass
		}
	}
	if f.Return != nil {
		if returnClass := f.Return.Classification(); returnClass > max {
			max = returnClass
		}
	}
	return max
}

// TypeParamType represents a type parameter with optional constraints.
type TypeParamType struct {
	Name        string
	Constraints []string
}

func (t *TypeParamType) TypeName() string {
	if len(t.Constraints) == 0 {
		return t.Name
	}
	// Format: T where Constraint1, Constraint2, ...
	result := t.Name + " where "
	for i, constraint := range t.Constraints {
		if i > 0 {
			result += ", "
		}
		result += constraint
	}
	return result
}

func (t *TypeParamType) IsAssignableTo(other Type) bool {
	// Type parameters are assignable if names match
	if opt, ok := other.(*OptionalType); ok {
		return t.IsAssignableTo(opt.Inner)
	}
	otherTypeParam, ok := other.(*TypeParamType)
	if !ok {
		return false
	}
	return t.Name == otherTypeParam.Name
}

func (t *TypeParamType) Classification() Classification {
	return ClassPublic
}
