package lexer_test

import (
	"testing"

	"gosplash.dev/splash/internal/lexer"
	"gosplash.dev/splash/internal/token"
)

func tokens(src string) []token.Token {
	return lexer.New("test.splash", src).Tokenize()
}

func TestLexerIntegers(t *testing.T) {
	toks := tokens("42")
	if len(toks) != 2 { // INT + EOF
		t.Fatalf("expected 2 tokens, got %d", len(toks))
	}
	if toks[0].Kind != token.INT || toks[0].Literal != "42" {
		t.Errorf("got %v", toks[0])
	}
}

func TestLexerString(t *testing.T) {
	toks := tokens(`"hello"`)
	if toks[0].Kind != token.STRING || toks[0].Literal != "hello" {
		t.Errorf("got %v", toks[0])
	}
}

func TestLexerKeywords(t *testing.T) {
	cases := []struct {
		src  string
		kind token.Kind
	}{
		{"fn", token.FN},
		{"type", token.TYPE},
		{"needs", token.NEEDS},
		{"module", token.MODULE},
		{"expose", token.EXPOSE},
		{"async", token.ASYNC},
		{"try", token.TRY},
	}
	for _, c := range cases {
		toks := tokens(c.src)
		if toks[0].Kind != c.kind {
			t.Errorf("src=%q: got kind %d, want %d", c.src, toks[0].Kind, c.kind)
		}
	}
}

func TestLexerAnnotation(t *testing.T) {
	toks := tokens("@sensitive")
	if len(toks) < 2 {
		t.Fatal("expected at least 2 tokens")
	}
	if toks[0].Kind != token.AT {
		t.Errorf("expected AT, got %d", toks[0].Kind)
	}
	if toks[1].Kind != token.IDENT || toks[1].Literal != "sensitive" {
		t.Errorf("expected IDENT 'sensitive', got %v", toks[1])
	}
}

func TestLexerOperators(t *testing.T) {
	cases := []struct {
		src  string
		kind token.Kind
	}{
		{"->", token.ARROW},
		{"=>", token.FAT_ARROW},
		{"??", token.NULL_COAL},
		{"?.", token.OPT_CHAIN},
		{"==", token.EQ},
		{"!=", token.NEQ},
	}
	for _, c := range cases {
		toks := tokens(c.src)
		if toks[0].Kind != c.kind {
			t.Errorf("src=%q: got kind %d, want %d", c.src, toks[0].Kind, c.kind)
		}
	}
}

func TestLexerLineTracking(t *testing.T) {
	toks := tokens("fn\nfoo")
	if toks[1].Pos.Line != 2 {
		t.Errorf("expected line 2 for 'foo', got %d", toks[1].Pos.Line)
	}
}

func TestLexerCommentStripped(t *testing.T) {
	toks := tokens("fn // this is a comment\nfoo")
	if toks[0].Kind != token.FN {
		t.Errorf("expected FN, got %d", toks[0].Kind)
	}
	if toks[1].Kind != token.IDENT || toks[1].Literal != "foo" {
		t.Errorf("expected IDENT 'foo', got %v", toks[1])
	}
}
