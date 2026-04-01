package lexer

import (
	"gosplash.dev/splash/internal/token"
)

// Lexer holds the state for tokenizing a Splash source file.
type Lexer struct {
	filename string
	src      []rune
	pos      int // current position in src
	line     int
	col      int
}

// New creates a new Lexer for the given filename and source string.
func New(filename, src string) *Lexer {
	return &Lexer{
		filename: filename,
		src:      []rune(src),
		pos:      0,
		line:     1,
		col:      1,
	}
}

// Tokenize scans the entire source and returns all tokens, ending with EOF.
func (l *Lexer) Tokenize() []token.Token {
	var toks []token.Token
	for {
		t := l.nextToken()
		toks = append(toks, t)
		if t.Kind == token.EOF {
			break
		}
	}
	return toks
}

// current returns the rune at pos, or 0 if past end.
func (l *Lexer) current() rune {
	if l.pos >= len(l.src) {
		return 0
	}
	return l.src[l.pos]
}

// peek returns the rune one ahead of current, or 0 if past end.
func (l *Lexer) peek() rune {
	return l.peekAt(1)
}

// peekAt returns the rune offset positions ahead of current, or 0 if past end.
func (l *Lexer) peekAt(offset int) rune {
	idx := l.pos + offset
	if idx >= len(l.src) {
		return 0
	}
	return l.src[idx]
}

// advance moves forward one rune, tracking line and column.
func (l *Lexer) advance() rune {
	ch := l.current()
	l.pos++
	if ch == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return ch
}

// position returns the current source position.
func (l *Lexer) position() token.Position {
	return token.Position{
		File:   l.filename,
		Line:   l.line,
		Column: l.col,
		Offset: l.pos,
	}
}

// skipWhitespace skips spaces, tabs, carriage returns, newlines, and comments.
func (l *Lexer) skipWhitespace() {
	for {
		ch := l.current()
		switch {
		case ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n':
			l.advance()
		case ch == '/' && l.peek() == '/':
			// Line comment: skip to end of line
			for l.current() != '\n' && l.current() != 0 {
				l.advance()
			}
		case ch == '/' && l.peek() == '*':
			// Block comment: skip until */
			l.advance() // /
			l.advance() // *
			for l.current() != 0 {
				if l.current() == '*' && l.peek() == '/' {
					l.advance() // *
					l.advance() // /
					break
				}
				l.advance()
			}
		default:
			return
		}
	}
}

// readIdent reads an identifier or keyword.
func (l *Lexer) readIdent() string {
	start := l.pos
	for isLetter(l.current()) || isDigit(l.current()) || l.current() == '_' {
		l.advance()
	}
	return string(l.src[start:l.pos])
}

// readNumber reads an integer or float literal.
func (l *Lexer) readNumber() (string, token.Kind) {
	start := l.pos
	for isDigit(l.current()) {
		l.advance()
	}
	kind := token.INT
	if l.current() == '.' && isDigit(l.peek()) {
		kind = token.FLOAT
		l.advance() // consume '.'
		for isDigit(l.current()) {
			l.advance()
		}
	}
	return string(l.src[start:l.pos]), kind
}

// readString reads a quoted string literal, returning the content without quotes.
func (l *Lexer) readString() string {
	l.advance() // consume opening "
	var buf []rune
	for l.current() != 0 && l.current() != '"' {
		if l.current() == '\\' {
			l.advance() // consume backslash
			switch l.current() {
			case 'n':
				buf = append(buf, '\n')
			case 't':
				buf = append(buf, '\t')
			case '\\':
				buf = append(buf, '\\')
			case '"':
				buf = append(buf, '"')
			default:
				buf = append(buf, l.current())
			}
			l.advance()
		} else {
			buf = append(buf, l.current())
			l.advance()
		}
	}
	if l.current() == '"' {
		l.advance() // consume closing "
	}
	return string(buf)
}

// nextToken scans and returns the next token.
func (l *Lexer) nextToken() token.Token {
	l.skipWhitespace()

	pos := l.position()
	ch := l.current()

	if ch == 0 {
		return token.Token{Kind: token.EOF, Pos: pos}
	}

	// Identifiers and keywords
	if isLetter(ch) || ch == '_' {
		lit := l.readIdent()
		kind := token.LookupIdent(lit)
		return token.Token{Kind: kind, Literal: lit, Pos: pos}
	}

	// Numbers
	if isDigit(ch) {
		lit, kind := l.readNumber()
		return token.Token{Kind: kind, Literal: lit, Pos: pos}
	}

	// Strings
	if ch == '"' {
		lit := l.readString()
		return token.Token{Kind: token.STRING, Literal: lit, Pos: pos}
	}

	// Multi-character and single-character operators
	switch ch {
	case '-':
		if l.peek() == '>' {
			l.advance()
			l.advance()
			return token.Token{Kind: token.ARROW, Literal: "->", Pos: pos}
		}
		l.advance()
		return token.Token{Kind: token.MINUS, Literal: "-", Pos: pos}

	case '=':
		if l.peek() == '>' {
			l.advance()
			l.advance()
			return token.Token{Kind: token.FAT_ARROW, Literal: "=>", Pos: pos}
		}
		if l.peek() == '=' {
			l.advance()
			l.advance()
			return token.Token{Kind: token.EQ, Literal: "==", Pos: pos}
		}
		l.advance()
		return token.Token{Kind: token.ASSIGN, Literal: "=", Pos: pos}

	case '?':
		if l.peek() == '?' {
			l.advance()
			l.advance()
			return token.Token{Kind: token.NULL_COAL, Literal: "??", Pos: pos}
		}
		if l.peek() == '.' {
			l.advance()
			l.advance()
			return token.Token{Kind: token.OPT_CHAIN, Literal: "?.", Pos: pos}
		}
		l.advance()
		return token.Token{Kind: token.QUESTION, Literal: "?", Pos: pos}

	case '!':
		if l.peek() == '=' {
			l.advance()
			l.advance()
			return token.Token{Kind: token.NEQ, Literal: "!=", Pos: pos}
		}
		l.advance()
		return token.Token{Kind: token.BANG, Literal: "!", Pos: pos}

	case '<':
		if l.peek() == '=' {
			l.advance()
			l.advance()
			return token.Token{Kind: token.LTE, Literal: "<=", Pos: pos}
		}
		l.advance()
		return token.Token{Kind: token.LT, Literal: "<", Pos: pos}

	case '>':
		if l.peek() == '=' {
			l.advance()
			l.advance()
			return token.Token{Kind: token.GTE, Literal: ">=", Pos: pos}
		}
		l.advance()
		return token.Token{Kind: token.GT, Literal: ">", Pos: pos}

	case '&':
		if l.peek() == '&' {
			l.advance()
			l.advance()
			return token.Token{Kind: token.AND_AND, Literal: "&&", Pos: pos}
		}
		l.advance()
		return token.Token{Kind: token.ILLEGAL, Literal: "&", Pos: pos}

	case '|':
		if l.peek() == '|' {
			l.advance()
			l.advance()
			return token.Token{Kind: token.OR_OR, Literal: "||", Pos: pos}
		}
		l.advance()
		return token.Token{Kind: token.PIPE, Literal: "|", Pos: pos}

	case '+':
		l.advance()
		return token.Token{Kind: token.PLUS, Literal: "+", Pos: pos}

	case '*':
		l.advance()
		return token.Token{Kind: token.STAR, Literal: "*", Pos: pos}

	case '/':
		l.advance()
		return token.Token{Kind: token.SLASH, Literal: "/", Pos: pos}

	case '%':
		l.advance()
		return token.Token{Kind: token.PERCENT, Literal: "%", Pos: pos}

	case '.':
		l.advance()
		return token.Token{Kind: token.DOT, Literal: ".", Pos: pos}

	case ',':
		l.advance()
		return token.Token{Kind: token.COMMA, Literal: ",", Pos: pos}

	case ':':
		l.advance()
		return token.Token{Kind: token.COLON, Literal: ":", Pos: pos}

	case ';':
		l.advance()
		return token.Token{Kind: token.SEMICOLON, Literal: ";", Pos: pos}

	case '(':
		l.advance()
		return token.Token{Kind: token.LPAREN, Literal: "(", Pos: pos}

	case ')':
		l.advance()
		return token.Token{Kind: token.RPAREN, Literal: ")", Pos: pos}

	case '{':
		l.advance()
		return token.Token{Kind: token.LBRACE, Literal: "{", Pos: pos}

	case '}':
		l.advance()
		return token.Token{Kind: token.RBRACE, Literal: "}", Pos: pos}

	case '[':
		l.advance()
		return token.Token{Kind: token.LBRACKET, Literal: "[", Pos: pos}

	case ']':
		l.advance()
		return token.Token{Kind: token.RBRACKET, Literal: "]", Pos: pos}

	case '@':
		l.advance()
		return token.Token{Kind: token.AT, Literal: "@", Pos: pos}

	default:
		lit := string(ch)
		l.advance()
		return token.Token{Kind: token.ILLEGAL, Literal: lit, Pos: pos}
	}
}

func isLetter(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func isDigit(ch rune) bool {
	return ch >= '0' && ch <= '9'
}
