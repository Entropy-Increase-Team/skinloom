package settings

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// checkMessage reports whether s is a message the frontend can format: ICU
// MessageFormat as @formatjs/icu-messageformat-parser reads it for next-intl (tags on,
// plural and select need an other clause). A text it rejects would show its message
// key instead (ADR 0039). Whether the text keeps the default's placeholders is checked
// by the translations page, which has the default texts; the server does not.
func checkMessage(s string) error {
	p := &icuParser{s: s}
	if err := p.message(0, "", false); err != nil {
		return err
	}
	return nil
}

type icuParser struct {
	s   string
	pos int
}

func (p *icuParser) eof() bool { return p.pos >= len(p.s) }

func (p *icuParser) peekRune(off int) rune {
	i := p.pos
	for ; off > 0 && i < len(p.s); off-- {
		_, n := utf8.DecodeRuneInString(p.s[i:])
		i += n
	}
	if i >= len(p.s) {
		return -1
	}
	r, _ := utf8.DecodeRuneInString(p.s[i:])
	return r
}

func (p *icuParser) char() rune { return p.peekRune(0) }

func (p *icuParser) bump() {
	if !p.eof() {
		_, n := utf8.DecodeRuneInString(p.s[p.pos:])
		p.pos += n
	}
}

func (p *icuParser) bumpIf(prefix string) bool {
	if strings.HasPrefix(p.s[p.pos:], prefix) {
		p.pos += len(prefix)
		return true
	}
	return false
}

// bumpSpace skips Pattern_White_Space, as formatjs does between tokens.
func (p *icuParser) bumpSpace() {
	for !p.eof() && unicode.Is(unicode.Pattern_White_Space, p.char()) {
		p.bump()
	}
}

// identifier reads [^\p{White_Space}\p{Pattern_Syntax}]*.
func (p *icuParser) identifier() string {
	start := p.pos
	for !p.eof() {
		r := p.char()
		if unicode.Is(unicode.White_Space, r) || unicode.Is(unicode.Pattern_Syntax, r) {
			break
		}
		p.bump()
	}
	return p.s[start:p.pos]
}

func pluralLike(argType string) bool { return argType == "plural" || argType == "selectordinal" }

// message reads elements up to the end, the } that closes a nested message
// (nesting > 0) or the </ of an enclosing tag (inTag).
func (p *icuParser) message(nesting int, parentArg string, inTag bool) error {
	for !p.eof() {
		switch c := p.char(); {
		case c == '{':
			if err := p.argument(nesting, inTag); err != nil {
				return err
			}
		case c == '}' && nesting > 0:
			return nil
		case c == '#' && pluralLike(parentArg):
			p.bump()
		case c == '<' && p.peekRune(1) == '/':
			if inTag {
				return nil
			}
			return errors.New("a closing tag without its opening tag")
		case c == '<' && isAlpha(p.peekRune(1)):
			if err := p.tag(nesting, parentArg); err != nil {
				return err
			}
		default:
			start := p.pos
			if p.literal(nesting, parentArg); p.pos == start {
				// Cannot happen: every character not handled above starts text. Kept so
				// that a mistake here rejects a text instead of looping on it.
				return fmt.Errorf("unexpected %q", c)
			}
		}
	}
	return nil
}

// literal reads text up to the next special character, taking ICU's apostrophe
// quoting into account.
func (p *icuParser) literal(nesting int, parentArg string) {
	for !p.eof() {
		if p.quote(parentArg) {
			continue
		}
		c := p.char()
		if c == '<' {
			if isAlpha(p.peekRune(1)) || p.peekRune(1) == '/' {
				return
			}
			p.bump() // a < that starts no tag is text
			continue
		}
		if c == '{' || (c == '#' && pluralLike(parentArg)) || (c == '}' && nesting > 0) {
			return
		}
		p.bump()
	}
}

// quote reads a doubled apostrophe (one apostrophe of text) or a quoted run: an
// apostrophe before { } < > or (in a plural) #, up to the closing apostrophe or the end.
func (p *icuParser) quote(parentArg string) bool {
	if p.char() != '\'' {
		return false
	}
	switch next := p.peekRune(1); {
	case next == '\'':
		p.bump()
		p.bump()
		return true
	case next == '{' || next == '<' || next == '>' || next == '}':
	case next == '#' && pluralLike(parentArg):
	default:
		return false
	}
	p.bump() // the opening apostrophe
	for !p.eof() {
		if p.char() == '\'' {
			if p.peekRune(1) == '\'' {
				p.bump()
				p.bump()
				continue
			}
			p.bump()
			break
		}
		p.bump()
	}
	return true
}

func (p *icuParser) argument(nesting int, inTag bool) error {
	p.bump() // {
	p.bumpSpace()
	if p.eof() {
		return errors.New("a { is not closed")
	}
	if p.char() == '}' {
		return errors.New("{} names no placeholder")
	}
	name := p.identifier()
	if name == "" {
		return fmt.Errorf("a placeholder name cannot start with %q", p.char())
	}
	p.bumpSpace()
	if p.eof() {
		return fmt.Errorf("{%s is not closed", name)
	}
	switch p.char() {
	case '}':
		p.bump()
		return nil
	case ',':
		p.bump()
		p.bumpSpace()
		if p.eof() {
			return fmt.Errorf("{%s, is not closed", name)
		}
		return p.argumentOptions(name, nesting, inTag)
	default:
		return fmt.Errorf("{%s is followed by %q, not } or a comma", name, p.char())
	}
}

func (p *icuParser) argumentOptions(name string, nesting int, inTag bool) error {
	argType := p.identifier()
	switch argType {
	case "":
		return fmt.Errorf("{%s, needs a type such as plural or select", name)
	case "number", "date", "time":
		p.bumpSpace()
		style := ""
		if p.bumpIf(",") {
			p.bumpSpace()
			var err error
			if style, err = p.simpleStyle(); err != nil {
				return err
			}
			if style = strings.TrimRightFunc(style, unicode.IsSpace); style == "" {
				return fmt.Errorf("{%s, %s, needs a style", name, argType)
			}
		}
		if !p.bumpIf("}") {
			return fmt.Errorf("{%s, %s is not closed", name, argType)
		}
		if skeleton, ok := strings.CutPrefix(style, "::"); ok && !validSkeleton(argType, strings.TrimLeftFunc(skeleton, unicode.IsSpace)) {
			return fmt.Errorf("{%s, %s, %s}: not a valid skeleton", name, argType, style)
		}
		return nil
	case "plural", "selectordinal", "select":
		p.bumpSpace()
		if !p.bumpIf(",") {
			return fmt.Errorf("{%s, %s needs a comma and its cases", name, argType)
		}
		p.bumpSpace()
		selector := p.identifier()
		if argType != "select" && selector == "offset" {
			if !p.bumpIf(":") {
				return fmt.Errorf("{%s, %s, offset needs a number", name, argType)
			}
			p.bumpSpace()
			if !p.integer() {
				return fmt.Errorf("{%s, %s, offset needs a whole number", name, argType)
			}
			p.bumpSpace()
			selector = p.identifier()
		}
		if err := p.options(name, argType, selector, nesting, inTag); err != nil {
			return err
		}
		if !p.bumpIf("}") {
			return fmt.Errorf("{%s, %s is not closed", name, argType)
		}
		return nil
	default:
		return fmt.Errorf("{%s, %s: the type must be number, date, time, plural, selectordinal or select", name, argType)
	}
}

// options reads the cases of a plural or select: selector {message} …, with other.
func (p *icuParser) options(name, argType, selector string, nesting int, inTag bool) error {
	seen := map[string]bool{}
	for {
		if selector == "" {
			if argType == "select" || !p.bumpIf("=") {
				break
			}
			start := p.pos
			if !p.integer() {
				return fmt.Errorf("{%s, %s: = must be followed by a whole number", name, argType)
			}
			selector = "=" + p.s[start:p.pos]
		}
		if seen[selector] {
			return fmt.Errorf("{%s, %s: the case %s appears twice", name, argType, selector)
		}
		seen[selector] = true
		p.bumpSpace()
		if !p.bumpIf("{") {
			return fmt.Errorf("{%s, %s: the case %s needs its text in { }", name, argType, selector)
		}
		if err := p.message(nesting+1, argType, inTag); err != nil {
			return err
		}
		if !p.bumpIf("}") {
			return fmt.Errorf("{%s, %s: the text of case %s is not closed", name, argType, selector)
		}
		p.bumpSpace()
		selector = p.identifier()
	}
	if len(seen) == 0 {
		return fmt.Errorf("{%s, %s needs cases", name, argType)
	}
	if !seen["other"] {
		return fmt.Errorf("{%s, %s needs an other case", name, argType)
	}
	return nil
}

// validSkeleton: a skeleton (after ::) is not empty, and a number skeleton's
// /-separated options are not empty either.
func validSkeleton(argType, skeleton string) bool {
	if skeleton == "" {
		return false
	}
	if argType != "number" {
		return true
	}
	for _, token := range strings.Fields(skeleton) {
		for _, option := range strings.Split(token, "/")[1:] {
			if option == "" {
				return false
			}
		}
	}
	return true
}

// integer reads an optional sign and digits.
func (p *icuParser) integer() bool {
	if !p.bumpIf("+") {
		p.bumpIf("-")
	}
	start := p.pos
	for !p.eof() && p.char() >= '0' && p.char() <= '9' {
		p.bump()
	}
	return p.pos > start
}

// simpleStyle reads the style of number, date or time up to its closing }.
func (p *icuParser) simpleStyle() (string, error) {
	start, depth := p.pos, 0
	for !p.eof() {
		switch p.char() {
		case '\'':
			p.bump()
			i := strings.IndexByte(p.s[p.pos:], '\'')
			if i < 0 {
				return "", errors.New("a quote in a style is not closed")
			}
			p.pos += i + 1
		case '{':
			depth++
			p.bump()
		case '}':
			if depth == 0 {
				return p.s[start:p.pos], nil
			}
			depth--
			p.bump()
		default:
			p.bump()
		}
	}
	return p.s[start:p.pos], nil
}

// tag reads <name>…</name>; <name/> is text.
func (p *icuParser) tag(nesting int, parentArg string) error {
	p.bump() // <
	name := p.tagName()
	p.bumpSpace()
	if p.bumpIf("/>") {
		return nil
	}
	if !p.bumpIf(">") {
		return fmt.Errorf("<%s is not a well-formed tag", name)
	}
	if err := p.message(nesting+1, parentArg, true); err != nil {
		return err
	}
	if !p.bumpIf("</") {
		return fmt.Errorf("<%s> is not closed", name)
	}
	if p.eof() || !isAlpha(p.char()) {
		return fmt.Errorf("</ after <%s> is not a well-formed closing tag", name)
	}
	if closing := p.tagName(); closing != name {
		return fmt.Errorf("<%s> is closed by </%s>", name, closing)
	}
	p.bumpSpace()
	if !p.bumpIf(">") {
		return fmt.Errorf("</%s is not a well-formed closing tag", name)
	}
	return nil
}

func (p *icuParser) tagName() string {
	start := p.pos
	for !p.eof() && isTagNameChar(p.char()) {
		p.bump()
	}
	return p.s[start:p.pos]
}

func isAlpha(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }

// isTagNameChar is formatjs's _isPotentialElementNameChar (HTML's PCENChar).
func isTagNameChar(c rune) bool {
	return c == '-' || c == '.' || (c >= '0' && c <= '9') || c == '_' || isAlpha(c) || c == 0xb7 ||
		(c >= 0xc0 && c <= 0xd6) || (c >= 0xd8 && c <= 0xf6) || (c >= 0xf8 && c <= 0x37d) ||
		(c >= 0x37f && c <= 0x1fff) || (c >= 0x200c && c <= 0x200d) || (c >= 0x203f && c <= 0x2040) ||
		(c >= 0x2070 && c <= 0x218f) || (c >= 0x2c00 && c <= 0x2fef) || (c >= 0x3001 && c <= 0xd7ff) ||
		(c >= 0xf900 && c <= 0xfdcf) || (c >= 0xfdf0 && c <= 0xfffd) || (c >= 0x10000 && c <= 0xeffff)
}
