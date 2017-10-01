// Copyright 2016 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package repo

import (
	"fmt"
	"strings"

	api "github.com/gogits/go-gogs-client"

	"github.com/gogits/gogs/models"
	"github.com/gogits/gogs/modules/context"
	"github.com/gogits/gogs/modules/setting"
	"github.com/gogits/gogs/routers/api/v1/convert"
	"github.com/gogits/gogs/routers/repo"
)

func ListIssues(ctx *context.APIContext) {
	issues, err := models.Issues(&models.IssuesOptions{
		RepoID: ctx.Repo.Repository.ID,
		Page:   ctx.QueryInt("page"),
	})
	if err != nil {
		ctx.Error(500, "Issues", err)
		return
	}

	apiIssues := make([]*api.Issue, len(issues))
	for i := range issues {
		apiIssues[i] = convert.ToIssue(issues[i])
	}

	ctx.SetLinkHeader(ctx.Repo.Repository.NumIssues, setting.IssuePagingNum)
	ctx.JSON(200, &apiIssues)
}

func GetIssue(ctx *context.APIContext) {
	issue, err := models.GetIssueByIndex(ctx.Repo.Repository.ID, ctx.ParamsInt64(":index"))
	if err != nil {
		if models.IsErrIssueNotExist(err) {
			ctx.Status(404)
		} else {
			ctx.Error(500, "GetIssueByIndex", err)
		}
		return
	}

	ctx.JSON(200, convert.ToIssue(issue))
}

func CreateIssue(ctx *context.APIContext, form api.CreateIssueOption) {
	issue := &models.Issue{
		RepoID:   ctx.Repo.Repository.ID,
		Name:     form.Title,
		PosterID: ctx.User.Id,
		Poster:   ctx.User,
		Content:  form.Body,
	}

	if ctx.Repo.IsWriter() {
		if len(form.Assignee) > 0 {
			assignee, err := models.GetUserByName(form.Assignee)
			if err != nil {
				if models.IsErrUserNotExist(err) {
					ctx.Error(422, "", fmt.Sprintf("Assignee does not exist: [name: %s]", form.Assignee))
				} else {
					ctx.Error(500, "GetUserByName", err)
				}
				return
			}
			issue.AssigneeID = assignee.Id
		}
		issue.MilestoneID = form.Milestone
	} else {
		form.Labels = nil
	}

	if err := models.NewIssue(ctx.Repo.Repository, issue, form.Labels, nil); err != nil {
		ctx.Error(500, "NewIssue", err)
		return
	} else if err := repo.MailWatchersAndMentions(ctx.Context, issue); err != nil {
		ctx.Error(500, "MailWatchersAndMentions", err)
		return
	}

	// Refetch from database to assign some automatic values
	var err error
	issue, err = models.GetIssueByID(issue.ID)
	if err != nil {
		ctx.Error(500, "GetIssueByID", err)
		return
	}
	ctx.JSON(201, convert.ToIssue(issue))
}

func EditIssue(ctx *context.APIContext, form api.EditIssueOption) {
	issue, err := models.GetIssueByIndex(ctx.Repo.Repository.ID, ctx.ParamsInt64(":index"))
	if err != nil {
		if models.IsErrIssueNotExist(err) {
			ctx.Status(404)
		} else {
			ctx.Error(500, "GetIssueByIndex", err)
		}
		return
	}

	if !issue.IsPoster(ctx.User.Id) && !ctx.Repo.IsWriter() {
		ctx.Status(403)
		return
	}

	if len(form.Title) > 0 {
		issue.Name = form.Title
	}
	if form.Body != nil {
		issue.Content = *form.Body
	}

	if ctx.Repo.IsWriter() && form.Assignee != nil &&
		(issue.Assignee == nil || issue.Assignee.LowerName != strings.ToLower(*form.Assignee)) {
		if len(*form.Assignee) == 0 {
			issue.AssigneeID = 0
		} else {
			assignee, err := models.GetUserByName(*form.Assignee)
			if err != nil {
				if models.IsErrUserNotExist(err) {
					ctx.Error(422, "", fmt.Sprintf("Assignee does not exist: [name: %s]", *form.Assignee))
				} else {
					ctx.Error(500, "GetUserByName", err)
				}
				return
			}
			issue.AssigneeID = assignee.Id
		}

		if err = models.UpdateIssueUserByAssignee(issue); err != nil {
			ctx.Error(500, "UpdateIssueUserByAssignee", err)
			return
		}
	}
	if ctx.Repo.IsWriter() && form.Milestone != nil &&
		issue.MilestoneID != *form.Milestone {
		oldMid := issue.MilestoneID
		issue.MilestoneID = *form.Milestone
		if err = models.ChangeMilestoneAssign(oldMid, issue); err != nil {
			ctx.Error(500, "ChangeMilestoneAssign", err)
			return
		}
	}

	if err = models.UpdateIssue(issue); err != nil {
		ctx.Error(500, "UpdateIssue", err)
		return
	}

	// Refetch from database to assign some automatic values
	issue, err = models.GetIssueByID(issue.ID)
	if err != nil {
		ctx.Error(500, "GetIssueByID", err)
		return
	}
	ctx.JSON(201, convert.ToIssue(issue))
}
