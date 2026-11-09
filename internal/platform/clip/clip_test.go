package clip

import (
	"testing"
	"unicode/utf8"
)

func TestBytes(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 3, "hel"},
		{"hello", 0, ""},
		{"héllo", 2, "h"},       // é is 2 bytes: never cut in half
		{"héllo", 3, "hé"},      //
		{"皮肤站", 4, "皮"},         // 3-byte runes
		{"皮肤站", 2, ""},          //
		{"a😀b", 4, "a"},         // 4-byte rune
		{"a😀b", 5, "a😀"},        //
		{"\xff\xfe", 1, "\xff"}, // invalid UTF-8 is cut by bytes, not looped on
	} {
		got := Bytes(c.in, c.n)
		if got != c.want {
			t.Errorf("Bytes(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
		if len(got) > c.n {
			t.Errorf("Bytes(%q, %d) is %d bytes long", c.in, c.n, len(got))
		}
		if utf8.ValidString(c.in) && !utf8.ValidString(got) {
			t.Errorf("Bytes(%q, %d) = %q is not valid UTF-8", c.in, c.n, got)
		}
	}
}

func TestRunes(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 3, "hel"},
		{"皮肤站", 2, "皮肤"},
		{"a😀b", 2, "a😀"},
		{"", 3, ""},
		{"abc", 0, ""},
	} {
		if got := Runes(c.in, c.n); got != c.want {
			t.Errorf("Runes(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
