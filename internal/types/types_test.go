package types_test

import (
	"testing"

	"gosplash.dev/splash/internal/types"
)

func TestOptionalAssignability(t *testing.T) {
	str := types.String
	opt := &types.OptionalType{Inner: types.String}

	if !str.IsAssignableTo(opt) {
		t.Error("String should be assignable to String?")
	}
	if opt.IsAssignableTo(str) {
		t.Error("String? should not be assignable to String")
	}
}

func TestResultType(t *testing.T) {
	r := &types.ResultType{Ok: types.String, Err: types.Named("AppError")}
	if r.TypeName() != "Result<String, AppError>" {
		t.Errorf("got %q", r.TypeName())
	}
}

func TestClassificationOrder(t *testing.T) {
	if types.ClassSensitive <= types.ClassInternal {
		t.Error("Sensitive should be higher classification than Internal")
	}
	if types.ClassRestricted <= types.ClassSensitive {
		t.Error("Restricted should be highest classification")
	}
}

func TestNamedTypeClassification(t *testing.T) {
	nt := &types.NamedType{
		Name: "User",
		FieldClassifications: []types.Classification{
			types.ClassPublic,
			types.ClassSensitive,
		},
	}
	if nt.Classification() != types.ClassSensitive {
		t.Errorf("expected Sensitive, got %v", nt.Classification())
	}
}
