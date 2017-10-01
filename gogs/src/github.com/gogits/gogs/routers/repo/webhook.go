// Copyright 2015 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package repo

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Unknwon/com"

	git "github.com/gogits/git-module"
	api "github.com/gogits/go-gogs-client"

	"github.com/gogits/gogs/models"
	"github.com/gogits/gogs/modules/auth"
	"github.com/gogits/gogs/modules/base"
	"github.com/gogits/gogs/modules/context"
	"github.com/gogits/gogs/modules/setting"
)

const (
	HOOKS        base.TplName = "repo/settings/hooks"
	HOOK_NEW     base.TplName = "repo/settings/hook_new"
	ORG_HOOK_NEW base.TplName = "org/settings/hook_new"
)

func Webhooks(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("repo.settings.hooks")
	ctx.Data["PageIsSettingsHooks"] = true
	ctx.Data["BaseLink"] = ctx.Repo.RepoLink
	ctx.Data["Description"] = ctx.Tr("repo.settings.hooks_desc", "https://github.com/gogits/go-gogs-client/wiki/Repositories-Webhooks")

	ws, err := models.GetWebhooksByRepoID(ctx.Repo.Repository.ID)
	if err != nil {
		ctx.Handle(500, "GetWebhooksByRepoID", err)
		return
	}
	ctx.Data["Webhooks"] = ws

	ctx.HTML(200, HOOKS)
}

type OrgRepoCtx struct {
	OrgID       int64
	RepoID      int64
	Link        string
	NewTemplate base.TplName
}

// getOrgRepoCtx determines whether this is a repo context or organization context.
func getOrgRepoCtx(ctx *context.Context) (*OrgRepoCtx, error) {
	if len(ctx.Repo.RepoLink) > 0 {
		return &OrgRepoCtx{
			RepoID:      ctx.Repo.Repository.ID,
			Link:        ctx.Repo.RepoLink,
			NewTemplate: HOOK_NEW,
		}, nil
	}

	if len(ctx.Org.OrgLink) > 0 {
		return &OrgRepoCtx{
			OrgID:       ctx.Org.Organization.Id,
			Link:        ctx.Org.OrgLink,
			NewTemplate: ORG_HOOK_NEW,
		}, nil
	}

	return nil, errors.New("Unable to set OrgRepo context")
}

func checkHookType(ctx *context.Context) string {
	hookType := strings.ToLower(ctx.Params(":type"))
	if !com.IsSliceContainsStr(setting.Webhook.Types, hookType) {
		ctx.Handle(404, "checkHookType", nil)
		return ""
	}
	return hookType
}

func WebhooksNew(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("repo.settings.add_webhook")
	ctx.Data["PageIsSettingsHooks"] = true
	ctx.Data["PageIsSettingsHooksNew"] = true
	ctx.Data["Webhook"] = models.Webhook{HookEvent: &models.HookEvent{}}

	orCtx, err := getOrgRepoCtx(ctx)
	if err != nil {
		ctx.Handle(500, "getOrgRepoCtx", err)
		return
	}

	ctx.Data["HookType"] = checkHookType(ctx)
	if ctx.Written() {
		return
	}
	ctx.Data["BaseLink"] = orCtx.Link

	ctx.HTML(200, orCtx.NewTemplate)
}

func ParseHookEvent(form auth.WebhookForm) *models.HookEvent {
	return &models.HookEvent{
		PushOnly:       form.PushOnly(),
		SendEverything: form.SendEverything(),
		ChooseEvents:   form.ChooseEvents(),
		HookEvents: models.HookEvents{
			Create: form.Create,
			Push:   form.Push,
		},
	}
}

func WebHooksNewPost(ctx *context.Context, form auth.NewWebhookForm) {
	ctx.Data["Title"] = ctx.Tr("repo.settings.add_webhook")
	ctx.Data["PageIsSettingsHooks"] = true
	ctx.Data["PageIsSettingsHooksNew"] = true
	ctx.Data["Webhook"] = models.Webhook{HookEvent: &models.HookEvent{}}
	ctx.Data["HookType"] = "gogs"

	orCtx, err := getOrgRepoCtx(ctx)
	if err != nil {
		ctx.Handle(500, "getOrgRepoCtx", err)
		return
	}
	ctx.Data["BaseLink"] = orCtx.Link

	if ctx.HasError() {
		ctx.HTML(200, orCtx.NewTemplate)
		return
	}

	contentType := models.JSON
	if models.HookContentType(form.ContentType) == models.FORM {
		contentType = models.FORM
	}

	w := &models.Webhook{
		RepoID:       orCtx.RepoID,
		URL:          form.PayloadURL,
		ContentType:  contentType,
		Secret:       form.Secret,
		HookEvent:    ParseHookEvent(form.WebhookForm),
		IsActive:     form.Active,
		HookTaskType: models.GOGS,
		OrgID:        orCtx.OrgID,
	}
	if err := w.UpdateEvent(); err != nil {
		ctx.Handle(500, "UpdateEvent", err)
		return
	} else if err := models.CreateWebhook(w); err != nil {
		ctx.Handle(500, "CreateWebhook", err)
		return
	}

	ctx.Flash.Success(ctx.Tr("repo.settings.add_hook_success"))
	ctx.Redirect(orCtx.Link + "/settings/hooks")
}

func SlackHooksNewPost(ctx *context.Context, form auth.NewSlackHookForm) {
	ctx.Data["Title"] = ctx.Tr("repo.settings")
	ctx.Data["PageIsSettingsHooks"] = true
	ctx.Data["PageIsSettingsHooksNew"] = true
	ctx.Data["Webhook"] = models.Webhook{HookEvent: &models.HookEvent{}}

	orCtx, err := getOrgRepoCtx(ctx)
	if err != nil {
		ctx.Handle(500, "getOrgRepoCtx", err)
		return
	}

	if ctx.HasError() {
		ctx.HTML(200, orCtx.NewTemplate)
		return
	}

	meta, err := json.Marshal(&models.SlackMeta{
		Channel:  form.Channel,
		Username: form.Username,
		IconURL:  form.IconURL,
		Color:    form.Color,
	})
	if err != nil {
		ctx.Handle(500, "Marshal", err)
		return
	}

	w := &models.Webhook{
		RepoID:       orCtx.RepoID,
		URL:          form.PayloadURL,
		ContentType:  models.JSON,
		HookEvent:    ParseHookEvent(form.WebhookForm),
		IsActive:     form.Active,
		HookTaskType: models.SLACK,
		Meta:         string(meta),
		OrgID:        orCtx.OrgID,
	}
	if err := w.UpdateEvent(); err != nil {
		ctx.Handle(500, "UpdateEvent", err)
		return
	} else if err := models.CreateWebhook(w); err != nil {
		ctx.Handle(500, "CreateWebhook", err)
		return
	}

	ctx.Flash.Success(ctx.Tr("repo.settings.add_hook_success"))
	ctx.Redirect(orCtx.Link + "/settings/hooks")
}

func checkWebhook(ctx *context.Context) (*OrgRepoCtx, *models.Webhook) {
	ctx.Data["RequireHighlightJS"] = true

	orCtx, err := getOrgRepoCtx(ctx)
	if err != nil {
		ctx.Handle(500, "getOrgRepoCtx", err)
		return nil, nil
	}
	ctx.Data["BaseLink"] = orCtx.Link

	w, err := models.GetWebhookByID(ctx.ParamsInt64(":id"))
	if err != nil {
		if models.IsErrWebhookNotExist(err) {
			ctx.Handle(404, "GetWebhookByID", nil)
		} else {
			ctx.Handle(500, "GetWebhookByID", err)
		}
		return nil, nil
	}

	switch w.HookTaskType {
	case models.SLACK:
		ctx.Data["SlackHook"] = w.GetSlackHook()
		ctx.Data["HookType"] = "slack"
	default:
		ctx.Data["HookType"] = "gogs"
	}

	ctx.Data["History"], err = w.History(1)
	if err != nil {
		ctx.Handle(500, "History", err)
	}
	return orCtx, w
}

func WebHooksEdit(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("repo.settings.update_webhook")
	ctx.Data["PageIsSettingsHooks"] = true
	ctx.Data["PageIsSettingsHooksEdit"] = true

	orCtx, w := checkWebhook(ctx)
	if ctx.Written() {
		return
	}
	ctx.Data["Webhook"] = w

	ctx.HTML(200, orCtx.NewTemplate)
}

func WebHooksEditPost(ctx *context.Context, form auth.NewWebhookForm) {
	ctx.Data["Title"] = ctx.Tr("repo.settings.update_webhook")
	ctx.Data["PageIsSettingsHooks"] = true
	ctx.Data["PageIsSettingsHooksEdit"] = true

	orCtx, w := checkWebhook(ctx)
	if ctx.Written() {
		return
	}
	ctx.Data["Webhook"] = w

	if ctx.HasError() {
		ctx.HTML(200, orCtx.NewTemplate)
		return
	}

	contentType := models.JSON
	if models.HookContentType(form.ContentType) == models.FORM {
		contentType = models.FORM
	}

	w.URL = form.PayloadURL
	w.ContentType = contentType
	w.Secret = form.Secret
	w.HookEvent = ParseHookEvent(form.WebhookForm)
	w.IsActive = form.Active
	if err := w.UpdateEvent(); err != nil {
		ctx.Handle(500, "UpdateEvent", err)
		return
	} else if err := models.UpdateWebhook(w); err != nil {
		ctx.Handle(500, "WebHooksEditPost", err)
		return
	}

	ctx.Flash.Success(ctx.Tr("repo.settings.update_hook_success"))
	ctx.Redirect(fmt.Sprintf("%s/settings/hooks/%d", orCtx.Link, w.ID))
}

func SlackHooksEditPost(ctx *context.Context, form auth.NewSlackHookForm) {
	ctx.Data["Title"] = ctx.Tr("repo.settings")
	ctx.Data["PageIsSettingsHooks"] = true
	ctx.Data["PageIsSettingsHooksEdit"] = true

	orCtx, w := checkWebhook(ctx)
	if ctx.Written() {
		return
	}
	ctx.Data["Webhook"] = w

	if ctx.HasError() {
		ctx.HTML(200, orCtx.NewTemplate)
		return
	}

	meta, err := json.Marshal(&models.SlackMeta{
		Channel:  form.Channel,
		Username: form.Username,
		IconURL:  form.IconURL,
		Color:    form.Color,
	})
	if err != nil {
		ctx.Handle(500, "Marshal", err)
		return
	}

	w.URL = form.PayloadURL
	w.Meta = string(meta)
	w.HookEvent = ParseHookEvent(form.WebhookForm)
	w.IsActive = form.Active
	if err := w.UpdateEvent(); err != nil {
		ctx.Handle(500, "UpdateEvent", err)
		return
	} else if err := models.UpdateWebhook(w); err != nil {
		ctx.Handle(500, "UpdateWebhook", err)
		return
	}

	ctx.Flash.Success(ctx.Tr("repo.settings.update_hook_success"))
	ctx.Redirect(fmt.Sprintf("%s/settings/hooks/%d", orCtx.Link, w.ID))
}

func TestWebhook(ctx *context.Context) {
	p := &api.PushPayload{
		Ref:    git.BRANCH_PREFIX + ctx.Repo.Repository.DefaultBranch,
		Before: ctx.Repo.CommitID,
		After:  ctx.Repo.CommitID,
		Commits: []*api.PayloadCommit{
			{
				ID:      ctx.Repo.CommitID,
				Message: ctx.Repo.Commit.Message(),
				URL:     ctx.Repo.Repository.FullRepoLink() + "/commit/" + ctx.Repo.CommitID,
				Author: &api.PayloadAuthor{
					Name:  ctx.Repo.Commit.Author.Name,
					Email: ctx.Repo.Commit.Author.Email,
				},
			},
		},
		Repo: ctx.Repo.Repository.ComposePayload(),
		Pusher: &api.PayloadAuthor{
			Name:     ctx.User.Name,
			Email:    ctx.User.Email,
			UserName: ctx.User.Name,
		},
		Sender: &api.PayloadUser{
			UserName:  ctx.User.Name,
			ID:        ctx.User.Id,
			AvatarUrl: ctx.User.AvatarLink(),
		},
	}
	if err := models.PrepareWebhooks(ctx.Repo.Repository, models.HOOK_EVENT_PUSH, p); err != nil {
		ctx.Flash.Error("PrepareWebhooks: " + err.Error())
		ctx.Status(500)
	} else {
		go models.HookQueue.Add(ctx.Repo.Repository.ID)
		ctx.Flash.Info(ctx.Tr("repo.settings.webhook.test_delivery_success"))
		ctx.Status(200)
	}
}

func DeleteWebhook(ctx *context.Context) {
	if err := models.DeleteWebhook(ctx.QueryInt64("id")); err != nil {
		ctx.Flash.Error("DeleteWebhook: " + err.Error())
	} else {
		ctx.Flash.Success(ctx.Tr("repo.settings.webhook_deletion_success"))
	}

	ctx.JSON(200, map[string]interface{}{
		"redirect": ctx.Repo.RepoLink + "/settings/hooks",
	})
}
