package main

import (
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// ExtractCodeBlocks returns the plain text of each <pre> block in a message
// body, so a script can be copied without the prose around it. Teams sends
// code blocks as <pre><code> with <br> line breaks and syntax-highlight
// <span>s inside. When a message has no <pre> block, inline <code> spans are
// returned instead, since short commands are often sent that way.
func ExtractCodeBlocks(content string) []string {
	if blocks := extractTagText(content, "pre"); len(blocks) > 0 {
		return blocks
	}
	return extractTagText(content, "code")
}

func extractTagText(content, tag string) []string {
	var blocks []string
	var buf strings.Builder
	depth := 0
	z := html.NewTokenizer(strings.NewReader(content))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		tok := z.Token()
		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			if tok.Data == tag && tt == html.StartTagToken {
				if depth == 0 {
					buf.Reset()
				}
				depth++
			} else if depth > 0 && tok.Data == "br" {
				buf.WriteByte('\n')
			}
		case html.EndTagToken:
			if tok.Data == tag && depth > 0 {
				depth--
				if depth == 0 {
					text := strings.TrimRight(strings.ReplaceAll(buf.String(), " ", " "), "\n")
					if strings.TrimSpace(text) != "" {
						blocks = append(blocks, text)
					}
				}
			}
		case html.TextToken:
			if depth > 0 {
				buf.WriteString(tok.Data)
			}
		}
	}
	return blocks
}

// codeBlockLabel is a one-line picker label: the first non-blank line plus
// the block's line count.
func codeBlockLabel(block string) string {
	lines := strings.Split(block, "\n")
	first := ""
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			first = strings.TrimSpace(l)
			break
		}
	}
	if len(lines) > 1 {
		return first + "  (" + strconv.Itoa(len(lines)) + " lines)"
	}
	return first
}
