// Copyright 2016 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package models

import (
	"fmt"
	"strings"
	"time"

	"github.com/Unknwon/com"
	"github.com/go-xorm/xorm"

	"github.com/gogits/gogs/modules/log"
)

// CommentType defines whether a comment is just a simple comment, an action (like close) or a reference.
type CommentType int

const (
	// Plain comment, can be associated with a commit (CommitID > 0) and a line (LineNum > 0)
	COMMENT_TYPE_COMMENT CommentType = iota
	COMMENT_TYPE_REOPEN
	COMMENT_TYPE_CLOSE

	// References.
	COMMENT_TYPE_ISSUE_REF
	// Reference from a commit (not part of a pull request)
	COMMENT_TYPE_COMMIT_REF
	// Reference from a comment
	COMMENT_TYPE_COMMENT_REF
	// Reference from a pull request
	COMMENT_TYPE_PULL_REF
)

type CommentTag int

const (
	COMMENT_TAG_NONE CommentTag = iota
	COMMENT_TAG_POSTER
	COMMENT_TAG_WRITER
	COMMENT_TAG_OWNER
)

// Comment represents a comment in commit and issue page.
type Comment struct {
	ID              int64 `xorm:"pk autoincr"`
	Type            CommentType
	PosterID        int64
	Poster          *User `xorm:"-"`
	IssueID         int64 `xorm:"INDEX"`
	CommitID        int64
	Line            int64
	Content         string `xorm:"TEXT"`
	RenderedContent string `xorm:"-"`

	Created     time.Time `xorm:"-"`
	CreatedUnix int64

	// Reference issue in commit message
	CommitSHA string `xorm:"VARCHAR(40)"`

	Attachments []*Attachment `xorm:"-"`

	// For view issue page.
	ShowTag CommentTag `xorm:"-"`
}

func (c *Comment) BeforeInsert() {
	c.CreatedUnix = time.Now().UTC().Unix()
}

func (c *Comment) AfterSet(colName string, _ xorm.Cell) {
	var err error
	switch colName {
	case "id":
		c.Attachments, err = GetAttachmentsByCommentID(c.ID)
		if err != nil {
			log.Error(3, "GetAttachmentsByCommentID[%d]: %v", c.ID, err)
		}

	case "poster_id":
		c.Poster, err = GetUserByID(c.PosterID)
		if err != nil {
			if IsErrUserNotExist(err) {
				c.PosterID = -1
				c.Poster = NewFakeUser()
			} else {
				log.Error(3, "GetUserByID[%d]: %v", c.ID, err)
			}
		}
	case "created_unix":
		c.Created = time.Unix(c.CreatedUnix, 0).Local()
	}
}

func (c *Comment) AfterDelete() {
	_, err := DeleteAttachmentsByComment(c.ID, true)

	if err != nil {
		log.Info("Could not delete files for comment %d on issue #%d: %s", c.ID, c.IssueID, err)
	}
}

// HashTag returns unique hash tag for comment.
func (c *Comment) HashTag() string {
	return "issuecomment-" + com.ToStr(c.ID)
}

// EventTag returns unique event hash tag for comment.
func (c *Comment) EventTag() string {
	return "event-" + com.ToStr(c.ID)
}

func createComment(e *xorm.Session, opts *CreateCommentOptions) (_ *Comment, err error) {
	comment := &Comment{
		Type:      opts.Type,
		PosterID:  opts.Doer.Id,
		IssueID:   opts.Issue.ID,
		CommitID:  opts.CommitID,
		CommitSHA: opts.CommitSHA,
		Line:      opts.LineNum,
		Content:   opts.Content,
	}
	if _, err = e.Insert(comment); err != nil {
		return nil, err
	}

	// Compose comment action, could be plain comment, close or reopen issue/pull request.
	// This object will be used to notify watchers in the end of function.
	act := &Action{
		ActUserID:    opts.Doer.Id,
		ActUserName:  opts.Doer.Name,
		ActEmail:     opts.Doer.Email,
		Content:      fmt.Sprintf("%d|%s", opts.Issue.Index, strings.Split(opts.Content, "\n")[0]),
		RepoID:       opts.Repo.ID,
		RepoUserName: opts.Repo.Owner.Name,
		RepoName:     opts.Repo.Name,
		IsPrivate:    opts.Repo.IsPrivate,
	}

	// Check comment type.
	switch opts.Type {
	case COMMENT_TYPE_COMMENT:
		act.OpType = ACTION_COMMENT_ISSUE

		if _, err = e.Exec("UPDATE `issue` SET num_comments=num_comments+1 WHERE id=?", opts.Issue.ID); err != nil {
			return nil, err
		}

		// Check attachments
		attachments := make([]*Attachment, 0, len(opts.Attachments))
		for _, uuid := range opts.Attachments {
			attach, err := getAttachmentByUUID(e, uuid)
			if err != nil {
				if IsErrAttachmentNotExist(err) {
					continue
				}
				return nil, fmt.Errorf("getAttachmentByUUID[%s]: %v", uuid, err)
			}
			attachments = append(attachments, attach)
		}

		for i := range attachments {
			attachments[i].IssueID = opts.Issue.ID
			attachments[i].CommentID = comment.ID
			// No assign value could be 0, so ignore AllCols().
			if _, err = e.Id(attachments[i].ID).Update(attachments[i]); err != nil {
				return nil, fmt.Errorf("update attachment[%d]: %v", attachments[i].ID, err)
			}
		}

	case COMMENT_TYPE_REOPEN:
		act.OpType = ACTION_REOPEN_ISSUE
		if opts.Issue.IsPull {
			act.OpType = ACTION_REOPEN_PULL_REQUEST
		}

		if opts.Issue.IsPull {
			_, err = e.Exec("UPDATE `repository` SET num_closed_pulls=num_closed_pulls-1 WHERE id=?", opts.Repo.ID)
		} else {
			_, err = e.Exec("UPDATE `repository` SET num_closed_issues=num_closed_issues-1 WHERE id=?", opts.Repo.ID)
		}
		if err != nil {
			return nil, err
		}

	case COMMENT_TYPE_CLOSE:
		act.OpType = ACTION_CLOSE_ISSUE
		if opts.Issue.IsPull {
			act.OpType = ACTION_CLOSE_PULL_REQUEST
		}

		if opts.Issue.IsPull {
			_, err = e.Exec("UPDATE `repository` SET num_closed_pulls=num_closed_pulls+1 WHERE id=?", opts.Repo.ID)
		} else {
			_, err = e.Exec("UPDATE `repository` SET num_closed_issues=num_closed_issues+1 WHERE id=?", opts.Repo.ID)
		}
		if err != nil {
			return nil, err
		}
	}

	// Notify watchers for whatever action comes in, ignore if no action type
	if act.OpType > 0 {
		if err = notifyWatchers(e, act); err != nil {
			return nil, fmt.Errorf("notifyWatchers: %v", err)
		}
	}

	return comment, nil
}

func createStatusComment(e *xorm.Session, doer *User, repo *Repository, issue *Issue) (*Comment, error) {
	cmtType := COMMENT_TYPE_CLOSE
	if !issue.IsClosed {
		cmtType = COMMENT_TYPE_REOPEN
	}
	return createComment(e, &CreateCommentOptions{
		Type:  cmtType,
		Doer:  doer,
		Repo:  repo,
		Issue: issue,
	})
}

type CreateCommentOptions struct {
	Type  CommentType
	Doer  *User
	Repo  *Repository
	Issue *Issue

	CommitID    int64
	CommitSHA   string
	LineNum     int64
	Content     string
	Attachments []string // UUIDs of attachments
}

// CreateComment creates comment of issue or commit.
func CreateComment(opts *CreateCommentOptions) (comment *Comment, err error) {
	sess := x.NewSession()
	defer sessionRelease(sess)
	if err = sess.Begin(); err != nil {
		return nil, err
	}

	comment, err = createComment(sess, opts)
	if err != nil {
		return nil, err
	}

	return comment, sess.Commit()
}

// CreateIssueComment creates a plain issue comment.
func CreateIssueComment(doer *User, repo *Repository, issue *Issue, content string, attachments []string) (*Comment, error) {
	return CreateComment(&CreateCommentOptions{
		Type:        COMMENT_TYPE_COMMENT,
		Doer:        doer,
		Repo:        repo,
		Issue:       issue,
		Content:     content,
		Attachments: attachments,
	})
}

// CreateRefComment creates a commit reference comment to issue.
func CreateRefComment(doer *User, repo *Repository, issue *Issue, content, commitSHA string) error {
	if len(commitSHA) == 0 {
		return fmt.Errorf("cannot create reference with empty commit SHA")
	}

	// Check if same reference from same commit has already existed.
	has, err := x.Get(&Comment{
		Type:      COMMENT_TYPE_COMMIT_REF,
		IssueID:   issue.ID,
		CommitSHA: commitSHA,
	})
	if err != nil {
		return fmt.Errorf("check reference comment: %v", err)
	} else if has {
		return nil
	}

	_, err = CreateComment(&CreateCommentOptions{
		Type:      COMMENT_TYPE_COMMIT_REF,
		Doer:      doer,
		Repo:      repo,
		Issue:     issue,
		CommitSHA: commitSHA,
		Content:   content,
	})
	return err
}

// GetCommentByID returns the comment by given ID.
func GetCommentByID(id int64) (*Comment, error) {
	c := new(Comment)
	has, err := x.Id(id).Get(c)
	if err != nil {
		return nil, err
	} else if !has {
		return nil, ErrCommentNotExist{id}
	}
	return c, nil
}

// GetCommentsByIssueID returns all comments of issue by given ID.
func GetCommentsByIssueID(issueID int64) ([]*Comment, error) {
	comments := make([]*Comment, 0, 10)
	return comments, x.Where("issue_id=?", issueID).Asc("created_unix").Find(&comments)
}

// UpdateComment updates information of comment.
func UpdateComment(c *Comment) error {
	_, err := x.Id(c.ID).AllCols().Update(c)
	return err
}
