// Package callgraph builds a directed call graph from a Splash AST and computes reachability.
package callgraph

import (
	"fmt"
	"slices"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/effects"
	"gosplash.dev/splash/internal/token"
)

// CallSite describes a resolved invocation that consumes its value as part of
// a larger expression or assignment.
type CallSite struct {
	Target   string
	Position token.Position
	End      token.Position
}

// Node represents a single function in the call graph.
type Node struct {
	Name            string
	Effects         effects.EffectSet
	Annotations     []ast.Annotation
	UnresolvedCalls []token.Position
	NestedCalls     []CallSite
	callees         map[string]bool // direct callees by graph key
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
	nodes         map[string]*Node
	functionKeys  map[*ast.FunctionDecl]string
	hasUnresolved bool
}

// DeclaredEffects is the union of the declared effects on all graph nodes.
func (g *Graph) DeclaredEffects() effects.EffectSet {
	var declared effects.EffectSet
	for _, node := range g.nodes {
		declared |= node.Effects
	}
	return declared
}

// HasUnresolvedCalls reports whether any analyzed call lacked a named target.
func (g *Graph) HasUnresolvedCalls() bool { return g.hasUnresolved }

// FunctionKey returns the graph node key for the exact declaration.
func (g *Graph) FunctionKey(fn *ast.FunctionDecl) string { return g.functionKeys[fn] }

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

// ApprovalFunctions returns graph keys of approval-gated declarations.
func (g *Graph) ApprovalFunctions() map[string]bool {
	fns := make(map[string]bool)
	for name, node := range g.nodes {
		if node.HasAnnotation(ast.AnnotApprove) {
			fns[name] = true
		}
	}
	return fns
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
	tree := g.ShortestPaths(roots)
	parents := make(map[string]string, len(tree))
	for name, link := range tree {
		parents[name] = link.parent
	}
	return parents
}

type pathLink struct {
	parent string
	root   string
	length int
}

// PathTree records shortest paths from a set of roots. Lengths count both
// endpoints, and ties follow the supplied root order and sorted callee order.
type PathTree map[string]pathLink

// ShortestPaths builds a breadth-first tree rooted at the supplied functions.
func (g *Graph) ShortestPaths(roots []string) PathTree {
	tree := make(PathTree)
	queue := append([]string(nil), roots...)
	for _, root := range roots {
		tree[root] = pathLink{root: root, length: 1}
	}
	for head := 0; head < len(queue); head++ {
		cur := queue[head]
		node := g.nodes[cur]
		if node == nil {
			continue
		}
		link := tree[cur]
		for _, callee := range node.Callees() {
			if _, seen := tree[callee]; !seen {
				tree[callee] = pathLink{parent: cur, root: link.root, length: link.length + 1}
				queue = append(queue, callee)
			}
		}
	}
	return tree
}

// PathExcerpt returns the root and the suffix nearest target, plus the full
// length. The caller supplies a positive limit. An unreachable target has
// length zero.
func (tree PathTree) PathExcerpt(target string, limit int) ([]string, int) {
	link, ok := tree[target]
	if !ok {
		return nil, 0
	}
	path := make([]string, min(link.length, limit))
	path[0] = link.root
	for i := len(path) - 1; i > 0; i-- {
		path[i] = target
		target = tree[target].parent
	}
	return path, link.length
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

// Build walks a parsed AST for callers that do not have a type checker result.
func Build(file *ast.File) *Graph {
	return build([]*ast.File{file}, func(call *ast.CallExpr, _ *Graph) (string, bool) {
		if ident, ok := call.Callee.(*ast.Ident); ok {
			return ident.Name, false
		}
		return "", false
	})
}

// BuildResolved builds nodes from the owning files and uses only the call
// targets established by the type checker. Indirect calls produce no named edge.
func BuildResolved(files []*ast.File, references map[ast.Expr]*ast.FunctionDecl, unresolved map[*ast.CallExpr]bool) *Graph {
	return build(files, func(call *ast.CallExpr, g *Graph) (string, bool) {
		missing := unresolved[call]
		g.hasUnresolved = g.hasUnresolved || missing
		return g.FunctionKey(references[call.Callee]), missing
	})
}

func build(files []*ast.File, resolve func(*ast.CallExpr, *Graph) (string, bool)) *Graph {
	g := &Graph{
		nodes:        make(map[string]*Node),
		functionKeys: make(map[*ast.FunctionDecl]string),
	}
	counts := make(map[string]int)
	for _, file := range files {
		for _, decl := range file.Declarations {
			if fn, ok := decl.(*ast.FunctionDecl); ok {
				counts[fn.Name]++
			}
		}
	}

	for _, file := range files {
		for _, decl := range file.Declarations {
			fn, ok := decl.(*ast.FunctionDecl)
			if !ok {
				continue
			}
			key := fn.Name
			if counts[fn.Name] > 1 {
				if file.Module != nil {
					key = file.Module.Name + "." + key
				}
				if g.nodes[key] != nil {
					prefix := key
					for n := 2; g.nodes[key] != nil; n++ {
						key = fmt.Sprintf("%s#%d", prefix, n)
					}
				}
			}
			g.functionKeys[fn] = key
			g.nodes[key] = &Node{
				Name:        key,
				Effects:     effects.Parse(fn.Effects),
				Annotations: fn.Annotations,
				callees:     make(map[string]bool),
			}
		}
	}

	target := func(call *ast.CallExpr) (string, bool) { return resolve(call, g) }
	for _, file := range files {
		for _, decl := range file.Declarations {
			fn, ok := decl.(*ast.FunctionDecl)
			if ok && fn.Body != nil {
				collectCalls(fn.Body, g.nodes[g.FunctionKey(fn)], target)
			}
		}
	}

	return g
}

// collectCalls walks a block statement and records all direct CallExpr targets.
func collectCalls(block *ast.BlockStmt, node *Node, target func(*ast.CallExpr) (string, bool)) {
	if block == nil {
		return
	}
	for _, stmt := range block.Stmts {
		walkStmt(stmt, node, target)
	}
}

func walkStmt(stmt ast.Stmt, node *Node, target func(*ast.CallExpr) (string, bool)) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		walkExprContext(s.Value, node, target, true)
	case *ast.LetStmt:
		walkExprContext(s.Value, node, target, true)
	case *ast.ExprStmt:
		walkExprContext(s.Expr, node, target, true)
	case *ast.AssignStmt:
		walkExpr(s.Target, node, target)
		walkExpr(s.Value, node, target)
	case *ast.IfStmt:
		walkExpr(s.Cond, node, target)
		collectCalls(s.Then, node, target)
		if s.Else != nil {
			walkStmt(s.Else, node, target)
		}
	case *ast.GuardStmt:
		walkExpr(s.Cond, node, target)
		collectCalls(s.Else, node, target)
	case *ast.ForStmt:
		walkExpr(s.Iter, node, target)
		collectCalls(s.Body, node, target)
	case *ast.BlockStmt:
		collectCalls(s, node, target)
	}
}

func walkExpr(expr ast.Expr, node *Node, target func(*ast.CallExpr) (string, bool)) {
	walkExprContext(expr, node, target, false)
}

func walkExprContext(expr ast.Expr, node *Node, target func(*ast.CallExpr) (string, bool), wholeStatementValue bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.CallExpr:
		key, unresolved := target(e)
		if key != "" {
			node.callees[key] = true
			if !wholeStatementValue {
				node.NestedCalls = append(node.NestedCalls, CallSite{Target: key, Position: e.Pos(), End: e.End})
			}
		}
		if unresolved {
			node.UnresolvedCalls = append(node.UnresolvedCalls, e.Pos())
		}
		walkExpr(e.Callee, node, target)
		for _, arg := range e.Args {
			walkExpr(arg, node, target)
		}
	case *ast.BinaryExpr:
		walkExpr(e.Left, node, target)
		walkExpr(e.Right, node, target)
	case *ast.UnaryExpr:
		walkExpr(e.Operand, node, target)
	case *ast.MemberExpr:
		walkExpr(e.Object, node, target)
	case *ast.IndexExpr:
		walkExpr(e.Object, node, target)
		walkExpr(e.Index, node, target)
	case *ast.NullCoalesceExpr:
		walkExpr(e.Left, node, target)
		walkExpr(e.Right, node, target)
	case *ast.ListLiteral:
		for _, el := range e.Elements {
			walkExpr(el, node, target)
		}
	case *ast.StructLiteral:
		for _, f := range e.Fields {
			walkExpr(f.Value, node, target)
		}
	}
}
