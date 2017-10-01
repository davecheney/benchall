// Copyright 2014 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package markdown

import (
	"bytes"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Unknwon/com"
	"github.com/microcosm-cc/bluemonday"
	"github.com/russross/blackfriday"
	"golang.org/x/net/html"

	"github.com/gogits/gogs/modules/base"
	"github.com/gogits/gogs/modules/setting"
)

var Sanitizer = bluemonday.UGCPolicy()

// BuildSanitizer initializes sanitizer with allowed attributes based on settings.
// This function should only be called once during entire application lifecycle.
func BuildSanitizer() {
	// Normal markdown-stuff
	Sanitizer.AllowAttrs("class").Matching(regexp.MustCompile(`[\p{L}\p{N}\s\-_',:\[\]!\./\\\(\)&]*`)).OnElements("code")

	// Checkboxes
	Sanitizer.AllowAttrs("type").Matching(regexp.MustCompile(`^checkbox$`)).OnElements("input")
	Sanitizer.AllowAttrs("checked", "disabled").OnElements("input")

	// Custom URL-Schemes
	Sanitizer.AllowURLSchemes(setting.Markdown.CustomURLSchemes...)
}

var validLinksPattern = regexp.MustCompile(`^[a-z][\w-]+://`)

// isLink reports whether link fits valid format.
func isLink(link []byte) bool {
	return validLinksPattern.Match(link)
}

// IsMarkdownFile reports whether name looks like a Markdown file
// based on its extension.
func IsMarkdownFile(name string) bool {
	name = strings.ToLower(name)
	switch filepath.Ext(name) {
	case ".md", ".markdown", ".mdown", ".mkd":
		return true
	}
	return false
}

// IsReadmeFile reports whether name looks like a README file
// based on its extension.
func IsReadmeFile(name string) bool {
	name = strings.ToLower(name)
	if len(name) < 6 {
		return false
	} else if len(name) == 6 {
		return name == "readme"
	}
	return name[:7] == "readme."
}

var (
	// MentionPattern matches string that mentions someone, e.g. @Unknwon
	MentionPattern = regexp.MustCompile(`(\s|^)@[0-9a-zA-Z_\.]+`)

	// CommitPattern matches link to certain commit with or without trailing hash,
	// e.g. https://try.gogs.io/gogs/gogs/commit/d8a994ef243349f321568f9e36d5c3f444b99cae#diff-2
	CommitPattern = regexp.MustCompile(`(\s|^)https?.*commit/[0-9a-zA-Z]+(#+[0-9a-zA-Z-]*)?`)

	// IssueFullPattern matches link to an issue with or without trailing hash,
	// e.g. https://try.gogs.io/gogs/gogs/issues/4#issue-685
	IssueFullPattern = regexp.MustCompile(`(\s|^)https?.*issues/[0-9]+(#+[0-9a-zA-Z-]*)?`)
	// IssueIndexPattern matches string that references to an issue, e.g. #1287
	IssueIndexPattern = regexp.MustCompile(`( |^|\()#[0-9]+\b`)

	// Sha1CurrentPattern matches string that represents a commit SHA, e.g. d8a994ef243349f321568f9e36d5c3f444b99cae
	Sha1CurrentPattern = regexp.MustCompile(`\b[0-9a-f]{40}\b`)
)

// Renderer is a extended version of underlying render object.
type Renderer struct {
	blackfriday.Renderer
	urlPrefix string
}

// Link defines how formal links should be processed to produce corresponding HTML elements.
func (r *Renderer) Link(out *bytes.Buffer, link []byte, title []byte, content []byte) {
	if len(link) > 0 && !isLink(link) {
		if link[0] != '#' {
			link = []byte(path.Join(r.urlPrefix, string(link)))
		}
	}

	r.Renderer.Link(out, link, title, content)
}

// AutoLink defines how auto-detected links should be processed to produce corresponding HTML elements.
// Reference for kind: https://github.com/russross/blackfriday/blob/master/markdown.go#L69-L76
func (r *Renderer) AutoLink(out *bytes.Buffer, link []byte, kind int) {
	if kind != blackfriday.LINK_TYPE_NORMAL {
		r.Renderer.AutoLink(out, link, kind)
		return
	}

	// Since this method could only possibly serve one link at a time,
	// we do not need to find all.
	m := CommitPattern.Find(link)
	if m != nil {
		m = bytes.TrimSpace(m)
		i := strings.Index(string(m), "commit/")
		j := strings.Index(string(m), "#")
		if j == -1 {
			j = len(m)
		}
		out.WriteString(fmt.Sprintf(` <code><a href="%s">%s</a></code>`, m, base.ShortSha(string(m[i+7:j]))))
		return
	}

	m = IssueFullPattern.Find(link)
	if m != nil {
		m = bytes.TrimSpace(m)
		i := strings.Index(string(m), "issues/")
		j := strings.Index(string(m), "#")
		if j == -1 {
			j = len(m)
		}
		out.WriteString(fmt.Sprintf(` <a href="%s">#%s</a>`, m, base.ShortSha(string(m[i+7:j]))))
		return
	}

	r.Renderer.AutoLink(out, link, kind)
}

// ListItem defines how list items should be processed to produce corresponding HTML elements.
func (options *Renderer) ListItem(out *bytes.Buffer, text []byte, flags int) {
	// Detect procedures to draw checkboxes.
	switch {
	case bytes.HasPrefix(text, []byte("[ ] ")):
		text = append([]byte(`<input type="checkbox" disabled="" />`), text[3:]...)
	case bytes.HasPrefix(text, []byte("[x] ")):
		text = append([]byte(`<input type="checkbox" disabled="" checked="" />`), text[3:]...)
	}
	options.Renderer.ListItem(out, text, flags)
}

// Note: this section is for purpose of increase performance and
// reduce memory allocation at runtime since they are constant literals.
var (
	svgSuffix         = []byte(".svg")
	svgSuffixWithMark = []byte(".svg?")
	spaceBytes        = []byte(" ")
	spaceEncodedBytes = []byte("%20")
	space             = " "
	spaceEncoded      = "%20"
)

// Image defines how images should be processed to produce corresponding HTML elements.
func (r *Renderer) Image(out *bytes.Buffer, link []byte, title []byte, alt []byte) {
	prefix := strings.Replace(r.urlPrefix, "/src/", "/raw/", 1)
	if len(link) > 0 {
		if isLink(link) {
			// External link with .svg suffix usually means CI status.
			// TODO: define a keyword to allow non-svg images render as external link.
			if bytes.HasSuffix(link, svgSuffix) || bytes.Contains(link, svgSuffixWithMark) {
				r.Renderer.Image(out, link, title, alt)
				return
			}
		} else {
			if link[0] != '/' {
				prefix += "/"
			}
			link = bytes.Replace([]byte((prefix + string(link))), spaceBytes, spaceEncodedBytes, -1)
			fmt.Println(333, string(link))
		}
	}

	out.WriteString(`<a href="`)
	out.Write(link)
	out.WriteString(`">`)
	r.Renderer.Image(out, link, title, alt)
	out.WriteString("</a>")
}

// cutoutVerbosePrefix cutouts URL prefix including sub-path to
// return a clean unified string of request URL path.
func cutoutVerbosePrefix(prefix string) string {
	count := 0
	for i := 0; i < len(prefix); i++ {
		if prefix[i] == '/' {
			count++
		}
		if count >= 3+setting.AppSubUrlDepth {
			return prefix[:i]
		}
	}
	return prefix
}

// RenderIssueIndexPattern renders issue indexes to corresponding links.
func RenderIssueIndexPattern(rawBytes []byte, urlPrefix string, metas map[string]string) []byte {
	urlPrefix = cutoutVerbosePrefix(urlPrefix)
	ms := IssueIndexPattern.FindAll(rawBytes, -1)
	for _, m := range ms {
		var space string
		if m[0] != '#' {
			space = string(m[0])
			m = m[1:]
		}
		if metas == nil {
			rawBytes = bytes.Replace(rawBytes, m, []byte(fmt.Sprintf(`%s<a href="%s/issues/%s">%s</a>`,
				space, urlPrefix, m[1:], m)), 1)
		} else {
			// Support for external issue tracker
			metas["index"] = string(m[1:])
			rawBytes = bytes.Replace(rawBytes, m, []byte(fmt.Sprintf(`%s<a href="%s">%s</a>`,
				space, com.Expand(metas["format"], metas), m)), 1)
		}
	}
	return rawBytes
}

// RenderSha1CurrentPattern renders SHA1 strings to corresponding links that assumes in the same repository.
func RenderSha1CurrentPattern(rawBytes []byte, urlPrefix string) []byte {
	ms := Sha1CurrentPattern.FindAll(rawBytes, -1)
	for _, m := range ms {
		rawBytes = bytes.Replace(rawBytes, m, []byte(fmt.Sprintf(
			`<a href="%s/commit/%s"><code>%s</code></a>`, urlPrefix, m, base.ShortSha(string(m)))), -1)
	}
	return rawBytes
}

// RenderSpecialLink renders mentions, indexes and SHA1 strings to corresponding links.
func RenderSpecialLink(rawBytes []byte, urlPrefix string, metas map[string]string) []byte {
	ms := MentionPattern.FindAll(rawBytes, -1)
	for _, m := range ms {
		m = bytes.TrimSpace(m)
		rawBytes = bytes.Replace(rawBytes, m,
			[]byte(fmt.Sprintf(`<a href="%s/%s">%s</a>`, setting.AppSubUrl, m[1:], m)), -1)
	}

	rawBytes = RenderIssueIndexPattern(rawBytes, urlPrefix, metas)
	rawBytes = RenderSha1CurrentPattern(rawBytes, urlPrefix)
	return rawBytes
}

// RenderRaw renders Markdown to HTML without handling special links.
func RenderRaw(body []byte, urlPrefix string) []byte {
	htmlFlags := 0
	htmlFlags |= blackfriday.HTML_SKIP_STYLE
	htmlFlags |= blackfriday.HTML_OMIT_CONTENTS
	renderer := &Renderer{
		Renderer:  blackfriday.HtmlRenderer(htmlFlags, "", ""),
		urlPrefix: urlPrefix,
	}

	// set up the parser
	extensions := 0
	extensions |= blackfriday.EXTENSION_NO_INTRA_EMPHASIS
	extensions |= blackfriday.EXTENSION_TABLES
	extensions |= blackfriday.EXTENSION_FENCED_CODE
	extensions |= blackfriday.EXTENSION_AUTOLINK
	extensions |= blackfriday.EXTENSION_STRIKETHROUGH
	extensions |= blackfriday.EXTENSION_SPACE_HEADERS
	extensions |= blackfriday.EXTENSION_NO_EMPTY_LINE_BEFORE_BLOCK

	if setting.Markdown.EnableHardLineBreak {
		extensions |= blackfriday.EXTENSION_HARD_LINE_BREAK
	}

	body = blackfriday.Markdown(body, renderer, extensions)
	return body
}

var (
	leftAngleBracket  = []byte("</")
	rightAngleBracket = []byte(">")
)

var noEndTags = []string{"img", "input", "br", "hr"}

// PostProcess treats different types of HTML differently,
// and only renders special links for plain text blocks.
func PostProcess(rawHtml []byte, urlPrefix string, metas map[string]string) []byte {
	startTags := make([]string, 0, 5)
	var buf bytes.Buffer
	tokenizer := html.NewTokenizer(bytes.NewReader(rawHtml))

OUTER_LOOP:
	for html.ErrorToken != tokenizer.Next() {
		token := tokenizer.Token()
		switch token.Type {
		case html.TextToken:
			buf.Write(RenderSpecialLink([]byte(token.String()), urlPrefix, metas))

		case html.StartTagToken:
			buf.WriteString(token.String())
			tagName := token.Data
			// If this is an excluded tag, we skip processing all output until a close tag is encountered.
			if strings.EqualFold("a", tagName) || strings.EqualFold("code", tagName) || strings.EqualFold("pre", tagName) {
				stackNum := 1
				for html.ErrorToken != tokenizer.Next() {
					token = tokenizer.Token()

					// Copy the token to the output verbatim
					buf.WriteString(token.String())

					if token.Type == html.StartTagToken {
						stackNum++
					}

					// If this is the close tag to the outer-most, we are done
					if token.Type == html.EndTagToken {
						stackNum--

						if stackNum <= 0 && strings.EqualFold(tagName, token.Data) {
							break
						}
					}
				}
				continue OUTER_LOOP
			}

			if !com.IsSliceContainsStr(noEndTags, token.Data) {
				startTags = append(startTags, token.Data)
			}

		case html.EndTagToken:
			if len(startTags) == 0 {
				buf.WriteString(token.String())
				break
			}

			buf.Write(leftAngleBracket)
			buf.WriteString(startTags[len(startTags)-1])
			buf.Write(rightAngleBracket)
			startTags = startTags[:len(startTags)-1]
		default:
			buf.WriteString(token.String())
		}
	}

	if io.EOF == tokenizer.Err() {
		return buf.Bytes()
	}

	// If we are not at the end of the input, then some other parsing error has occurred,
	// so return the input verbatim.
	return rawHtml
}

// Render renders Markdown to HTML with special links.
func Render(rawBytes []byte, urlPrefix string, metas map[string]string) []byte {
	urlPrefix = strings.Replace(urlPrefix, space, spaceEncoded, -1)
	result := RenderRaw(rawBytes, urlPrefix)
	result = PostProcess(result, urlPrefix, metas)
	result = Sanitizer.SanitizeBytes(result)
	return result
}

// RenderString renders Markdown to HTML with special links and returns string type.
func RenderString(raw, urlPrefix string, metas map[string]string) string {
	return string(Render([]byte(raw), urlPrefix, metas))
}
