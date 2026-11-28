package compiler

import (
	"cmp"
	"fmt"
	"maps"
	"slices"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/effects"
	"gosplash.dev/splash/internal/types"
)

type ComparisonStatus string

const (
	ComparisonUnchanged     ComparisonStatus = "unchanged"
	ComparisonChanged       ComparisonStatus = "changed"
	ComparisonIndeterminate ComparisonStatus = "indeterminate"
)

// AuthorityFunctionID identifies a declaration across captures of similarly
// laid-out source trees. Moving or renaming a declaration changes its ID.
type AuthorityFunctionID struct {
	Path   string `json:"path"`
	Module string `json:"module"`
	Name   string `json:"name"`
}

type AuthorityDeclarationLocation struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type DeclaredAuthorityChange struct {
	Function AuthorityFunctionID           `json:"function"`
	Kind     string                        `json:"kind"`
	Before   *AuthorityDeclarationLocation `json:"before"`
	After    *AuthorityDeclarationLocation `json:"after"`
	Added    []string                      `json:"added"`
	Removed  []string                      `json:"removed"`
}

type PublicSignatureChange struct {
	Function AuthorityFunctionID `json:"function"`
	Kind     string              `json:"kind"`
	Before   *Signature          `json:"before"`
	After    *Signature          `json:"after"`
	Reason   string              `json:"reason,omitempty"`
}

type DeclaredAuthorityComparison struct {
	Status  ComparisonStatus          `json:"status"`
	Changes []DeclaredAuthorityChange `json:"changes"`
}

type PublicSignatureComparison struct {
	Status   ComparisonStatus        `json:"status"`
	Complete bool                    `json:"complete"`
	Changes  []PublicSignatureChange `json:"changes"`
}

// AuthorityComparison describes differences in modeled source properties.
type AuthorityComparison struct {
	DeclaredAuthority DeclaredAuthorityComparison `json:"declaredAuthority"`
	PublicSignatures  PublicSignatureComparison   `json:"publicSignatures"`
}

type authorityFunctionFacts struct {
	location *AuthorityDeclarationLocation
	declared effects.EffectSet
	public   *authorityPublicFunction
}

type authorityPublicFunction struct {
	shape       Signature
	checkedType *types.FunctionType
}

func (f *authorityPublicFunction) displaySignature() *Signature {
	if f == nil {
		return nil
	}
	f.shape.setDisplayTypes(f.checkedType)
	return &f.shape
}

// CompareAuthority compares two captures verified by this compiler. The
// comparison reports modeled declarations and signatures for human review.
func CompareAuthority(base, head *VerifiedProgram) (AuthorityComparison, error) {
	if base == nil || base.verified == nil || head == nil || head.verified == nil {
		return AuthorityComparison{}, ErrUnverifiedProgram
	}
	before, err := base.verified.authorityFacts()
	if err != nil {
		return AuthorityComparison{}, fmt.Errorf("base: %w", err)
	}
	after, err := head.verified.authorityFacts()
	if err != nil {
		return AuthorityComparison{}, fmt.Errorf("head: %w", err)
	}
	ids := make(map[AuthorityFunctionID]struct{}, len(before))
	for id := range before {
		ids[id] = struct{}{}
	}
	for id := range after {
		ids[id] = struct{}{}
	}
	ordered := slices.SortedFunc(maps.Keys(ids), func(a, b AuthorityFunctionID) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Module, b.Module), cmp.Compare(a.Name, b.Name))
	})
	comparison := AuthorityComparison{
		DeclaredAuthority: DeclaredAuthorityComparison{Status: ComparisonUnchanged, Changes: []DeclaredAuthorityChange{}},
		PublicSignatures:  PublicSignatureComparison{Status: ComparisonUnchanged, Complete: true, Changes: []PublicSignatureChange{}},
	}
	for _, id := range ordered {
		old, next := before[id], after[id]
		if old.location == nil || next.location == nil || old.declared != next.declared {
			kind := "modified"
			if old.location == nil {
				kind = "added"
			} else if next.location == nil {
				kind = "removed"
			}
			comparison.DeclaredAuthority.Status = ComparisonChanged
			comparison.DeclaredAuthority.Changes = append(comparison.DeclaredAuthority.Changes, DeclaredAuthorityChange{
				Function: id, Kind: kind, Before: old.location, After: next.location,
				Added: (next.declared &^ old.declared).Labels(), Removed: (old.declared &^ next.declared).Labels(),
			})
		}
		comparison.PublicSignatures.add(id, old.public, next.public)
	}
	return comparison, nil
}

func (a *analysis) authorityFacts() (map[AuthorityFunctionID]authorityFunctionFacts, error) {
	summaries := make(map[*ast.FunctionDecl]*effects.Summary, len(a.effects))
	for _, summary := range a.effects {
		summaries[summary.Function] = summary
	}
	result := make(map[AuthorityFunctionID]authorityFunctionFacts, len(a.effects))
	for _, owner := range a.modules {
		var module string
		if owner.File.Module != nil {
			module = owner.File.Module.Name
		}
		exposes := make(map[string]bool, len(owner.File.Exposes))
		for _, name := range owner.File.Exposes {
			exposes[name] = true
		}
		for _, fact := range owner.EffectFacts {
			function := fact.Function
			pos := function.Pos()
			id := AuthorityFunctionID{Path: a.sourcePath(pos.File), Module: module, Name: function.Name}
			if _, exists := result[id]; exists {
				return nil, fmt.Errorf("ambiguous function declaration %q in %q", id.Name, id.Path)
			}
			summary := summaries[function]
			functionFacts := authorityFunctionFacts{
				location: &AuthorityDeclarationLocation{Line: pos.Line, Column: pos.Column},
				declared: summary.Declared,
			}
			if len(exposes) == 0 || exposes[function.Name] {
				fnType := checkedFunctionType(owner, function)
				functionFacts.public = &authorityPublicFunction{
					shape: a.functionSignatureShape(fnType, function), checkedType: fnType,
				}
			}
			result[id] = functionFacts
		}
	}
	return result, nil
}

func (c *PublicSignatureComparison) add(id AuthorityFunctionID, before, after *authorityPublicFunction) {
	if before == nil && after == nil {
		return
	}
	reason := before.uncertainty()
	if reason == "" {
		reason = after.uncertainty()
	}
	var kind string
	switch {
	case before == nil:
		kind = "added"
	case after == nil:
		kind = "removed"
	case knownSignatureDifference(&before.shape, &after.shape):
		kind = "modified"
	case reason != "":
		kind = "indeterminate"
	default:
		return
	}
	if reason != "" {
		c.Complete = false
	}
	if kind != "indeterminate" {
		c.Status = ComparisonChanged
	} else if c.Status == ComparisonUnchanged {
		c.Status = ComparisonIndeterminate
	}
	c.Changes = append(c.Changes, PublicSignatureChange{
		Function: id, Kind: kind, Before: before.displaySignature(), After: after.displaySignature(), Reason: reason,
	})
}

func (f *authorityPublicFunction) uncertainty() string {
	if f == nil {
		return ""
	}
	signature := &f.shape
	if len(signature.TypeParameters) != 0 {
		return "generic signature equivalence is outside this comparison"
	}
	if signature.ReturnTypeIdentity.Kind == "" {
		return "checked return type identity is unavailable"
	}
	for _, parameter := range signature.Parameters {
		if parameter.TypeIdentity.Kind == "" {
			return "checked parameter type identity is unavailable"
		}
	}
	return ""
}

func knownSignatureDifference(a, b *Signature) bool {
	if a.Async != b.Async || len(a.TypeParameters) != len(b.TypeParameters) ||
		differentKnownTypes(a.ReturnTypeIdentity, b.ReturnTypeIdentity) {
		return true
	}
	return !slices.EqualFunc(a.Parameters, b.Parameters, func(a, b Parameter) bool {
		return a.Name == b.Name && a.HasDefault == b.HasDefault && a.Variadic == b.Variadic &&
			!differentKnownTypes(a.TypeIdentity, b.TypeIdentity)
	})
}

// Compare established parts of a checked type independently. An unknown or
// symbolic leaf does not erase a concrete difference in another branch.
func differentKnownTypes(a, b TypeIdentity) bool {
	if a.Kind == "" || b.Kind == "" || a.Kind == "typeParameter" || b.Kind == "typeParameter" {
		return false
	}
	if a.Kind != b.Kind || a.Name != b.Name || a.Path != b.Path ||
		len(a.Arguments) != len(b.Arguments) || len(a.Parameters) != len(b.Parameters) {
		return true
	}
	for i, argument := range a.Arguments {
		if differentKnownTypes(argument, b.Arguments[i]) {
			return true
		}
	}
	for i, parameter := range a.Parameters {
		if differentKnownTypes(parameter, b.Parameters[i]) {
			return true
		}
	}
	return a.ReturnType != nil && b.ReturnType != nil && differentKnownTypes(*a.ReturnType, *b.ReturnType)
}
