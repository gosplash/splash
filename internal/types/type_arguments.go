package types

import "gosplash.dev/splash/internal/ast"

// RetainsTypeArguments reports whether the checked type represents every
// explicit type application in a data type. The frontend currently retains
// applications for List and Result and can erase other applications.
func RetainsTypeArguments(expr ast.TypeExpr, checked Type) bool {
	switch expr := expr.(type) {
	case *ast.NamedTypeExpr:
		args := expr.TypeArgs
		if len(args) == 0 {
			return true
		}
		switch checked := checked.(type) {
		case *ListType:
			return checked != nil && len(args) == 1 && RetainsTypeArguments(args[0], checked.Element)
		case *ResultType:
			return checked != nil && len(args) == 2 &&
				RetainsTypeArguments(args[0], checked.Ok) && RetainsTypeArguments(args[1], checked.Err)
		default:
			return false
		}
	case *ast.OptionalTypeExpr:
		optional, ok := checked.(*OptionalType)
		return ok && optional != nil && RetainsTypeArguments(expr.Inner, optional.Inner)
	case *ast.FnTypeExpr:
		function, ok := checked.(*FunctionType)
		if !ok || function == nil || len(function.Params) != len(expr.Params) {
			return false
		}
		for i, param := range expr.Params {
			if !RetainsTypeArguments(param, function.Params[i]) {
				return false
			}
		}
		return RetainsTypeArguments(expr.ReturnType, function.Return)
	default:
		return true
	}
}
