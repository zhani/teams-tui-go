package main

import (
	"reflect"
	"testing"
)

func TestExtractCodeBlocks(t *testing.T) {
	tests := []struct {
		name string
		html string
		want []string
	}{
		{
			name: "teams highlighted block skips surrounding prose",
			html: `<p>Try this:</p><pre class="language-bash" itemid="x"><code><span class="k">echo</span> a &amp;&amp; b<br>ls&nbsp;-la<br></code></pre><p>thanks</p>`,
			want: []string{"echo a && b\nls -la"},
		},
		{
			name: "multiple blocks and literal newlines",
			html: "<pre><code>one\ntwo\n</code></pre><p>x</p><pre><code>three</code></pre>",
			want: []string{"one\ntwo", "three"},
		},
		{
			name: "inline code is the fallback",
			html: `<p>Run <code>make test</code> then <code>go vet</code></p>`,
			want: []string{"make test", "go vet"},
		},
		{
			name: "inline code ignored when a block exists",
			html: `<p>Run <code>x</code></p><pre><code>y</code></pre>`,
			want: []string{"y"},
		},
		{name: "no code", html: `<p>hello</p>`, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractCodeBlocks(tt.html); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCodeBlockLabel(t *testing.T) {
	if got := codeBlockLabel("\n  #!/bin/sh\necho hi"); got != "#!/bin/sh  (3 lines)" {
		t.Errorf("got %q", got)
	}
	if got := codeBlockLabel("ls"); got != "ls" {
		t.Errorf("got %q", got)
	}
}
