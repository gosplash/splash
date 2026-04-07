// Package callgraph builds a directed call graph from a Splash AST and computes reachability.
package callgraph

import (
	"slices"

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

// Nodes returns all nodes sorted by function name.
func (g *Graph) Nodes() []*Node {
	names := make([]string, 0, len(g.nodes))
	for name := range g.nodes {
		names = append(names, name)
	}
	slices.Sort(names)

	nodes := make([]*Node, 0, len(names))
	for _, name := range names {
		nodes = append(nodes, g.nodes[name])
	}
	return nodes
}

// Callees returns the node's direct callees sorted by function name.
func (n *Node) Callees() []string {
	callees := make([]string, 0, len(n.callees))
	for name := range n.callees {
		callees = append(callees, name)
	}
	slices.Sort(callees)
	return callees
}

// AgentRoots returns the names of all agent entry-point functions:
// those with the Agent effect or marked as tool functions.
func (g *Graph) AgentRoots() []string {
	var roots []string
	for name, node := range g.nodes {
		if node.Effects.Has(effects.Agent) || node.HasAnnotation(ast.AnnotTool) {
			roots = append(roots, name)
		}
	}
	slices.Sort(roots)
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

// Callers returns the set of all functions that transitively call any function
// in targets. The targets themselves are included in the result.
// This is reverse reachability: BFS backwards through the call graph.
func (g *Graph) Callers(targets map[string]bool) map[string]bool {
	// Build reverse adjacency: callee → []callers
	reverse := make(map[string][]string)
	for name, node := range g.nodes {
		for callee := range node.callees {
			reverse[callee] = append(reverse[callee], name)
		}
	}
	// BFS backwards from every target
	visited := make(map[string]bool)
	queue := make([]string, 0, len(targets))
	for name := range targets {
		queue = append(queue, name)
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if visited[cur] {
			continue
		}
		visited[cur] = true
		for _, caller := range reverse[cur] {
			if !visited[caller] {
				queue = append(queue, caller)
			}
		}
	}
	return visited
}

// Parents performs a BFS from the given root function names and returns a
// spanning-tree parent map: each reachable function name maps to the name of
// the function that first reached it. Roots map to the empty string.
func (g *Graph) Parents(roots []string) map[string]string {
	parents := make(map[string]string)
	queue := append([]string(nil), roots...)
	for _, r := range roots {
		parents[r] = ""
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		node := g.nodes[cur]
		if node == nil {
			continue
		}
		for callee := range node.callees {
			if _, seen := parents[callee]; !seen {
				parents[callee] = cur
				queue = append(queue, callee)
			}
		}
	}
	return parents
}

// PathTo reconstructs the call path from an agent root to target using the
// parent map returned by Parents. Returns nil if target is not reachable.
func PathTo(parents map[string]string, target string) []string {
	if _, ok := parents[target]; !ok {
		return nil
	}
	var path []string
	for cur := target; ; {
		path = append(path, cur)
		p := parents[cur]
		if p == "" {
			break
		}
		cur = p
	}
	// reverse: path was built from target back to root
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
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
