// Package toolschema generates JSON Schema tool definitions from @tool-annotated
// Splash function declarations. Output is compatible with the Anthropic and
// OpenAI tool-calling APIs.
package toolschema

import "gosplash.dev/splash/internal/ast"

// ToolSchema is the complete schema for a single @tool function.
type ToolSchema struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema InputSchema `json:"input_schema"`
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

// Extract returns a ToolSchema for every @tool-annotated function in file.
// (Stub — implemented in Task 4.)
func Extract(file *ast.File) []ToolSchema {
	return nil
}
