// Copyright 2014 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package template

import (
	"container/list"
	"encoding/json"
	"fmt"
	"html/template"
	"runtime"
	"strings"
	"time"

	"golang.org/x/net/html/charset"
	"golang.org/x/text/transform"

	"github.com/gogits/gogs/models"
	"github.com/gogits/gogs/modules/base"
	"github.com/gogits/gogs/modules/markdown"
	"github.com/gogits/gogs/modules/setting"
)

func NewFuncMap() []template.FuncMap {
	return []template.FuncMap{map[string]interface{}{
		"GoVer": func() string {
			return strings.Title(runtime.Version())
		},
		"UseHTTPS": func() bool {
			return strings.HasPrefix(setting.AppUrl, "https")
		},
		"AppName": func() string {
			return setting.AppName
		},
		"AppSubUrl": func() string {
			return setting.AppSubUrl
		},
		"AppUrl": func() string {
			return setting.AppUrl
		},
		"AppVer": func() string {
			return setting.AppVer
		},
		"AppDomain": func() string {
			return setting.Domain
		},
		"DisableGravatar": func() bool {
			return setting.DisableGravatar
		},
		"LoadTimes": func(startTime time.Time) string {
			return fmt.Sprint(time.Since(startTime).Nanoseconds()/1e6) + "ms"
		},
		"AvatarLink":   base.AvatarLink,
		"Safe":         Safe,
		"Str2html":     Str2html,
		"TimeSince":    base.TimeSince,
		"RawTimeSince": base.RawTimeSince,
		"FileSize":     base.FileSize,
		"Subtract":     base.Subtract,
		"Add": func(a, b int) int {
			return a + b
		},
		"ActionIcon": ActionIcon,
		"DateFmtLong": func(t time.Time) string {
			return t.Format(time.RFC1123Z)
		},
		"DateFmtShort": func(t time.Time) string {
			return t.Format("Jan 02, 2006")
		},
		"List": List,
		"Mail2Domain": func(mail string) string {
			if !strings.Contains(mail, "@") {
				return "try.gogs.io"
			}

			return strings.SplitN(mail, "@", 2)[1]
		},
		"SubStr": func(str string, start, length int) string {
			if len(str) == 0 {
				return ""
			}
			end := start + length
			if length == -1 {
				end = len(str)
			}
			if len(str) < end {
				return str
			}
			return str[start:end]
		},
		"DiffTypeToStr":     DiffTypeToStr,
		"DiffLineTypeToStr": DiffLineTypeToStr,
		"Sha1":              Sha1,
		"ShortSha":          base.ShortSha,
		"MD5":               base.EncodeMD5,
		"ActionContent2Commits": ActionContent2Commits,
		"ToUtf8":                ToUtf8,
		"EscapePound": func(str string) string {
			return strings.Replace(strings.Replace(str, "%", "%25", -1), "#", "%23", -1)
		},
		"RenderCommitMessage": RenderCommitMessage,
		"ThemeColorMetaTag": func() string {
			return setting.ThemeColorMetaTag
		},
	}}
}

func Safe(raw string) template.HTML {
	return template.HTML(raw)
}

func Str2html(raw string) template.HTML {
	return template.HTML(markdown.Sanitizer.Sanitize(raw))
}

func Range(l int) []int {
	return make([]int, l)
}

func List(l *list.List) chan interface{} {
	e := l.Front()
	c := make(chan interface{})
	go func() {
		for e != nil {
			c <- e.Value
			e = e.Next()
		}
		close(c)
	}()
	return c
}

func Sha1(str string) string {
	return base.EncodeSha1(str)
}

func ToUtf8WithErr(content []byte) (error, string) {
	charsetLabel, err := base.DetectEncoding(content)
	if err != nil {
		return err, ""
	} else if charsetLabel == "UTF-8" {
		return nil, string(content)
	}

	encoding, _ := charset.Lookup(charsetLabel)
	if encoding == nil {
		return fmt.Errorf("Unknown encoding: %s", charsetLabel), string(content)
	}

	// If there is an error, we concatenate the nicely decoded part and the
	// original left over. This way we won't loose data.
	result, n, err := transform.String(encoding.NewDecoder(), string(content))
	if err != nil {
		result = result + string(content[n:])
	}

	return err, result
}

func ToUtf8(content string) string {
	_, res := ToUtf8WithErr([]byte(content))
	return res
}

// Replaces all prefixes 'old' in 's' with 'new'.
func ReplaceLeft(s, old, new string) string {
	old_len, new_len, i, n := len(old), len(new), 0, 0
	for ; i < len(s) && strings.HasPrefix(s[i:], old); n += 1 {
		i += old_len
	}

	// simple optimization
	if n == 0 {
		return s
	}

	// allocating space for the new string
	newLen := n*new_len + len(s[i:])
	replacement := make([]byte, newLen, newLen)

	j := 0
	for ; j < n*new_len; j += new_len {
		copy(replacement[j:j+new_len], new)
	}

	copy(replacement[j:], s[i:])
	return string(replacement)
}

// RenderCommitMessage renders commit message with XSS-safe and special links.
func RenderCommitMessage(full bool, msg, urlPrefix string, metas map[string]string) template.HTML {
	cleanMsg := template.HTMLEscapeString(msg)
	fullMessage := string(markdown.RenderIssueIndexPattern([]byte(cleanMsg), urlPrefix, metas))
	msgLines := strings.Split(strings.TrimSpace(fullMessage), "\n")
	numLines := len(msgLines)
	if numLines == 0 {
		return template.HTML("")
	} else if !full {
		return template.HTML(msgLines[0])
	} else if numLines == 1 || (numLines >= 2 && len(msgLines[1]) == 0) {
		// First line is a header, standalone or followed by empty line
		header := fmt.Sprintf("<h3>%s</h3>", msgLines[0])
		if numLines >= 2 {
			fullMessage = header + fmt.Sprintf("\n<pre>%s</pre>", strings.Join(msgLines[2:], "\n"))
		} else {
			fullMessage = header
		}
	} else {
		// Non-standard git message, there is no header line
		fullMessage = fmt.Sprintf("<h4>%s</h4>", strings.Join(msgLines, "<br>"))
	}
	return template.HTML(fullMessage)
}

type Actioner interface {
	GetOpType() int
	GetActUserName() string
	GetActEmail() string
	GetRepoUserName() string
	GetRepoName() string
	GetRepoPath() string
	GetRepoLink() string
	GetBranch() string
	GetContent() string
	GetCreate() time.Time
	GetIssueInfos() []string
}

// ActionIcon accepts a int that represents action operation type
// and returns a icon class name.
func ActionIcon(opType int) string {
	switch opType {
	case 1, 8: // Create and transfer repository
		return "repo"
	case 5, 9: // Commit repository
		return "git-commit"
	case 6: // Create issue
		return "issue-opened"
	case 7: // New pull request
		return "git-pull-request"
	case 10: // Comment issue
		return "comment"
	case 11: // Merge pull request
		return "git-merge"
	case 12, 14: // Close issue or pull request
		return "issue-closed"
	case 13, 15: // Reopen issue or pull request
		return "issue-reopened"
	default:
		return "invalid type"
	}
}

func ActionContent2Commits(act Actioner) *models.PushCommits {
	push := models.NewPushCommits()
	if err := json.Unmarshal([]byte(act.GetContent()), push); err != nil {
		return nil
	}
	return push
}

func DiffTypeToStr(diffType int) string {
	diffTypes := map[int]string{
		1: "add", 2: "modify", 3: "del", 4: "rename",
	}
	return diffTypes[diffType]
}

func DiffLineTypeToStr(diffType int) string {
	switch diffType {
	case 2:
		return "add"
	case 3:
		return "del"
	case 4:
		return "tag"
	}
	return "same"
}
