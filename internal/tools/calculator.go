package tools

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// Calculator is a tool that evaluates arithmetic expressions.
type Calculator struct{}

func (c *Calculator) Name() string { return "calculator" }

func (c *Calculator) Description() string {
	return "Evaluate a mathematical expression. Supports +, -, *, /, ** (power), sqrt(), sin(), cos(), tan(), log(), ln(), abs(), round(). Use 'x' for multiplication instead of '*' where needed."
}

func (c *Calculator) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"expression": map[string]any{
				"type":        "string",
				"description": "The mathematical expression to evaluate, e.g. \"2 + 2\", \"sqrt(144)\", \"3.14 * 5\"",
			},
		},
		"required": []string{"expression"},
	}
}

func (c *Calculator) Execute(_ context.Context, params map[string]any) (string, error) {
	expr, _ := params["expression"].(string)
	if expr == "" {
		return "", fmt.Errorf("expression is required")
	}

	result, err := eval(expr)
	if err != nil {
		return "", fmt.Errorf("failed to evaluate %q: %w", expr, err)
	}

	return fmt.Sprintf("%s = %s", expr, result), nil
}

// Simple expression evaluator.
func eval(expr string) (string, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", fmt.Errorf("empty expression")
	}

	// Check for supported functions.
	expr = strings.ReplaceAll(expr, "pi", fmt.Sprintf("%.15f", math.Pi))
	expr = strings.ReplaceAll(expr, "e", fmt.Sprintf("%.15f", math.E))

	// Use a simple recursive descent parser for safe evaluation.
	tokens := tokenize(expr)
	if len(tokens) == 0 {
		return "", fmt.Errorf("empty expression")
	}

	parser := &parser{tokens: tokens}
	parser.pos = 0
	val, err := parser.parseExpr()
	if err != nil {
		return "", err
	}

	// Format the result.
	if val == math.Trunc(val) && !math.IsInf(val, 0) && math.Abs(val) < 1e15 {
		return strconv.FormatInt(int64(val), 10), nil
	}
	return strconv.FormatFloat(val, 'f', -1, 64), nil
}

type tokenType int

const (
	tokNumber tokenType = iota
	tokPlus
	tokMinus
	tokMul
	tokDiv
	tokPow
	tokLParen
	tokRParen
	tokIdent
	tokComma
	tokEOF
)

type token struct {
	typ   tokenType
	value string
}

func tokenize(expr string) []token {
	var tokens []token
	runes := []rune(expr)
	i := 0

	for i < len(runes) {
		ch := runes[i]

		// Skip whitespace.
		if ch == ' ' || ch == '\t' {
			i++
			continue
		}

		// Number.
		if unicode.IsDigit(ch) || (ch == '.' && i+1 < len(runes) && unicode.IsDigit(runes[i+1])) {
			start := i
			hasDot := ch == '.'
			i++
			for i < len(runes) && (unicode.IsDigit(runes[i]) || runes[i] == '.') {
				if runes[i] == '.' {
					if hasDot {
						break
					}
					hasDot = true
				}
				i++
			}
			tokens = append(tokens, token{tokNumber, string(runes[start:i])})
			continue
		}

		// Identifiers (functions).
		if unicode.IsLetter(ch) {
			start := i
			for i < len(runes) && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i])) {
				i++
			}
			tokens = append(tokens, token{tokIdent, string(runes[start:i])})
			continue
		}

		switch ch {
		case '+':
			tokens = append(tokens, token{tokPlus, "+"})
		case '-':
			tokens = append(tokens, token{tokMinus, "-"})
		case '*':
			if i+1 < len(runes) && runes[i+1] == '*' {
				tokens = append(tokens, token{tokPow, "**"})
				i++
			} else {
				tokens = append(tokens, token{tokMul, "*"})
			}
		case '/':
			tokens = append(tokens, token{tokDiv, "/"})
		case '(':
			tokens = append(tokens, token{tokLParen, "("})
		case ')':
			tokens = append(tokens, token{tokRParen, ")"})
		case ',':
			tokens = append(tokens, token{tokComma, ","})
		case 'x':
			// Support 'x' as multiplication.
			tokens = append(tokens, token{tokMul, "x"})
		default:
			tokens = append(tokens, token{tokEOF, string(ch)})
		}
		i++
	}
	tokens = append(tokens, token{tokEOF, ""})
	return tokens
}

type parser struct {
	tokens []token
	pos    int
}

func (p *parser) peek() token {
	return p.tokens[p.pos]
}

func (p *parser) advance() token {
	t := p.tokens[p.pos]
	p.pos++
	return t
}

func (p *parser) expect(typ tokenType) (token, error) {
	t := p.peek()
	if t.typ != typ {
		return t, fmt.Errorf("expected %d but got %s", typ, t.value)
	}
	return p.advance(), nil
}

// expr = term (('+' | '-') term)*
func (p *parser) parseExpr() (float64, error) {
	val, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for p.peek().typ == tokPlus || p.peek().typ == tokMinus {
		op := p.advance()
		right, err := p.parseTerm()
		if err != nil {
			return 0, err
		}
		if op.typ == tokPlus {
			val += right
		} else {
			val -= right
		}
	}
	return val, nil
}

// term = factor (('*' | '/') factor)*
func (p *parser) parseTerm() (float64, error) {
	val, err := p.parseFactor()
	if err != nil {
		return 0, err
	}
	for p.peek().typ == tokMul || p.peek().typ == tokDiv {
		op := p.advance()
		right, err := p.parseFactor()
		if err != nil {
			return 0, err
		}
		if op.typ == tokMul {
			val *= right
		} else {
			if right == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			val /= right
		}
	}
	return val, nil
}

// factor = power (('^' | '**') power)*  |  unary
func (p *parser) parseFactor() (float64, error) {
	// Handle unary minus/plus.
	if p.peek().typ == tokMinus || p.peek().typ == tokPlus {
		op := p.advance()
		val, err := p.parseAtom()
		if err != nil {
			return 0, err
		}
		if op.typ == tokMinus {
			val = -val
		}
		// Handle power.
		if p.peek().typ == tokPow {
			p.advance()
			exp, err := p.parseFactor()
			if err != nil {
				return 0, err
			}
			return math.Pow(val, exp), nil
		}
		return val, nil
	}

	val, err := p.parseAtom()
	if err != nil {
		return 0, err
	}

	if p.peek().typ == tokPow {
		p.advance()
		exp, err := p.parseFactor()
		if err != nil {
			return 0, err
		}
		return math.Pow(val, exp), nil
	}
	return val, nil
}

// atom = number | '(' expr ')' | ident '(' expr (',' expr)* ')'
func (p *parser) parseAtom() (float64, error) {
	t := p.peek()

	switch t.typ {
	case tokNumber:
		p.advance()
		return strconv.ParseFloat(t.value, 64)

	case tokLParen:
		p.advance()
		val, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		_, err = p.expect(tokRParen)
		return val, err

	case tokIdent:
		p.advance()
		name := t.value
		if p.peek().typ != tokLParen {
			return 0, fmt.Errorf("expected '(' after function %s", name)
		}
		p.advance() // skip '('
		args := []float64{}
		for p.peek().typ != tokRParen {
			if len(args) > 0 {
				_, err := p.expect(tokComma)
				if err != nil {
					return 0, err
				}
			}
			arg, err := p.parseExpr()
			if err != nil {
				return 0, err
			}
			args = append(args, arg)
		}
		_, err := p.expect(tokRParen)
		if err != nil {
			return 0, err
		}
		return callFunc(name, args)

	default:
		return 0, fmt.Errorf("unexpected token: %s", t.value)
	}
}

func callFunc(name string, args []float64) (float64, error) {
	switch strings.ToLower(name) {
	case "sqrt":
		if len(args) != 1 {
			return 0, fmt.Errorf("sqrt requires 1 argument")
		}
		if args[0] < 0 {
			return 0, fmt.Errorf("sqrt of negative number")
		}
		return math.Sqrt(args[0]), nil
	case "sin":
		if len(args) != 1 {
			return 0, fmt.Errorf("sin requires 1 argument")
		}
		return math.Sin(args[0]), nil
	case "cos":
		if len(args) != 1 {
			return 0, fmt.Errorf("cos requires 1 argument")
		}
		return math.Cos(args[0]), nil
	case "tan":
		if len(args) != 1 {
			return 0, fmt.Errorf("tan requires 1 argument")
		}
		return math.Tan(args[0]), nil
	case "log":
		if len(args) != 1 {
			return 0, fmt.Errorf("log requires 1 argument (base 10)")
		}
		if args[0] <= 0 {
			return 0, fmt.Errorf("log of non-positive number")
		}
		return math.Log10(args[0]), nil
	case "ln":
		if len(args) != 1 {
			return 0, fmt.Errorf("ln requires 1 argument")
		}
		if args[0] <= 0 {
			return 0, fmt.Errorf("ln of non-positive number")
		}
		return math.Log(args[0]), nil
	case "abs":
		if len(args) != 1 {
			return 0, fmt.Errorf("abs requires 1 argument")
		}
		return math.Abs(args[0]), nil
	case "round":
		if len(args) != 1 {
			return 0, fmt.Errorf("round requires 1 argument")
		}
		return math.Round(args[0]), nil
	case "max":
		if len(args) < 2 {
			return 0, fmt.Errorf("max requires at least 2 arguments")
		}
		val := args[0]
		for _, a := range args[1:] {
			if a > val {
				val = a
			}
		}
		return val, nil
	case "min":
		if len(args) < 2 {
			return 0, fmt.Errorf("min requires at least 2 arguments")
		}
		val := args[0]
		for _, a := range args[1:] {
			if a < val {
				val = a
			}
		}
		return val, nil
	default:
		return 0, fmt.Errorf("unknown function: %s", name)
	}
}
