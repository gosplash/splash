package token

import "fmt"

// Kind represents the type of a token
type Kind int

const (
	ILLEGAL Kind = iota
	EOF

	// Literals
	INT    // 42
	FLOAT  // 3.14
	STRING // "hello"
	IDENT  // foo

	// Keywords
	FN
	TYPE
	ENUM
	CONSTRAINT
	MODULE
	EXPOSE
	USE
	LET
	RETURN
	IF
	ELSE
	GUARD
	MATCH
	FOR
	IN
	ASYNC
	AWAIT
	TRY
	NEEDS
	NONE
	STATIC
	OVERRIDE
	WHERE
	TRUE
	FALSE

	// Operators
	PLUS      // +
	MINUS     // -
	STAR      // *
	SLASH     // /
	PERCENT   // %
	ASSIGN    // =
	EQ        // ==
	NEQ       // !=
	LT        // <
	LTE       // <=
	GT        // >
	GTE       // >=
	AND_AND   // &&
	OR_OR     // ||
	BANG      // !
	QUESTION  // ?
	NULL_COAL // ??
	OPT_CHAIN // ?.
	ARROW     // ->
	FAT_ARROW // =>
	PIPE      // |

	// Punctuation
	DOT
	COMMA
	COLON
	SEMICOLON
	LPAREN
	RPAREN
	LBRACE
	RBRACE
	LBRACKET
	RBRACKET
	AT // @ annotation prefix
)

var keywords = map[string]Kind{
	"fn":         FN,
	"type":       TYPE,
	"enum":       ENUM,
	"constraint": CONSTRAINT,
	"module":     MODULE,
	"expose":     EXPOSE,
	"use":        USE,
	"let":        LET,
	"return":     RETURN,
	"if":         IF,
	"else":       ELSE,
	"guard":      GUARD,
	"match":      MATCH,
	"for":        FOR,
	"in":         IN,
	"async":      ASYNC,
	"await":      AWAIT,
	"try":        TRY,
	"needs":      NEEDS,
	"none":       NONE,
	"static":     STATIC,
	"override":   OVERRIDE,
	"where":      WHERE,
	"true":       TRUE,
	"false":      FALSE,
}

// LookupIdent returns the keyword Kind for a given identifier string,
// or IDENT if the string is not a keyword.
func LookupIdent(s string) Kind {
	if kind, ok := keywords[s]; ok {
		return kind
	}
	return IDENT
}

// Position represents the location of a token in source code
type Position struct {
	File   string
	Line   int
	Column int
	Offset int
}

// String returns a formatted position string in "file:line:col" format
func (p Position) String() string {
	return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Column)
}

// Token represents a lexical token
type Token struct {
	Kind    Kind
	Literal string
	Pos     Position
}
