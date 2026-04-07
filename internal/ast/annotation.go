package ast

import "gosplash.dev/splash/internal/token"

type AnnotationKind int

const (
	AnnotRedline AnnotationKind = iota
	AnnotApprove
	AnnotContainment
	AnnotAgentAllowed
	AnnotSandbox
	AnnotBudget
	AnnotCapabilityDecay
	AnnotSensitive
	AnnotRestricted
	AnnotInternal
	AnnotAudit
	AnnotTool
	AnnotTrace
	AnnotDeadline
	AnnotRoute
	AnnotTest
	AnnotDeploy
)

var annotationNames = map[string]AnnotationKind{
	"containment":      AnnotContainment,
	"agent_allowed":    AnnotAgentAllowed,
	"sandbox":          AnnotSandbox,
	"budget":           AnnotBudget,
	"capability_decay": AnnotCapabilityDecay,
	"sensitive":        AnnotSensitive,
	"restricted":       AnnotRestricted,
	"internal":         AnnotInternal,
	"audit":            AnnotAudit,
	"trace":            AnnotTrace,
	"deadline":         AnnotDeadline,
	"route":            AnnotRoute,
	"test":             AnnotTest,
	"deploy":           AnnotDeploy,
}

func LookupAnnotation(name string) (AnnotationKind, bool) {
	k, ok := annotationNames[name]
	return k, ok
}

type Annotation struct {
	Kind AnnotationKind
	Args map[string]Expr
	Pos  token.Position
}
