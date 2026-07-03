package core

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
)

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokIdent
	tokRequest
	tokString
	tokNumber
	tokBool
	tokNull
	tokLParen
	tokRParen
	tokAnd
	tokOr
	tokCompare
)

type ruleToken struct {
	kind tokenKind
	text string
}

type ruleLexer struct {
	input string
	pos   int
}

func (l *ruleLexer) next() (ruleToken, error) {
	for l.pos < len(l.input) && unicode.IsSpace(rune(l.input[l.pos])) {
		l.pos++
	}
	if l.pos >= len(l.input) {
		return ruleToken{kind: tokEOF}, nil
	}
	ch := l.input[l.pos]
	switch ch {
	case '(':
		l.pos++
		return ruleToken{kind: tokLParen, text: "("}, nil
	case ')':
		l.pos++
		return ruleToken{kind: tokRParen, text: ")"}, nil
	case '&':
		if l.match("&&") {
			return ruleToken{kind: tokAnd, text: "&&"}, nil
		}
	case '|':
		if l.match("||") {
			return ruleToken{kind: tokOr, text: "||"}, nil
		}
	case '=', '!', '>', '<':
		return l.scanCompare()
	case '"':
		return l.scanString()
	case '@':
		return l.scanRequest()
	}
	if ch == '-' || unicode.IsDigit(rune(ch)) {
		return l.scanNumber()
	}
	if isIdentStart(ch) {
		return l.scanIdent()
	}
	return ruleToken{}, fmt.Errorf("%w: unexpected character %q", ErrInvalidRule, ch)
}

func (l *ruleLexer) match(s string) bool {
	if strings.HasPrefix(l.input[l.pos:], s) {
		l.pos += len(s)
		return true
	}
	return false
}

func (l *ruleLexer) scanCompare() (ruleToken, error) {
	for _, op := range []string{"!=", ">=", "<=", "=", ">", "<"} {
		if l.match(op) {
			return ruleToken{kind: tokCompare, text: op}, nil
		}
	}
	return ruleToken{}, fmt.Errorf("%w: invalid comparison operator", ErrInvalidRule)
}

func (l *ruleLexer) scanString() (ruleToken, error) {
	l.pos++
	var b strings.Builder
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		l.pos++
		if ch == '"' {
			return ruleToken{kind: tokString, text: b.String()}, nil
		}
		if ch == '\\' {
			if l.pos >= len(l.input) {
				return ruleToken{}, fmt.Errorf("%w: unterminated string", ErrInvalidRule)
			}
			esc := l.input[l.pos]
			l.pos++
			switch esc {
			case '"', '\\':
				b.WriteByte(esc)
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				return ruleToken{}, fmt.Errorf("%w: unsupported string escape", ErrInvalidRule)
			}
			continue
		}
		b.WriteByte(ch)
	}
	return ruleToken{}, fmt.Errorf("%w: unterminated string", ErrInvalidRule)
}

func (l *ruleLexer) scanRequest() (ruleToken, error) {
	start := l.pos
	l.pos++
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if !isIdentStart(ch) && !unicode.IsDigit(rune(ch)) && ch != '.' {
			break
		}
		l.pos++
	}
	return ruleToken{kind: tokRequest, text: l.input[start:l.pos]}, nil
}

func (l *ruleLexer) scanNumber() (ruleToken, error) {
	start := l.pos
	if l.input[l.pos] == '-' {
		l.pos++
	}
	digits := 0
	for l.pos < len(l.input) && unicode.IsDigit(rune(l.input[l.pos])) {
		l.pos++
		digits++
	}
	if l.pos < len(l.input) && l.input[l.pos] == '.' {
		l.pos++
		for l.pos < len(l.input) && unicode.IsDigit(rune(l.input[l.pos])) {
			l.pos++
			digits++
		}
	}
	if digits == 0 {
		return ruleToken{}, fmt.Errorf("%w: invalid number", ErrInvalidRule)
	}
	text := l.input[start:l.pos]
	if _, err := strconv.ParseFloat(text, 64); err != nil {
		return ruleToken{}, fmt.Errorf("%w: invalid number", ErrInvalidRule)
	}
	return ruleToken{kind: tokNumber, text: text}, nil
}

func (l *ruleLexer) scanIdent() (ruleToken, error) {
	start := l.pos
	l.pos++
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if !isIdentStart(ch) && !unicode.IsDigit(rune(ch)) {
			break
		}
		l.pos++
	}
	text := l.input[start:l.pos]
	switch text {
	case "true", "false":
		return ruleToken{kind: tokBool, text: text}, nil
	case "null":
		return ruleToken{kind: tokNull, text: text}, nil
	default:
		return ruleToken{kind: tokIdent, text: text}, nil
	}
}

func isIdentStart(ch byte) bool {
	return ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

type ruleNode interface{}

type binaryNode struct {
	op          string
	left, right ruleNode
}

type compareNode struct {
	op          string
	left, right ruleNode
}

type identNode struct{ name string }
type requestNode struct{ name string }

type literalNode struct {
	kind  tokenKind
	text  string
	value any
}

type ruleParser struct {
	tokens []ruleToken
	pos    int
}

func parseRuleExpression(input string) (ruleNode, error) {
	lexer := &ruleLexer{input: input}
	var tokens []ruleToken
	for {
		tok, err := lexer.next()
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, tok)
		if tok.kind == tokEOF {
			break
		}
	}
	parser := &ruleParser{tokens: tokens}
	node, err := parser.parseOr()
	if err != nil {
		return nil, err
	}
	if parser.peek().kind != tokEOF {
		return nil, fmt.Errorf("%w: unexpected token %q", ErrInvalidRule, parser.peek().text)
	}
	return node, nil
}

func (p *ruleParser) parseOr() (ruleNode, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokOr {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: "or", left: left, right: right}
	}
	return left, nil
}

func (p *ruleParser) parseAnd() (ruleNode, error) {
	left, err := p.parseCompare()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokAnd {
		p.next()
		right, err := p.parseCompare()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: "and", left: left, right: right}
	}
	return left, nil
}

func (p *ruleParser) parseCompare() (ruleNode, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokCompare {
		return left, nil
	}
	op := p.next().text
	right, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	return compareNode{op: op, left: left, right: right}, nil
}

func (p *ruleParser) parsePrimary() (ruleNode, error) {
	tok := p.next()
	switch tok.kind {
	case tokIdent:
		return identNode{name: NormalizeIdentifier(tok.text)}, nil
	case tokRequest:
		return requestNode{name: tok.text}, nil
	case tokString:
		return literalNode{kind: tokString, text: tok.text, value: tok.text}, nil
	case tokNumber:
		n, _ := strconv.ParseFloat(tok.text, 64)
		return literalNode{kind: tokNumber, text: tok.text, value: n}, nil
	case tokBool:
		return literalNode{kind: tokBool, text: tok.text, value: tok.text == "true"}, nil
	case tokNull:
		return literalNode{kind: tokNull, text: tok.text, value: nil}, nil
	case tokLParen:
		node, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.next().kind != tokRParen {
			return nil, fmt.Errorf("%w: expected closing parenthesis", ErrInvalidRule)
		}
		return node, nil
	default:
		return nil, fmt.Errorf("%w: unexpected token %q", ErrInvalidRule, tok.text)
	}
}

func (p *ruleParser) peek() ruleToken {
	if p.pos >= len(p.tokens) {
		return ruleToken{kind: tokEOF}
	}
	return p.tokens[p.pos]
}

func (p *ruleParser) next() ruleToken {
	tok := p.peek()
	p.pos++
	return tok
}

type SQLExpression struct {
	SQL  string
	Args []any
}

type ruleCompileMode int

const (
	compilePolicy ruleCompileMode = iota
	compileFilter
)

type ruleCompiler struct {
	collection *Collection
	mode       ruleCompileMode
	args       []any
}

func CompileFilter(filter string, collection *Collection) (*SQLExpression, error) {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return &SQLExpression{}, nil
	}
	node, err := parseRuleExpression(filter)
	if err != nil {
		return nil, asFilterError(err)
	}
	compiler := &ruleCompiler{collection: collection, mode: compileFilter}
	sql, err := compiler.compile(node)
	if err != nil {
		return nil, asFilterError(err)
	}
	return &SQLExpression{SQL: sql, Args: compiler.args}, nil
}

func compilePolicyRule(rule *string, collection *Collection) (string, error) {
	if rule == nil {
		return "false", nil
	}
	body := strings.TrimSpace(*rule)
	if body == "" {
		return "true", nil
	}
	node, err := parseRuleExpression(body)
	if err != nil {
		return "", err
	}
	compiler := &ruleCompiler{collection: collection, mode: compilePolicy}
	return compiler.compile(node)
}

func ValidateCollectionRules(collection *Collection) error {
	for _, rule := range []*string{collection.ListRule, collection.ViewRule, collection.CreateRule, collection.UpdateRule, collection.DeleteRule} {
		if _, err := compilePolicyRule(rule, collection); err != nil {
			return asRuleError(err)
		}
	}
	return nil
}

func (c *ruleCompiler) compile(node ruleNode) (string, error) {
	switch n := node.(type) {
	case binaryNode:
		left, err := c.compile(n.left)
		if err != nil {
			return "", err
		}
		right, err := c.compile(n.right)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("((%s) %s (%s))", left, n.op, right), nil
	case compareNode:
		return c.compileCompare(n)
	case identNode:
		if err := c.validateField(n.name); err != nil {
			return "", err
		}
		return quoteIdent(n.name), nil
	case requestNode:
		return compileRequestRef(n.name)
	case literalNode:
		return c.compileLiteral(n)
	default:
		return "", fmt.Errorf("%w: invalid expression", ErrInvalidRule)
	}
}

func (c *ruleCompiler) compileCompare(n compareNode) (string, error) {
	leftNull := isNullLiteral(n.left)
	rightNull := isNullLiteral(n.right)
	if leftNull || rightNull {
		if n.op != "=" && n.op != "!=" {
			return "", fmt.Errorf("%w: null only supports = and !=", ErrInvalidRule)
		}
		var target ruleNode
		if leftNull {
			target = n.right
		} else {
			target = n.left
		}
		sql, err := c.compile(target)
		if err != nil {
			return "", err
		}
		if n.op == "=" {
			return fmt.Sprintf("(%s is null)", sql), nil
		}
		return fmt.Sprintf("(%s is not null)", sql), nil
	}
	left, err := c.compile(n.left)
	if err != nil {
		return "", err
	}
	right, err := c.compile(n.right)
	if err != nil {
		return "", err
	}
	op := n.op
	if op == "=" {
		op = "="
	}
	return fmt.Sprintf("(%s %s %s)", left, op, right), nil
}

func (c *ruleCompiler) validateField(name string) error {
	if name == "id" || name == "created" || name == "updated" {
		return nil
	}
	for _, field := range c.collection.Fields {
		if field.Name == name {
			return nil
		}
	}
	return fmt.Errorf("%w: unknown field %q", ErrInvalidRule, name)
}

func compileRequestRef(name string) (string, error) {
	switch name {
	case "@request.auth.id":
		return "(select _dbo.request_auth_id())", nil
	case "@request.auth.role":
		return "(select _dbo.request_role())", nil
	case "@request.auth.collection":
		return "(select _dbo.request_claim('collection'))", nil
	default:
		return "", fmt.Errorf("%w: unsupported request reference %q", ErrInvalidRule, name)
	}
}

func (c *ruleCompiler) compileLiteral(n literalNode) (string, error) {
	if c.mode == compileFilter {
		c.args = append(c.args, n.value)
		return fmt.Sprintf("$%d", len(c.args)), nil
	}
	switch n.kind {
	case tokString:
		return quoteLiteral(n.text), nil
	case tokNumber:
		return n.text, nil
	case tokBool:
		if n.value.(bool) {
			return "true", nil
		}
		return "false", nil
	case tokNull:
		return "null", nil
	default:
		return "", fmt.Errorf("%w: invalid literal", ErrInvalidRule)
	}
}

func isNullLiteral(node ruleNode) bool {
	lit, ok := node.(literalNode)
	return ok && lit.kind == tokNull
}

func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func asRuleError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrInvalidRule, strings.TrimPrefix(err.Error(), ErrInvalidRule.Error()+": "))
}

func asFilterError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrInvalidFilter, strings.TrimPrefix(err.Error(), ErrInvalidRule.Error()+": "))
}

func syncCollectionPolicies(ctx context.Context, tx pgx.Tx, project *Project, collection *Collection) error {
	if err := ValidateCollectionRules(collection); err != nil {
		return err
	}
	_, roles := ProjectNames(project.Slug)
	table := quoteIdent(project.SchemaName, collection.Name)
	anonRole := quoteIdent(roles.Anon)
	authRole := quoteIdent(roles.Authenticated)
	serviceRole := quoteIdent(roles.Service)

	listSQL, _ := compilePolicyRule(collection.ListRule, collection)
	viewSQL, _ := compilePolicyRule(collection.ViewRule, collection)
	createSQL, _ := compilePolicyRule(collection.CreateRule, collection)
	updateSQL, _ := compilePolicyRule(collection.UpdateRule, collection)
	deleteSQL, _ := compilePolicyRule(collection.DeleteRule, collection)
	selectSQL := fmt.Sprintf(
		`(((select _dbo.request_operation()) = 'list' and (%s)) or ((select _dbo.request_operation()) = 'view' and (%s)) or ((select _dbo.request_operation()) = 'create' and (%s)) or ((select _dbo.request_operation()) = 'update' and (%s)) or ((select _dbo.request_operation()) = 'delete' and (%s)))`,
		listSQL,
		viewSQL,
		createSQL,
		updateSQL,
		deleteSQL,
	)

	statements := []string{
		fmt.Sprintf(`alter table %s enable row level security`, table),
		fmt.Sprintf(`alter table %s force row level security`, table),
		fmt.Sprintf(`grant select, insert, update, delete on table %s to %s, %s, %s`, table, anonRole, authRole, serviceRole),
	}
	for _, policy := range []string{
		collection.Name + "_select_deny",
		collection.Name + "_insert_deny",
		collection.Name + "_update_deny",
		collection.Name + "_delete_deny",
		"dbo_svc_select",
		"dbo_svc_insert",
		"dbo_svc_update",
		"dbo_svc_delete",
		"dbo_client_select",
		"dbo_client_insert",
		"dbo_client_update",
		"dbo_client_delete",
	} {
		statements = append(statements, fmt.Sprintf(`drop policy if exists %s on %s`, quoteIdent(policy), table))
	}
	statements = append(statements,
		fmt.Sprintf(`create policy %s on %s for select to %s using (true)`, quoteIdent("dbo_svc_select"), table, serviceRole),
		fmt.Sprintf(`create policy %s on %s for insert to %s with check (true)`, quoteIdent("dbo_svc_insert"), table, serviceRole),
		fmt.Sprintf(`create policy %s on %s for update to %s using (true) with check (true)`, quoteIdent("dbo_svc_update"), table, serviceRole),
		fmt.Sprintf(`create policy %s on %s for delete to %s using (true)`, quoteIdent("dbo_svc_delete"), table, serviceRole),
		fmt.Sprintf(`create policy %s on %s for select to %s, %s using (%s)`, quoteIdent("dbo_client_select"), table, anonRole, authRole, selectSQL),
		fmt.Sprintf(`create policy %s on %s for insert to %s, %s with check (%s)`, quoteIdent("dbo_client_insert"), table, anonRole, authRole, createSQL),
		fmt.Sprintf(`create policy %s on %s for update to %s, %s using (%s) with check (%s)`, quoteIdent("dbo_client_update"), table, anonRole, authRole, updateSQL, updateSQL),
		fmt.Sprintf(`create policy %s on %s for delete to %s, %s using (%s)`, quoteIdent("dbo_client_delete"), table, anonRole, authRole, deleteSQL),
	)
	for _, stmt := range statements {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
