// Package parser implements a Pratt parser for the Splash language.
package parser

import (
	"strconv"
	"strings"

	"gosplash.dev/splash/internal/ast"
	"gosplash.dev/splash/internal/diagnostic"
	"gosplash.dev/splash/internal/token"
)

// precedence levels for Pratt parsing
type precedence int

const (
	precLowest  precedence = iota
	precAssign             // =
	precOr                 // ||
	precAnd                // &&
	precEqual              // == !=
	precCompare            // < <= > >=
	precAdd                // + - ??
	precMul                // * / %
	precUnary              // ! -
	precCall               // f()
	precMember             // . ?.
)

var tokenPrecedence = map[token.Kind]precedence{
	token.ASSIGN:    precAssign,
	token.OR_OR:     precOr,
	token.AND_AND:   precAnd,
	token.EQ:        precEqual,
	token.NEQ:       precEqual,
	token.LT:        precCompare,
	token.LTE:       precCompare,
	token.GT:        precCompare,
	token.GTE:       precCompare,
	token.NULL_COAL: precAdd,
	token.PLUS:      precAdd,
	token.MINUS:     precAdd,
	token.STAR:      precMul,
	token.SLASH:     precMul,
	token.PERCENT:   precMul,
	token.LPAREN:    precCall,
	token.LBRACKET:  precCall,
	token.DOT:       precMember,
	token.OPT_CHAIN: precMember,
}

// Parser holds state for parsing a token stream into an AST.
type Parser struct {
	filename string
	tokens   []token.Token
	pos      int
	diags    []diagnostic.Diagnostic
}

// New creates a new Parser for the given token stream.
func New(filename string, tokens []token.Token) *Parser {
	return &Parser{
		filename: filename,
		tokens:   tokens,
		pos:      0,
	}
}

// current returns the token at the current position.
func (p *Parser) current() token.Token {
	if p.pos < len(p.tokens) {
		return p.tokens[p.pos]
	}
	return token.Token{Kind: token.EOF}
}

// peek returns the token one position ahead.
func (p *Parser) peek() token.Token {
	if p.pos+1 < len(p.tokens) {
		return p.tokens[p.pos+1]
	}
	return token.Token{Kind: token.EOF}
}

// advance moves to the next token and returns the previous one.
func (p *Parser) advance() token.Token {
	tok := p.current()
	p.pos++
	return tok
}

// check returns true if the current token is of the given kind.
func (p *Parser) check(k token.Kind) bool {
	return p.current().Kind == k
}

// eat advances if the current token matches and returns it; otherwise emits an error.
func (p *Parser) eat(k token.Kind) token.Token {
	if p.check(k) {
		return p.advance()
	}
	cur := p.current()
	p.errorf(cur.Pos, "expected %v, got %v (%q)", k, cur.Kind, cur.Literal)
	p.advance() // always advance to prevent infinite loops
	return cur
}

// errorf records a parse error diagnostic.
func (p *Parser) errorf(pos token.Position, format string, args ...any) {
	p.diags = append(p.diags, diagnostic.Errorf(pos, format, args...))
}

// sync advances past tokens until a top-level declaration keyword or EOF is found.
func (p *Parser) sync() {
	for {
		switch p.current().Kind {
		case token.FN, token.TYPE, token.ENUM, token.CONSTRAINT,
			token.MODULE, token.EXPOSE, token.USE, token.AT, token.RBRACE, token.SEMICOLON, token.EOF:
			return
		}
		p.advance()
	}
}

// isTopLevelKeyword returns true if the token starts a top-level construct.
func isTopLevelKeyword(k token.Kind) bool {
	switch k {
	case token.FN, token.TYPE, token.ENUM, token.CONSTRAINT,
		token.MODULE, token.EXPOSE, token.USE, token.AT, token.ASYNC, token.EOF:
		return true
	}
	return false
}

// ParseFile parses a full Splash file and returns the AST and any diagnostics.
func (p *Parser) ParseFile() (*ast.File, []diagnostic.Diagnostic) {
	file := &ast.File{}

	// collect any annotations that precede the module declaration
	var moduleAnnots []ast.Annotation
	for p.check(token.AT) {
		moduleAnnots = append(moduleAnnots, p.parseAnnotation())
	}

	// module declaration (required, must come first)
	if p.check(token.MODULE) {
		file.Module = p.parseModuleDecl()
		if file.Module != nil {
			file.Module.Annotations = moduleAnnots
		}
	} else if len(moduleAnnots) > 0 {
		// annotations were collected but no module keyword followed
		for _, ann := range moduleAnnots {
			p.errorf(ann.Pos, "annotation before non-module declaration")
		}
	}

	// expose list
	if p.check(token.EXPOSE) {
		file.Exposes = p.parseExposeList()
	}

	// use declarations
	for p.check(token.USE) {
		file.Uses = append(file.Uses, p.parseUseDecl())
	}

	// top-level declarations
	for !p.check(token.EOF) {
		decl := p.parseTopLevelDecl()
		if decl != nil {
			file.Declarations = append(file.Declarations, decl)
		}
	}

	return file, p.diags
}

// parseModuleDecl parses: module <name>
func (p *Parser) parseModuleDecl() *ast.ModuleDecl {
	pos := p.current().Pos
	p.eat(token.MODULE)
	name := p.eat(token.IDENT)
	return &ast.ModuleDecl{
		Name:     name.Literal,
		Position: pos,
	}
}

// parseExposeList parses: expose name1, name2, ...
func (p *Parser) parseExposeList() []string {
	p.eat(token.EXPOSE)
	var names []string
	for {
		if !p.check(token.IDENT) {
			break
		}
		names = append(names, p.advance().Literal)
		if !p.check(token.COMMA) {
			break
		}
		p.advance() // consume comma
	}
	return names
}

// parseUseDecl parses: use path/to/module [as alias]
func (p *Parser) parseUseDecl() *ast.UseDecl {
	pos := p.current().Pos
	p.eat(token.USE)

	// path is slash-separated identifiers
	var parts []string
	if p.check(token.IDENT) {
		parts = append(parts, p.advance().Literal)
		for p.check(token.SLASH) {
			p.advance() // consume /
			if p.check(token.IDENT) {
				parts = append(parts, p.advance().Literal)
			}
		}
	}
	path := strings.Join(parts, "/")

	var alias string
	if p.check(token.IDENT) && p.current().Literal == "as" {
		p.advance() // consume "as"
		if p.check(token.IDENT) {
			alias = p.advance().Literal
		}
	}

	return &ast.UseDecl{
		Path:     path,
		Alias:    alias,
		Position: pos,
	}
}

// parseTopLevelDecl parses annotations then dispatches to the right declaration parser.
func (p *Parser) parseTopLevelDecl() ast.Decl {
	// collect leading annotations
	var annots []ast.Annotation
	for p.check(token.AT) {
		a := p.parseAnnotation()
		annots = append(annots, a)
	}

	switch p.current().Kind {
	case token.FN:
		return p.parseFunctionDecl(annots, false)
	case token.ASYNC:
		p.advance() // consume async
		return p.parseFunctionDecl(annots, true)
	case token.TYPE:
		return p.parseTypeDecl(annots)
	case token.ENUM:
		return p.parseEnumDecl(annots)
	case token.CONSTRAINT:
		return p.parseConstraintDecl(annots)
	case token.EOF:
		return nil
	default:
		cur := p.current()
		p.errorf(cur.Pos, "unexpected token %q at top level", cur.Literal)
		before := p.pos
		p.sync()
		if p.pos == before {
			p.advance() // sync didn't move; force past the stuck token
		}
		return nil
	}
}

// parseAnnotation parses: @name or @name(key: value, ...)
func (p *Parser) parseAnnotation() ast.Annotation {
	pos := p.current().Pos
	p.eat(token.AT)

	nameTok := p.eat(token.IDENT)
	kind, ok := ast.LookupAnnotation(nameTok.Literal)
	if !ok {
		p.errorf(nameTok.Pos, "unknown annotation %q", nameTok.Literal)
	}

	args := map[string]ast.Expr{}
	if p.check(token.LPAREN) {
		p.advance() // consume (
		for !p.check(token.RPAREN) && !p.check(token.EOF) {
			keyTok := p.eat(token.IDENT)
			p.eat(token.COLON)
			val := p.parseExpr(precLowest)
			args[keyTok.Literal] = val
			if !p.check(token.COMMA) {
				break
			}
			p.advance() // consume ,
		}
		p.eat(token.RPAREN)
	}

	return ast.Annotation{
		Kind: kind,
		Args: args,
		Pos:  pos,
	}
}

// parseFunctionDecl parses: fn name[<TypeParams>](params) [-> ReturnType] [needs Effects] Block
func (p *Parser) parseFunctionDecl(annots []ast.Annotation, isAsync bool) *ast.FunctionDecl {
	pos := p.current().Pos
	p.eat(token.FN)

	name := p.eat(token.IDENT)

	// optional type params
	typeParams := p.parseTypeParams()

	// params
	p.eat(token.LPAREN)
	params := p.parseParams()
	p.eat(token.RPAREN)

	// optional return type
	var returnType ast.TypeExpr
	if p.check(token.ARROW) {
		p.advance() // consume ->
		returnType = p.parseTypeExpr()
	}

	// optional needs clause
	var effects []ast.EffectExpr
	if p.check(token.NEEDS) {
		effects = p.parseEffects()
	}

	// body
	body := p.parseBlockStmt()

	return &ast.FunctionDecl{
		Name:        name.Literal,
		TypeParams:  typeParams,
		Params:      params,
		ReturnType:  returnType,
		Effects:     effects,
		Body:        body,
		Annotations: annots,
		IsAsync:     isAsync,
		Position:    pos,
	}
}

// parseTypeParams parses optional generic type parameters: <T, U: Constraint>
func (p *Parser) parseTypeParams() []ast.TypeParam {
	if !p.check(token.LT) {
		return nil
	}
	p.advance() // consume <

	var params []ast.TypeParam
	for !p.check(token.GT) && !p.check(token.EOF) {
		pos := p.current().Pos
		name := p.eat(token.IDENT)
		var constraints []string
		if p.check(token.COLON) {
			p.advance() // consume :
			constraints = append(constraints, p.eat(token.IDENT).Literal)
			for p.check(token.PLUS) {
				p.advance()
				constraints = append(constraints, p.eat(token.IDENT).Literal)
			}
		}
		params = append(params, ast.TypeParam{
			Name:        name.Literal,
			Constraints: constraints,
			Pos:         pos,
		})
		if !p.check(token.COMMA) {
			break
		}
		p.advance()
	}
	p.eat(token.GT)
	return params
}

// parseParams parses a comma-separated list of function parameters.
func (p *Parser) parseParams() []ast.Param {
	var params []ast.Param
	for !p.check(token.RPAREN) && !p.check(token.EOF) {
		param := p.parseParam()
		params = append(params, param)
		if !p.check(token.COMMA) {
			break
		}
		p.advance()
	}
	return params
}

// parseParam parses a single parameter: [...]name: Type [= default]
func (p *Parser) parseParam() ast.Param {
	pos := p.current().Pos
	variadic := false
	if p.check(token.DOT) && p.peek().Kind == token.DOT {
		// variadic: ...name
		p.advance()
		p.advance()
		if p.check(token.DOT) {
			p.advance()
		}
		variadic = true
	}

	name := p.eat(token.IDENT)
	p.eat(token.COLON)
	typ := p.parseTypeExpr()

	var def ast.Expr
	if p.check(token.ASSIGN) {
		p.advance()
		def = p.parseExpr(precLowest)
	}

	return ast.Param{
		Name:     name.Literal,
		Type:     typ,
		Default:  def,
		Variadic: variadic,
		Pos:      pos,
	}
}

// parseEffects parses: needs Effect1, Effect2.sub, ...
func (p *Parser) parseEffects() []ast.EffectExpr {
	p.eat(token.NEEDS)
	var effects []ast.EffectExpr
	for {
		pos := p.current().Pos
		nameTok := p.eat(token.IDENT)
		name := nameTok.Literal
		// optional dotted suffix: DB.read, DB.admin
		for p.check(token.DOT) {
			p.advance()
			sub := p.eat(token.IDENT)
			name = name + "." + sub.Literal
		}
		effects = append(effects, ast.EffectExpr{Name: name, Pos: pos})
		if !p.check(token.COMMA) {
			break
		}
		p.advance()
	}
	return effects
}

// parseTypeExpr parses a type expression.
func (p *Parser) parseTypeExpr() ast.TypeExpr {
	pos := p.current().Pos

	// fn type: fn(A, B) -> C
	if p.check(token.FN) {
		p.advance()
		p.eat(token.LPAREN)
		var paramTypes []ast.TypeExpr
		for !p.check(token.RPAREN) && !p.check(token.EOF) {
			paramTypes = append(paramTypes, p.parseTypeExpr())
			if !p.check(token.COMMA) {
				break
			}
			p.advance()
		}
		p.eat(token.RPAREN)
		var ret ast.TypeExpr
		if p.check(token.ARROW) {
			p.advance()
			ret = p.parseTypeExpr()
		}
		return &ast.FnTypeExpr{
			Params:     paramTypes,
			ReturnType: ret,
			Position:   pos,
		}
	}

	// named type (possibly with generics)
	name := p.eat(token.IDENT)
	var typeArgs []ast.TypeExpr
	if p.check(token.LT) {
		p.advance()
		for !p.check(token.GT) && !p.check(token.EOF) {
			typeArgs = append(typeArgs, p.parseTypeExpr())
			if !p.check(token.COMMA) {
				break
			}
			p.advance()
		}
		p.eat(token.GT)
	}
	var te ast.TypeExpr = &ast.NamedTypeExpr{
		Name:     name.Literal,
		TypeArgs: typeArgs,
		Position: pos,
	}

	// optional type: T?
	for p.check(token.QUESTION) {
		qpos := p.current().Pos
		p.advance()
		te = &ast.OptionalTypeExpr{Inner: te, Position: qpos}
	}

	return te
}

// parseTypeDecl parses: type Name [<TypeParams>] { fields }
func (p *Parser) parseTypeDecl(annots []ast.Annotation) *ast.TypeDecl {
	pos := p.current().Pos
	p.eat(token.TYPE)
	name := p.eat(token.IDENT)
	typeParams := p.parseTypeParams()

	p.eat(token.LBRACE)
	var fields []ast.FieldDecl
	for !p.check(token.RBRACE) && !p.check(token.EOF) {
		// collect field annotations
		var fieldAnnots []ast.Annotation
		for p.check(token.AT) {
			fieldAnnots = append(fieldAnnots, p.parseAnnotation())
		}

		fieldPos := p.current().Pos
		fieldName := p.eat(token.IDENT)
		p.eat(token.COLON)
		fieldType := p.parseTypeExpr()

		var def ast.Expr
		if p.check(token.ASSIGN) {
			p.advance()
			def = p.parseExpr(precLowest)
		}

		fields = append(fields, ast.FieldDecl{
			Name:        fieldName.Literal,
			Type:        fieldType,
			Default:     def,
			Annotations: fieldAnnots,
			Position:    fieldPos,
		})
	}
	p.eat(token.RBRACE)

	return &ast.TypeDecl{
		Name:        name.Literal,
		TypeParams:  typeParams,
		Fields:      fields,
		Annotations: annots,
		Position:    pos,
	}
}

// parseEnumDecl parses: enum Name { variant1 variant2(Type) ... }
func (p *Parser) parseEnumDecl(annots []ast.Annotation) *ast.EnumDecl {
	pos := p.current().Pos
	p.eat(token.ENUM)
	name := p.eat(token.IDENT)

	p.eat(token.LBRACE)
	var variants []ast.EnumVariant
	for !p.check(token.RBRACE) && !p.check(token.EOF) {
		vpos := p.current().Pos
		vname := p.eat(token.IDENT)
		var payload ast.TypeExpr
		if p.check(token.LPAREN) {
			p.advance()
			payload = p.parseTypeExpr()
			p.eat(token.RPAREN)
		}
		variants = append(variants, ast.EnumVariant{
			Name:    vname.Literal,
			Payload: payload,
			Pos:     vpos,
		})
	}
	p.eat(token.RBRACE)

	return &ast.EnumDecl{
		Name:        name.Literal,
		Variants:    variants,
		Annotations: annots,
		Position:    pos,
	}
}

// parseConstraintDecl parses: constraint Name [<TypeParams>] { methods }
func (p *Parser) parseConstraintDecl(annots []ast.Annotation) *ast.ConstraintDecl {
	pos := p.current().Pos
	p.eat(token.CONSTRAINT)
	name := p.eat(token.IDENT)
	typeParams := p.parseTypeParams()

	p.eat(token.LBRACE)
	var methods []ast.ConstraintMethod
	for !p.check(token.RBRACE) && !p.check(token.EOF) {
		mpos := p.current().Pos
		isStatic := false
		if p.check(token.STATIC) {
			p.advance()
			isStatic = true
		}
		p.eat(token.FN)
		mname := p.eat(token.IDENT)
		p.eat(token.LPAREN)
		params := p.parseParams()
		p.eat(token.RPAREN)

		var retType ast.TypeExpr
		if p.check(token.ARROW) {
			p.advance()
			retType = p.parseTypeExpr()
		}

		methods = append(methods, ast.ConstraintMethod{
			Name:       mname.Literal,
			Params:     params,
			ReturnType: retType,
			IsStatic:   isStatic,
			Pos:        mpos,
		})
	}
	p.eat(token.RBRACE)

	return &ast.ConstraintDecl{
		Name:        name.Literal,
		TypeParams:  typeParams,
		Methods:     methods,
		Annotations: annots,
		Position:    pos,
	}
}

// parseBlockStmt parses: { stmts }
func (p *Parser) parseBlockStmt() *ast.BlockStmt {
	pos := p.current().Pos
	p.eat(token.LBRACE)

	var stmts []ast.Stmt
	for !p.check(token.RBRACE) && !p.check(token.EOF) {
		s := p.parseStmt()
		if s != nil {
			stmts = append(stmts, s)
		}
	}
	p.eat(token.RBRACE)

	return &ast.BlockStmt{
		Stmts:    stmts,
		Position: pos,
	}
}

// parseStmt parses a single statement.
func (p *Parser) parseStmt() ast.Stmt {
	// consume optional semicolons
	for p.check(token.SEMICOLON) {
		p.advance()
	}

	switch p.current().Kind {
	case token.RETURN:
		return p.parseReturnStmt()
	case token.LET:
		return p.parseLetStmt()
	case token.IF:
		return p.parseIfStmt()
	case token.GUARD:
		return p.parseGuardStmt()
	case token.FOR:
		return p.parseForStmt()
	case token.RBRACE, token.EOF:
		return nil
	default:
		return p.parseExprOrAssignStmt()
	}
}

func (p *Parser) parseReturnStmt() *ast.ReturnStmt {
	pos := p.current().Pos
	p.eat(token.RETURN)
	var val ast.Expr
	if !p.check(token.RBRACE) && !p.check(token.SEMICOLON) && !p.check(token.EOF) {
		val = p.parseExpr(precLowest)
	}
	return &ast.ReturnStmt{Value: val, Position: pos}
}

func (p *Parser) parseLetStmt() *ast.LetStmt {
	pos := p.current().Pos
	p.eat(token.LET)
	name := p.eat(token.IDENT)

	var typ ast.TypeExpr
	if p.check(token.COLON) {
		p.advance()
		typ = p.parseTypeExpr()
	}

	p.eat(token.ASSIGN)
	val := p.parseExpr(precLowest)

	return &ast.LetStmt{
		Name:     name.Literal,
		Type:     typ,
		Value:    val,
		Position: pos,
	}
}

func (p *Parser) parseIfStmt() *ast.IfStmt {
	pos := p.current().Pos
	p.eat(token.IF)
	cond := p.parseExpr(precLowest)
	then := p.parseBlockStmt()

	var elseBranch ast.Stmt
	if p.check(token.ELSE) {
		p.advance()
		if p.check(token.IF) {
			elseBranch = p.parseIfStmt()
		} else {
			elseBranch = p.parseBlockStmt()
		}
	}

	return &ast.IfStmt{
		Cond:     cond,
		Then:     then,
		Else:     elseBranch,
		Position: pos,
	}
}

func (p *Parser) parseGuardStmt() *ast.GuardStmt {
	pos := p.current().Pos
	p.eat(token.GUARD)
	cond := p.parseExpr(precLowest)
	p.eat(token.ELSE)
	els := p.parseBlockStmt()
	return &ast.GuardStmt{Cond: cond, Else: els, Position: pos}
}

func (p *Parser) parseForStmt() *ast.ForStmt {
	pos := p.current().Pos
	p.eat(token.FOR)
	binding := p.eat(token.IDENT)
	p.eat(token.IN)
	iter := p.parseExpr(precLowest)
	body := p.parseBlockStmt()
	return &ast.ForStmt{
		Binding:  binding.Literal,
		Iter:     iter,
		Body:     body,
		Position: pos,
	}
}

// parseExprOrAssignStmt parses an expression statement or assignment.
func (p *Parser) parseExprOrAssignStmt() ast.Stmt {
	pos := p.current().Pos
	expr := p.parseExpr(precLowest)

	if p.check(token.ASSIGN) {
		p.advance()
		val := p.parseExpr(precLowest)
		return &ast.AssignStmt{Target: expr, Value: val, Position: pos}
	}

	return &ast.ExprStmt{Expr: expr, Position: pos}
}

// parseExpr implements Pratt (top-down operator precedence) parsing.
func (p *Parser) parseExpr(minPrec precedence) ast.Expr {
	left := p.parsePrefix()

	for {
		cur := p.current()
		prec, ok := tokenPrecedence[cur.Kind]
		if !ok || prec <= minPrec {
			break
		}

		switch cur.Kind {
		case token.DOT, token.OPT_CHAIN:
			left = p.parseMemberExpr(left)
		case token.LPAREN:
			left = p.parseCallExpr(left)
		case token.LBRACKET:
			left = p.parseIndexExpr(left)
		case token.NULL_COAL:
			left = p.parseNullCoalesceExpr(left)
		case token.ASSIGN:
			// assignment handled at statement level; treat as right-assoc binary
			p.advance()
			right := p.parseExpr(prec - 1)
			left = &ast.BinaryExpr{Left: left, Op: cur.Kind, Right: right, Position: cur.Pos}
		default:
			p.advance()
			right := p.parseExpr(prec + 1)
			left = &ast.BinaryExpr{Left: left, Op: cur.Kind, Right: right, Position: cur.Pos}
		}
	}

	return left
}

// parsePrefix parses a prefix expression (literal, ident, unary op, grouped).
func (p *Parser) parsePrefix() ast.Expr {
	cur := p.current()
	switch cur.Kind {
	case token.INT:
		p.advance()
		v, _ := strconv.ParseInt(cur.Literal, 10, 64)
		return &ast.IntLiteral{Value: v, Position: cur.Pos}

	case token.FLOAT:
		p.advance()
		v, _ := strconv.ParseFloat(cur.Literal, 64)
		return &ast.FloatLiteral{Value: v, Position: cur.Pos}

	case token.STRING:
		p.advance()
		// strip surrounding quotes
		s := cur.Literal
		if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
			s = s[1 : len(s)-1]
		}
		return &ast.StringLiteral{Value: s, Position: cur.Pos}

	case token.TRUE:
		p.advance()
		return &ast.BoolLiteral{Value: true, Position: cur.Pos}

	case token.FALSE:
		p.advance()
		return &ast.BoolLiteral{Value: false, Position: cur.Pos}

	case token.NONE:
		p.advance()
		return &ast.NoneLiteral{Position: cur.Pos}

	case token.IDENT:
		p.advance()
		// Struct literal: TypeName { field: expr, ... }
		if p.check(token.LBRACE) {
			return p.parseStructLiteral(cur)
		}
		return &ast.Ident{Name: cur.Literal, Position: cur.Pos}

	case token.BANG, token.MINUS:
		p.advance()
		operand := p.parseExpr(precUnary)
		return &ast.UnaryExpr{Op: cur.Kind, Operand: operand, Position: cur.Pos}

	case token.LPAREN:
		p.advance()
		inner := p.parseExpr(precLowest)
		p.eat(token.RPAREN)
		return inner

	case token.LBRACKET:
		return p.parseListLiteral()

	case token.MATCH:
		return p.parseMatchExpr()

	default:
		p.errorf(cur.Pos, "unexpected token %q in expression", cur.Literal)
		p.advance()
		return &ast.Ident{Name: "_error_", Position: cur.Pos}
	}
}

func (p *Parser) parseMemberExpr(obj ast.Expr) ast.Expr {
	tok := p.advance()
	optional := tok.Kind == token.OPT_CHAIN
	member := p.eat(token.IDENT)
	return &ast.MemberExpr{
		Object:   obj,
		Member:   member.Literal,
		Optional: optional,
		Position: tok.Pos,
	}
}

func (p *Parser) parseCallExpr(callee ast.Expr) ast.Expr {
	pos := p.current().Pos
	p.eat(token.LPAREN)
	var args []ast.Expr
	for !p.check(token.RPAREN) && !p.check(token.EOF) {
		args = append(args, p.parseExpr(precLowest))
		if !p.check(token.COMMA) {
			break
		}
		p.advance()
	}
	p.eat(token.RPAREN)
	return &ast.CallExpr{Callee: callee, Args: args, Position: pos}
}

func (p *Parser) parseIndexExpr(obj ast.Expr) ast.Expr {
	pos := p.current().Pos
	p.advance() // consume [
	idx := p.parseExpr(precLowest)
	p.eat(token.RBRACKET)
	return &ast.IndexExpr{Object: obj, Index: idx, Position: pos}
}

func (p *Parser) parseNullCoalesceExpr(left ast.Expr) ast.Expr {
	pos := p.current().Pos
	p.advance() // consume ??
	right := p.parseExpr(precAdd - 1)
	return &ast.NullCoalesceExpr{Left: left, Right: right, Position: pos}
}

func (p *Parser) parseStructLiteral(nameTok token.Token) *ast.StructLiteral {
	pos := nameTok.Pos
	p.eat(token.LBRACE)
	var fields []ast.StructField
	for !p.check(token.RBRACE) && !p.check(token.EOF) {
		fpos := p.current().Pos
		name := p.eat(token.IDENT)
		p.eat(token.COLON)
		val := p.parseExpr(precLowest)
		fields = append(fields, ast.StructField{Name: name.Literal, Value: val, Pos: fpos})
		if !p.check(token.COMMA) {
			break
		}
		p.advance()
	}
	p.eat(token.RBRACE)
	return &ast.StructLiteral{TypeName: nameTok.Literal, Fields: fields, Position: pos}
}

func (p *Parser) parseListLiteral() *ast.ListLiteral {
	pos := p.current().Pos
	p.eat(token.LBRACKET)
	var elems []ast.Expr
	for !p.check(token.RBRACKET) && !p.check(token.EOF) {
		elems = append(elems, p.parseExpr(precLowest))
		if !p.check(token.COMMA) {
			break
		}
		p.advance()
	}
	p.eat(token.RBRACKET)
	return &ast.ListLiteral{Elements: elems, Position: pos}
}

func (p *Parser) parseMatchExpr() *ast.MatchExpr {
	pos := p.current().Pos
	p.eat(token.MATCH)
	subject := p.parseExpr(precLowest)
	p.eat(token.LBRACE)
	var arms []ast.MatchArm
	for !p.check(token.RBRACE) && !p.check(token.EOF) {
		apos := p.current().Pos
		pat := p.parseExpr(precLowest)
		p.eat(token.FAT_ARROW)
		body := p.parseExpr(precLowest)
		arms = append(arms, ast.MatchArm{Pattern: pat, Body: body, Pos: apos})
		if p.check(token.COMMA) {
			p.advance()
		}
	}
	p.eat(token.RBRACE)
	return &ast.MatchExpr{Subject: subject, Arms: arms, Position: pos}
}
