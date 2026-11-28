package toolschema

import (
	"fmt"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/types"
)

// TypeToSchema projects a checked parameter type. Unknown or unsupported
// shapes fail admission instead of publishing a guessed schema.
func TypeToSchema(typ types.Type) (*SchemaProperty, error) {
	switch typ {
	case types.String:
		return &SchemaProperty{Type: "string"}, nil
	case types.Int:
		return &SchemaProperty{Type: "integer"}, nil
	case types.Float:
		return &SchemaProperty{Type: "number"}, nil
	case types.Bool:
		return &SchemaProperty{Type: "boolean"}, nil
	case types.Unknown, nil:
		return nil, fmt.Errorf("unresolved type")
	}
	switch typ := typ.(type) {
	case *types.EnumType:
		if typ == nil || typ.Declaration == nil {
			return nil, fmt.Errorf("unresolved enum")
		}
		if len(typ.Declaration.Variants) == 0 {
			return nil, fmt.Errorf("enum %s has no variants", typ.TypeName())
		}
		variants := make([]string, 0, len(typ.Declaration.Variants))
		for _, variant := range typ.Declaration.Variants {
			if variant.Payload != nil {
				return nil, fmt.Errorf("enum %s has a payload variant", typ.TypeName())
			}
			variants = append(variants, variant.Name)
		}
		return &SchemaProperty{Type: "string", Enum: variants}, nil
	case *types.NamedType:
		if typ != nil && typ.Declaration != nil {
			// Record properties are outside the current tool input projection.
			return &SchemaProperty{Type: "object"}, nil
		}
	case *types.OptionalType:
		if typ != nil {
			return TypeToSchema(typ.Inner)
		}
	case *types.ListType:
		if typ == nil {
			return nil, fmt.Errorf("unresolved list type")
		}
		elem, err := TypeToSchema(typ.Element)
		if err != nil {
			return nil, fmt.Errorf("list element: %w", err)
		}
		return &SchemaProperty{Type: "array", Items: elem}, nil
	}
	return nil, fmt.Errorf("unsupported parameter type (%T)", typ)
}

// supportedParameterSyntax detects type arguments the current checker can
// erase. Schema types still come exclusively from the checked signature.
func supportedParameterSyntax(expr ast.TypeExpr, typ types.Type) bool {
	switch expr := expr.(type) {
	case *ast.NamedTypeExpr:
		if len(expr.TypeArgs) != 0 {
			list, ok := typ.(*types.ListType)
			return ok && list != nil && len(expr.TypeArgs) == 1 && supportedParameterSyntax(expr.TypeArgs[0], list.Element)
		}
	case *ast.OptionalTypeExpr:
		if optional, ok := typ.(*types.OptionalType); ok && optional != nil {
			return supportedParameterSyntax(expr.Inner, optional.Inner)
		}
	}
	return true
}
