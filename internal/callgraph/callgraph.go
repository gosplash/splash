// Package callgraph builds a directed call graph from a Splash AST and computes reachability.
package callgraph

import (
	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/effects"
)

// Node represents a single function in the call graph.
type Node struct {
	Name        string
	Effects     effects.EffectSet
	Annotations []ast.Annotation
	callees     map[string]bool // direct callees by name
}

// Calls reports whether this node directly calls the named function.
func (n *Node) Calls(name string) bool { return n.callees[name] }

// HasAnnotation reports whether this node has the given annotation kind.
func (n *Node) HasAnnotation(kind ast.AnnotationKind) bool {
	for _, a := range n.Annotations {
		if a.Kind == kind {
			return true
		}
	}
	return false
}

// Graph is the complete call graph for a file.
type Graph struct {
	nodes map[string]*Node
}

// Node returns the node for the named function, or nil if not found.
func (g *Graph) Node(name string) *Node { return g.nodes[name] }

// AgentRoots returns the names of all agent entry-point functions:
// those with the Agent effect or annotated with @tool.
func (g *Graph) AgentRoots() []string {
	var roots []string
	for name, node := range g.nodes {
		if node.Effects.Has(effects.Agent) || node.HasAnnotation(ast.AnnotTool) {
			roots = append(roots, name)
		}
	}
	return roots
}

// Reachable performs a BFS from the given root function names and returns
// the set of all transitively reachable function names (roots included).
func (g *Graph) Reachable(roots []string) map[string]bool {
	visited := make(map[string]bool)
	queue := append([]string(nil), roots...)
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if visited[cur] {
			continue
		}
		visited[cur] = true
		node := g.nodes[cur]
		if node == nil {
			continue
		}
		for callee := range node.callees {
			if !visited[callee] {
				queue = append(queue, callee)
			}
		}
	}
	return visited
}

// Build constructs a call graph by walking all function declarations in file.
func Build(file *ast.File) *Graph {
	g := &Graph{nodes: make(map[string]*Node)}

	// First pass: register all function nodes.
	for _, decl := range file.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok {
			continue
		}
		g.nodes[fn.Name] = &Node{
			Name:        fn.Name,
			Effects:     effects.Parse(fn.Effects),
			Annotations: fn.Annotations,
			callees:     make(map[string]bool),
		}
	}

	// Second pass: walk bodies to collect call edges.
	for _, decl := range file.Declarations {
		fn, ok := decl.(*ast.FunctionDecl)
		if !ok || fn.Body == nil {
			continue
		}
		node := g.nodes[fn.Name]
		collectCalls(fn.Body, node)
	}

	return g
}

// collectCalls walks a block statement and records all direct CallExpr targets.
func collectCalls(block *ast.BlockStmt, node *Node) {
	if block == nil {
		return
	}
	for _, stmt := range block.Stmts {
		walkStmt(stmt, node)
	}
}

func walkStmt(stmt ast.Stmt, node *Node) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		if s.Value != nil {
			walkExpr(s.Value, node)
		}
	case *ast.LetStmt:
		walkExpr(s.Value, node)
	case *ast.ExprStmt:
		walkExpr(s.Expr, node)
	case *ast.AssignStmt:
		walkExpr(s.Target, node)
		walkExpr(s.Value, node)
	case *ast.IfStmt:
		walkExpr(s.Cond, node)
		collectCalls(s.Then, node)
		if s.Else != nil {
			walkStmt(s.Else, node)
		}
	case *ast.GuardStmt:
		walkExpr(s.Cond, node)
		collectCalls(s.Else, node)
	case *ast.ForStmt:
		walkExpr(s.Iter, node)
		collectCalls(s.Body, node)
	case *ast.BlockStmt:
		collectCalls(s, node)
	}
}

func walkExpr(expr ast.Expr, node *Node) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.CallExpr:
		// Record the direct callee name if it's a simple identifier.
		if ident, ok := e.Callee.(*ast.Ident); ok {
			node.callees[ident.Name] = true
		}
		walkExpr(e.Callee, node)
		for _, arg := range e.Args {
			walkExpr(arg, node)
		}
	case *ast.BinaryExpr:
		walkExpr(e.Left, node)
		walkExpr(e.Right, node)
	case *ast.UnaryExpr:
		walkExpr(e.Operand, node)
	case *ast.MemberExpr:
		walkExpr(e.Object, node)
	case *ast.IndexExpr:
		walkExpr(e.Object, node)
		walkExpr(e.Index, node)
	case *ast.NullCoalesceExpr:
		walkExpr(e.Left, node)
		walkExpr(e.Right, node)
	case *ast.ListLiteral:
		for _, el := range e.Elements {
			walkExpr(el, node)
		}
	case *ast.StructLiteral:
		for _, f := range e.Fields {
			walkExpr(f.Value, node)
		}
	}
}
