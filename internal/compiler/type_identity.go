package compiler

import "gosplash.dev/splash/internal/types"

// TypeIdentity is a detached representation of a checked signature type.
// Nominal names are scoped by the path of the bound declaration.
type TypeIdentity struct {
	Kind       string         `json:"kind"`
	Name       string         `json:"name,omitempty"`
	Path       string         `json:"path,omitempty"`
	Arguments  []TypeIdentity `json:"arguments,omitempty"`
	Parameters []TypeIdentity `json:"parameters,omitempty"`
	ReturnType *TypeIdentity  `json:"returnType,omitempty"`
}

func (a *analysis) typeIdentity(typ types.Type) TypeIdentity {
	switch t := typ.(type) {
	case *types.PrimitiveType:
		if t != types.Unknown {
			return TypeIdentity{Kind: "primitive", Name: t.TypeName()}
		}
	case *types.NamedType:
		if t.Declaration != nil {
			args := a.typeArguments(t.TypeArgs)
			if len(args) == len(t.TypeArgs) {
				return TypeIdentity{Kind: "record", Name: t.Declaration.Name, Path: a.sourcePath(t.Declaration.Pos().File), Arguments: args}
			}
		}
	case *types.EnumType:
		if t.Declaration != nil {
			return TypeIdentity{Kind: "enum", Name: t.Declaration.Name, Path: a.sourcePath(t.Declaration.Pos().File)}
		}
	case *types.OptionalType:
		if args := a.typeArguments([]types.Type{t.Inner}); args != nil {
			return TypeIdentity{Kind: "optional", Arguments: args}
		}
	case *types.ListType:
		if args := a.typeArguments([]types.Type{t.Element}); args != nil {
			return TypeIdentity{Kind: "list", Arguments: args}
		}
	case *types.ResultType:
		if args := a.typeArguments([]types.Type{t.Ok, t.Err}); args != nil {
			return TypeIdentity{Kind: "result", Arguments: args}
		}
	case *types.FunctionType:
		params := a.typeArguments(t.Params)
		ret := a.typeIdentity(t.Return)
		if len(params) == len(t.Params) && ret.Kind != "" {
			return TypeIdentity{Kind: "function", Parameters: params, ReturnType: &ret}
		}
	case *types.TypeParamType:
		return TypeIdentity{Kind: "typeParameter", Name: t.Name}
	}
	return TypeIdentity{}
}

func (a *analysis) typeArguments(input []types.Type) []TypeIdentity {
	if len(input) == 0 {
		return nil
	}
	out := make([]TypeIdentity, len(input))
	for i, typ := range input {
		out[i] = a.typeIdentity(typ)
		if out[i].Kind == "" {
			return nil
		}
	}
	return out
}
