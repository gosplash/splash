// Package toolschema generates JSON Schema tool definitions from tool-marked
// Splash function declarations. Use Serialize() with FormatAnthropic or FormatOpenAI
// to target specific API wire formats.
package toolschema

import (
	"encoding/json"
	"fmt"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/typechecker"
	"gosplash.dev/splash/internal/types"
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

// Extract projects tool schemas from the declarations and callable signatures
// retained by the owning checked modules.
func Extract(modules []*typechecker.TypedFile) ([]ToolSchema, error) {
	tools := []ToolSchema{}
	for _, module := range modules {
		for _, decl := range module.File.Declarations {
			fn, ok := decl.(*ast.FunctionDecl)
			if !ok || !hasAnnotation(fn.Annotations, ast.AnnotTool) {
				continue
			}
			signature, ok := module.Types[fn].(*types.FunctionType)
			if !ok || signature == nil || len(signature.Params) != len(fn.Params) {
				return nil, fmt.Errorf("%s: tool %q has no checked signature", fn.Pos(), fn.Name)
			}
			schema, err := buildToolSchema(fn, signature)
			if err != nil {
				return nil, err
			}
			tools = append(tools, schema)
		}
	}
	return tools, nil
}

func hasAnnotation(anns []ast.Annotation, kind ast.AnnotationKind) bool {
	for _, a := range anns {
		if a.Kind == kind {
			return true
		}
	}
	return false
}

func buildToolSchema(fn *ast.FunctionDecl, signature *types.FunctionType) (ToolSchema, error) {
	props := make(map[string]*SchemaProperty)
	var required []string

	for i, p := range fn.Params {
		paramType := signature.Params[i]
		if !types.RetainsTypeArguments(p.Type, paramType) {
			return ToolSchema{}, fmt.Errorf("%s: tool %q parameter %q has unsupported type arguments", p.Pos, fn.Name, p.Name)
		}
		prop, err := TypeToSchema(paramType)
		if err != nil {
			return ToolSchema{}, fmt.Errorf("%s: tool %q parameter %q: %w", p.Pos, fn.Name, p.Name, err)
		}
		if p.Doc != "" {
			prop.Description = p.Doc
		}
		props[p.Name] = prop
		// Optional params (T?) and params with defaults are not in required[]
		if _, isOptional := paramType.(*types.OptionalType); !isOptional && p.Default == nil {
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
	}, nil
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
