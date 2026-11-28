// Package toolschema generates JSON Schema tool definitions from tool-marked
// Splash function declarations. Use Serialize() with FormatAnthropic or FormatOpenAI
// to target specific API wire formats.
package toolschema

import (
	"encoding/json"
	"fmt"
	"gosplash.dev/splash/internal/ast"
)

// ToolSchema is the complete schema for a single tool function.
type ToolSchema struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema InputSchema `json:"input_schema"`
	Effects     []string    `json:"effects,omitempty"`
}

// InputSchema is the parameter schema for a tool. Always type "object".
type InputSchema struct {
	Type       string                     `json:"type"`
	Properties map[string]*SchemaProperty `json:"properties"`
	Required   []string                   `json:"required,omitempty"`
}

// SchemaProperty is a single value in a JSON Schema object.
type SchemaProperty struct {
	Type        string                     `json:"type,omitempty"`
	Description string                     `json:"description,omitempty"`
	Items       *SchemaProperty            `json:"items,omitempty"`
	Properties  map[string]*SchemaProperty `json:"properties,omitempty"`
	Enum        []string                   `json:"enum,omitempty"`
}

// Format controls the JSON wire format emitted by Serialize.
type Format string

const (
	// FormatAnthropic emits the Anthropic tool-calling format:
	// [{name, description, input_schema: {type, properties, required}}]
	FormatAnthropic Format = "anthropic"

	// FormatOpenAI emits the OpenAI tool-calling format:
	// [{type: "function", function: {name, description, parameters: {type, properties, required}}}]
	FormatOpenAI Format = "openai"
)

// openAITool is the top-level wrapper in the OpenAI tools array.
type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

// openAIFunction is the inner object in the OpenAI tool wrapper.
type openAIFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  InputSchema `json:"parameters"`
	Effects     []string    `json:"effects,omitempty"`
}

// Extract returns a ToolSchema for every tool function in file.
func Extract(file *ast.File) []ToolSchema {
	enumDecls := buildEnumIndex(file)
	var tools []ToolSchema
	for _, decl := range file.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		if !hasAnnotation(fn.Annotations, ast.AnnotTool) {
			continue
		}
		tools = append(tools, buildToolSchema(fn, enumDecls))
	}
	return tools
}

// ExtractReachable returns a ToolSchema for every tool function
// that appears in the agent-reachable set. Functions excluded by redline or
// @containment will not be in agentReachable and are silently omitted — their
// absence from the output is the guarantee.
func ExtractReachable(file *ast.File, agentReachable map[string]bool) []ToolSchema {
	enumDecls := buildEnumIndex(file)
	var tools []ToolSchema
	for _, decl := range file.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		if !hasAnnotation(fn.Annotations, ast.AnnotTool) {
			continue
		}
		if !agentReachable[fn.Name] {
			continue
		}
		tools = append(tools, buildToolSchema(fn, enumDecls))
	}
	return tools
}

func hasAnnotation(anns []ast.Annotation, kind ast.AnnotationKind) bool {
	for _, a := range anns {
		if a.Kind == kind {
			return true
		}
	}
	return false
}

func buildToolSchema(fn *ast.FunctionDecl, enumDecls map[string]*ast.EnumDecl) ToolSchema {
	props := make(map[string]*SchemaProperty)
	var required []string

	for _, p := range fn.Params {
		prop := typeExprToSchema(p.Type, enumDecls)
		if p.Doc != "" {
			prop.Description = p.Doc
		}
		props[p.Name] = prop
		// Optional params (T?) and params with defaults are not in required[]
		if _, isOptional := p.Type.(*ast.OptionalTypeExpr); !isOptional && p.Default == nil {
			required = append(required, p.Name)
		}
	}

	var effects []string
	for _, e := range fn.Effects {
		effects = append(effects, e.Name)
	}

	return ToolSchema{
		Name:        fn.Name,
		Description: fn.Doc,
		InputSchema: InputSchema{
			Type:       "object",
			Properties: props,
			Required:   required,
		},
		Effects: effects,
	}
}

// Serialize marshals schemas to JSON in the requested format.
// Returns an error for unrecognized format values.
func Serialize(schemas []ToolSchema, format Format) ([]byte, error) {
	switch format {
	case FormatAnthropic:
		return json.MarshalIndent(schemas, "", "  ")
	case FormatOpenAI:
		tools := make([]openAITool, len(schemas))
		for i, s := range schemas {
			tools[i] = openAITool{
				Type: "function",
				Function: openAIFunction{
					Name:        s.Name,
					Description: s.Description,
					Parameters:  s.InputSchema,
					Effects:     s.Effects,
				},
			}
		}
		return json.MarshalIndent(tools, "", "  ")
	default:
		return nil, fmt.Errorf("unknown format %q: use %q or %q", format, FormatAnthropic, FormatOpenAI)
	}
}
