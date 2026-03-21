package main

import "testing"

func TestStripHTML(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"<b>Hello</b>", "Hello"},
		{"<p>Some <em>text</em>.</p>", "Some text."},
		{"No tags here", "No tags here"},
		{"&amp; &lt; &gt; &quot; &#39;", "& < > \" '"},
		// &nbsp; at end is trimmed by TrimSpace
		{"hello &nbsp;", "hello"},
		// &nbsp; in the middle stays as a space
		{"a&nbsp;b", "a b"},
		{"  spaced  ", "spaced"},
		{"<br/><hr/>", ""},
		{"", ""},
	}
	for _, c := range cases {
		got := stripHTML(c.in)
		if got != c.want {
			t.Errorf("stripHTML(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitize(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"normal", "normal"},
		{"path/sep", "path-sep"},
		{"back\\slash", "back-slash"},
		{"colon:value", "colon-value"},
		{"star*glob", "starglob"},
		{"question?mark", "questionmark"},
		{`quote"here`, "quotehere"},
		{"less<than", "lessthan"},
		{"greater>than", "greaterthan"},
		{"pipe|char", "pipe-char"},
		{"  trimmed  ", "trimmed"},
		{"", ""},
	}
	for _, c := range cases {
		got := sanitize(c.in)
		if got != c.want {
			t.Errorf("sanitize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
