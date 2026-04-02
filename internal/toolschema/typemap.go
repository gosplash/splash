package toolschema

import "gosplash.dev/splash/internal/ast"

// TypeExprToSchema converts a Splash type expression to a JSON Schema property.
// Exported so tests can call it directly.
// file provides context for resolving enum variants.
func TypeExprToSchema(te ast.TypeExpr, file *ast.File) *SchemaProperty {
	enumDecls := buildEnumIndex(file)
	return typeExprToSchema(te, enumDecls)
}

func typeExprToSchema(te ast.TypeExpr, enumDecls map[string]*ast.EnumDecl) *SchemaProperty {
	switch t := te.(type) {
	case *ast.NamedTypeExpr:
		return namedTypeToSchema(t, enumDecls)
	case *ast.OptionalTypeExpr:
		// Optional only affects the required array, not the schema shape.
		return typeExprToSchema(t.Inner, enumDecls)
	case *ast.FnTypeExpr:
		return &SchemaProperty{Type: "object"}
	}
	return &SchemaProperty{Type: "string"}
}

func namedTypeToSchema(t *ast.NamedTypeExpr, enumDecls map[string]*ast.EnumDecl) *SchemaProperty {
	switch t.Name {
	case "String":
		return &SchemaProperty{Type: "string"}
	case "Int":
		return &SchemaProperty{Type: "integer"}
	case "Float":
		return &SchemaProperty{Type: "number"}
	case "Bool":
		return &SchemaProperty{Type: "boolean"}
	case "List":
		if len(t.TypeArgs) == 1 {
			return &SchemaProperty{
				Type:  "array",
				Items: typeExprToSchema(t.TypeArgs[0], enumDecls),
			}
		}
		return &SchemaProperty{Type: "array"}
	}
	if decl, ok := enumDecls[t.Name]; ok {
		var variants []string
		for _, v := range decl.Variants {
			variants = append(variants, v.Name)
		}
		return &SchemaProperty{Type: "string", Enum: variants}
	}
	return &SchemaProperty{Type: "object"}
}

func buildEnumIndex(file *ast.File) map[string]*ast.EnumDecl {
	decls := make(map[string]*ast.EnumDecl)
	if file == nil {
		return decls
	}
	for _, d := range file.Declarations {
		if e, ok := d.(*ast.EnumDecl); ok {
			decls[e.Name] = e
		}
	}
	return decls
}
