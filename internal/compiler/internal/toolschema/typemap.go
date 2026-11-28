package toolschema

import (
	"fmt"

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
		if typ.Declaration == nil {
			return nil, fmt.Errorf("unresolved enum")
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
		if typ.Declaration != nil {
			// Record properties are outside the current tool input projection.
			return &SchemaProperty{Type: "object"}, nil
		}
	case *types.OptionalType:
		return TypeToSchema(typ.Inner)
	case *types.ListType:
		elem, err := TypeToSchema(typ.Element)
		if err != nil {
			return nil, fmt.Errorf("list element: %w", err)
		}
		return &SchemaProperty{Type: "array", Items: elem}, nil
	}
	return nil, fmt.Errorf("unsupported type %s", typ.TypeName())
}
