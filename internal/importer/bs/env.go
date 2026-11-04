package bs

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// ReadEnv parses a Laravel .env file the way vlucas/phpdotenv reads the common cases:
// KEY=VALUE lines, # comments, an optional "export " prefix, single quotes (literal),
// double quotes (\n, \" and \\ escapes, ${NAME} expansion) and unquoted values (a #
// starts a comment, ${NAME} expands). Later keys override earlier ones.
func ReadEnv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	env := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\ufeff"))
		if line == "" || line[0] == '#' {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, raw, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" || strings.ContainsAny(key, " \t\"'") {
			return nil, fmt.Errorf("%s:%d: not a KEY=VALUE line", path, n)
		}
		v, err := envValue(strings.TrimSpace(raw), env)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %s: %w", path, n, key, err)
		}
		env[key] = v
	}
	return env, sc.Err()
}

func envValue(raw string, env map[string]string) (string, error) {
	switch {
	case raw == "":
		return "", nil
	case raw[0] == '\'':
		end := strings.IndexByte(raw[1:], '\'')
		if end < 0 {
			return "", fmt.Errorf("unterminated quote")
		}
		return raw[1 : 1+end], nil
	case raw[0] == '"':
		var b strings.Builder
		for i := 1; i < len(raw); i++ {
			c := raw[i]
			switch {
			case c == '\\' && i+1 < len(raw):
				i++
				switch raw[i] {
				case 'n':
					b.WriteByte('\n')
				case 't':
					b.WriteByte('\t')
				default:
					b.WriteByte(raw[i])
				}
			case c == '"':
				return expand(b.String(), env), nil
			default:
				b.WriteByte(c)
			}
		}
		return "", fmt.Errorf("unterminated quote")
	default:
		// phpdotenv ends an unquoted value at the first #, space before it or not.
		if i := strings.IndexByte(raw, '#'); i >= 0 {
			raw = strings.TrimSpace(raw[:i])
		}
		return expand(raw, env), nil
	}
}

// expand replaces ${NAME} with an earlier key or the process environment. A bare $ is
// literal (os.Expand would also expand $NAME, eating part of a password like "pa$word").
func expand(s string, env map[string]string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, "${")
		if i < 0 {
			break
		}
		end := strings.IndexByte(s[i:], '}')
		if end < 0 {
			break
		}
		name := s[i+2 : i+end]
		v, ok := env[name]
		if !ok {
			v = os.Getenv(name)
		}
		b.WriteString(s[:i])
		b.WriteString(v)
		s = s[i+end+1:]
	}
	b.WriteString(s)
	return b.String()
}
