// Copyright 2014 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package repo

import (
	"strings"
	"time"

	"github.com/gogits/git-module"

	"github.com/gogits/gogs/models"
	"github.com/gogits/gogs/modules/auth"
	"github.com/gogits/gogs/modules/base"
	"github.com/gogits/gogs/modules/context"
	"github.com/gogits/gogs/modules/log"
	"github.com/gogits/gogs/modules/mailer"
	"github.com/gogits/gogs/modules/setting"
)

const (
	SETTINGS_OPTIONS base.TplName = "repo/settings/options"
	COLLABORATION    base.TplName = "repo/settings/collaboration"
	GITHOOKS         base.TplName = "repo/settings/githooks"
	GITHOOK_EDIT     base.TplName = "repo/settings/githook_edit"
	DEPLOY_KEYS      base.TplName = "repo/settings/deploy_keys"
)

func Settings(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("repo.settings")
	ctx.Data["PageIsSettingsOptions"] = true
	ctx.HTML(200, SETTINGS_OPTIONS)
}

func SettingsPost(ctx *context.Context, form auth.RepoSettingForm) {
	ctx.Data["Title"] = ctx.Tr("repo.settings")
	ctx.Data["PageIsSettingsOptions"] = true

	repo := ctx.Repo.Repository

	switch ctx.Query("action") {
	case "update":
		if ctx.HasError() {
			ctx.HTML(200, SETTINGS_OPTIONS)
			return
		}

		isNameChanged := false
		oldRepoName := repo.Name
		newRepoName := form.RepoName
		// Check if repository name has been changed.
		if repo.LowerName != strings.ToLower(newRepoName) {
			isNameChanged = true
			if err := models.ChangeRepositoryName(ctx.Repo.Owner, repo.Name, newRepoName); err != nil {
				ctx.Data["Err_RepoName"] = true
				switch {
				case models.IsErrRepoAlreadyExist(err):
					ctx.RenderWithErr(ctx.Tr("form.repo_name_been_taken"), SETTINGS_OPTIONS, &form)
				case models.IsErrNameReserved(err):
					ctx.RenderWithErr(ctx.Tr("repo.form.name_reserved", err.(models.ErrNameReserved).Name), SETTINGS_OPTIONS, &form)
				case models.IsErrNamePatternNotAllowed(err):
					ctx.RenderWithErr(ctx.Tr("repo.form.name_pattern_not_allowed", err.(models.ErrNamePatternNotAllowed).Pattern), SETTINGS_OPTIONS, &form)
				default:
					ctx.Handle(500, "ChangeRepositoryName", err)
				}
				return
			}

			log.Trace("Repository name changed: %s/%s -> %s", ctx.Repo.Owner.Name, repo.Name, newRepoName)
		}
		// In case it's just a case change.
		repo.Name = newRepoName
		repo.LowerName = strings.ToLower(newRepoName)

		if ctx.Repo.GitRepo.IsBranchExist(form.Branch) &&
			repo.DefaultBranch != form.Branch {
			repo.DefaultBranch = form.Branch
			if err := ctx.Repo.GitRepo.SetDefaultBranch(form.Branch); err != nil {
				if !git.IsErrUnsupportedVersion(err) {
					ctx.Handle(500, "SetDefaultBranch", err)
					return
				}
			}
		}
		repo.Description = form.Description
		repo.Website = form.Website

		// Visibility of forked repository is forced sync with base repository.
		if repo.IsFork {
			form.Private = repo.BaseRepo.IsPrivate
		}

		visibilityChanged := repo.IsPrivate != form.Private
		repo.IsPrivate = form.Private
		if err := models.UpdateRepository(repo, visibilityChanged); err != nil {
			ctx.Handle(500, "UpdateRepository", err)
			return
		}
		log.Trace("Repository basic settings updated: %s/%s", ctx.Repo.Owner.Name, repo.Name)

		if isNameChanged {
			if err := models.RenameRepoAction(ctx.User, oldRepoName, repo); err != nil {
				log.Error(4, "RenameRepoAction: %v", err)
			}
		}

		if repo.IsMirror {
			if form.Interval > 0 {
				ctx.Repo.Mirror.Interval = form.Interval
				ctx.Repo.Mirror.NextUpdate = time.Now().Add(time.Duration(form.Interval) * time.Hour)
				if err := models.UpdateMirror(ctx.Repo.Mirror); err != nil {
					ctx.Handle(500, "UpdateMirror", err)
					return
				}
			}
			if err := ctx.Repo.Mirror.SaveAddress(form.MirrorAddress); err != nil {
				ctx.Handle(500, "SaveAddress", err)
				return
			}
		}

		ctx.Flash.Success(ctx.Tr("repo.settings.update_settings_success"))
		ctx.Redirect(repo.RepoLink() + "/settings")

	case "advanced":
		repo.EnableWiki = form.EnableWiki
		repo.EnableExternalWiki = form.EnableExternalWiki
		repo.ExternalWikiURL = form.ExternalWikiURL
		repo.EnableIssues = form.EnableIssues
		repo.EnableExternalTracker = form.EnableExternalTracker
		repo.ExternalTrackerFormat = form.TrackerURLFormat
		repo.EnablePulls = form.EnablePulls

		if err := models.UpdateRepository(repo, false); err != nil {
			ctx.Handle(500, "UpdateRepository", err)
			return
		}
		log.Trace("Repository advanced settings updated: %s/%s", ctx.Repo.Owner.Name, repo.Name)

		ctx.Flash.Success(ctx.Tr("repo.settings.update_settings_success"))
		ctx.Redirect(ctx.Repo.RepoLink + "/settings")

	case "convert":
		if !ctx.Repo.IsOwner() {
			ctx.Error(404)
			return
		}
		if repo.Name != form.RepoName {
			ctx.RenderWithErr(ctx.Tr("form.enterred_invalid_repo_name"), SETTINGS_OPTIONS, nil)
			return
		}

		if ctx.Repo.Owner.IsOrganization() {
			if !ctx.Repo.Owner.IsOwnedBy(ctx.User.Id) {
				ctx.Error(404)
				return
			}
		}

		if !repo.IsMirror {
			ctx.Error(404)
			return
		}
		repo.IsMirror = false

		if _, err := models.CleanUpMigrateInfo(repo, models.RepoPath(ctx.Repo.Owner.Name, repo.Name)); err != nil {
			ctx.Handle(500, "CleanUpMigrateInfo", err)
			return
		} else if err = models.DeleteMirrorByRepoID(ctx.Repo.Repository.ID); err != nil {
			ctx.Handle(500, "DeleteMirrorByRepoID", err)
			return
		}
		log.Trace("Repository converted from mirror to regular: %s/%s", ctx.Repo.Owner.Name, repo.Name)
		ctx.Flash.Success(ctx.Tr("repo.settings.convert_succeed"))
		ctx.Redirect(setting.AppSubUrl + "/" + ctx.Repo.Owner.Name + "/" + repo.Name)

	case "transfer":
		if !ctx.Repo.IsOwner() {
			ctx.Error(404)
			return
		}
		if repo.Name != form.RepoName {
			ctx.RenderWithErr(ctx.Tr("form.enterred_invalid_repo_name"), SETTINGS_OPTIONS, nil)
			return
		}

		if ctx.Repo.Owner.IsOrganization() {
			if !ctx.Repo.Owner.IsOwnedBy(ctx.User.Id) {
				ctx.Error(404)
				return
			}
		}

		newOwner := ctx.Query("new_owner_name")
		isExist, err := models.IsUserExist(0, newOwner)
		if err != nil {
			ctx.Handle(500, "IsUserExist", err)
			return
		} else if !isExist {
			ctx.RenderWithErr(ctx.Tr("form.enterred_invalid_owner_name"), SETTINGS_OPTIONS, nil)
			return
		}

		if err = models.TransferOwnership(ctx.User, newOwner, repo); err != nil {
			if models.IsErrRepoAlreadyExist(err) {
				ctx.RenderWithErr(ctx.Tr("repo.settings.new_owner_has_same_repo"), SETTINGS_OPTIONS, nil)
			} else {
				ctx.Handle(500, "TransferOwnership", err)
			}
			return
		}
		log.Trace("Repository transfered: %s/%s -> %s", ctx.Repo.Owner.Name, repo.Name, newOwner)
		ctx.Flash.Success(ctx.Tr("repo.settings.transfer_succeed"))
		ctx.Redirect(setting.AppSubUrl + "/" + newOwner + "/" + repo.Name)

	case "delete":
		if !ctx.Repo.IsOwner() {
			ctx.Error(404)
			return
		}
		if repo.Name != form.RepoName {
			ctx.RenderWithErr(ctx.Tr("form.enterred_invalid_repo_name"), SETTINGS_OPTIONS, nil)
			return
		}

		if ctx.Repo.Owner.IsOrganization() {
			if !ctx.Repo.Owner.IsOwnedBy(ctx.User.Id) {
				ctx.Error(404)
				return
			}
		}

		if err := models.DeleteRepository(ctx.Repo.Owner.Id, repo.ID); err != nil {
			ctx.Handle(500, "DeleteRepository", err)
			return
		}
		log.Trace("Repository deleted: %s/%s", ctx.Repo.Owner.Name, repo.Name)

		ctx.Flash.Success(ctx.Tr("repo.settings.deletion_success"))
		ctx.Redirect(ctx.Repo.Owner.DashboardLink())

	case "delete-wiki":
		if !ctx.Repo.IsOwner() {
			ctx.Error(404)
			return
		}
		if repo.Name != form.RepoName {
			ctx.RenderWithErr(ctx.Tr("form.enterred_invalid_repo_name"), SETTINGS_OPTIONS, nil)
			return
		}

		if ctx.Repo.Owner.IsOrganization() {
			if !ctx.Repo.Owner.IsOwnedBy(ctx.User.Id) {
				ctx.Error(404)
				return
			}
		}

		repo.DeleteWiki()
		log.Trace("Repository wiki deleted: %s/%s", ctx.Repo.Owner.Name, repo.Name)

		repo.EnableWiki = false
		if err := models.UpdateRepository(repo, false); err != nil {
			ctx.Handle(500, "UpdateRepository", err)
			return
		}

		ctx.Flash.Success(ctx.Tr("repo.settings.wiki_deletion_success"))
		ctx.Redirect(ctx.Repo.RepoLink + "/settings")
	}
}

func Collaboration(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("repo.settings")
	ctx.Data["PageIsSettingsCollaboration"] = true

	users, err := ctx.Repo.Repository.GetCollaborators()
	if err != nil {
		ctx.Handle(500, "GetCollaborators", err)
		return
	}
	ctx.Data["Collaborators"] = users

	ctx.HTML(200, COLLABORATION)
}

func CollaborationPost(ctx *context.Context) {
	name := strings.ToLower(ctx.Query("collaborator"))
	if len(name) == 0 || ctx.Repo.Owner.LowerName == name {
		ctx.Redirect(setting.AppSubUrl + ctx.Req.URL.Path)
		return
	}

	u, err := models.GetUserByName(name)
	if err != nil {
		if models.IsErrUserNotExist(err) {
			ctx.Flash.Error(ctx.Tr("form.user_not_exist"))
			ctx.Redirect(setting.AppSubUrl + ctx.Req.URL.Path)
		} else {
			ctx.Handle(500, "GetUserByName", err)
		}
		return
	}

	// Organization is not allowed to be added as a collaborator.
	if u.IsOrganization() {
		ctx.Flash.Error(ctx.Tr("repo.settings.org_not_allowed_to_be_collaborator"))
		ctx.Redirect(setting.AppSubUrl + ctx.Req.URL.Path)
		return
	}

	// Check if user is organization member.
	if ctx.Repo.Owner.IsOrganization() && ctx.Repo.Owner.IsOrgMember(u.Id) {
		ctx.Flash.Info(ctx.Tr("repo.settings.user_is_org_member"))
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/collaboration")
		return
	}

	if err = ctx.Repo.Repository.AddCollaborator(u); err != nil {
		ctx.Handle(500, "AddCollaborator", err)
		return
	}

	if setting.Service.EnableNotifyMail {
		if err = mailer.SendCollaboratorMail(ctx.Render, u, ctx.User, ctx.Repo.Repository); err != nil {
			ctx.Handle(500, "SendCollaboratorMail", err)
			return
		}
	}

	ctx.Flash.Success(ctx.Tr("repo.settings.add_collaborator_success"))
	ctx.Redirect(setting.AppSubUrl + ctx.Req.URL.Path)
}

func ChangeCollaborationAccessMode(ctx *context.Context) {
	if err := ctx.Repo.Repository.ChangeCollaborationAccessMode(
		ctx.QueryInt64("uid"),
		models.AccessMode(ctx.QueryInt("mode"))); err != nil {
		log.Error(4, "ChangeCollaborationAccessMode: %v", err)
	}
}

func DeleteCollaboration(ctx *context.Context) {
	if err := ctx.Repo.Repository.DeleteCollaboration(ctx.QueryInt64("id")); err != nil {
		ctx.Flash.Error("DeleteCollaboration: " + err.Error())
	} else {
		ctx.Flash.Success(ctx.Tr("repo.settings.remove_collaborator_success"))
	}

	ctx.JSON(200, map[string]interface{}{
		"redirect": ctx.Repo.RepoLink + "/settings/collaboration",
	})
}

func parseOwnerAndRepo(ctx *context.Context) (*models.User, *models.Repository) {
	owner, err := models.GetUserByName(ctx.Params(":username"))
	if err != nil {
		if models.IsErrUserNotExist(err) {
			ctx.Handle(404, "GetUserByName", err)
		} else {
			ctx.Handle(500, "GetUserByName", err)
		}
		return nil, nil
	}

	repo, err := models.GetRepositoryByName(owner.Id, ctx.Params(":reponame"))
	if err != nil {
		if models.IsErrRepoNotExist(err) {
			ctx.Handle(404, "GetRepositoryByName", err)
		} else {
			ctx.Handle(500, "GetRepositoryByName", err)
		}
		return nil, nil
	}

	return owner, repo
}

func GitHooks(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("repo.settings.githooks")
	ctx.Data["PageIsSettingsGitHooks"] = true

	hooks, err := ctx.Repo.GitRepo.Hooks()
	if err != nil {
		ctx.Handle(500, "Hooks", err)
		return
	}
	ctx.Data["Hooks"] = hooks

	ctx.HTML(200, GITHOOKS)
}

func GitHooksEdit(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("repo.settings.githooks")
	ctx.Data["PageIsSettingsGitHooks"] = true

	name := ctx.Params(":name")
	hook, err := ctx.Repo.GitRepo.GetHook(name)
	if err != nil {
		if err == git.ErrNotValidHook {
			ctx.Handle(404, "GetHook", err)
		} else {
			ctx.Handle(500, "GetHook", err)
		}
		return
	}
	ctx.Data["Hook"] = hook
	ctx.HTML(200, GITHOOK_EDIT)
}

func GitHooksEditPost(ctx *context.Context) {
	name := ctx.Params(":name")
	hook, err := ctx.Repo.GitRepo.GetHook(name)
	if err != nil {
		if err == git.ErrNotValidHook {
			ctx.Handle(404, "GetHook", err)
		} else {
			ctx.Handle(500, "GetHook", err)
		}
		return
	}
	hook.Content = ctx.Query("content")
	if err = hook.Update(); err != nil {
		ctx.Handle(500, "hook.Update", err)
		return
	}
	ctx.Redirect(ctx.Repo.RepoLink + "/settings/hooks/git")
}

func DeployKeys(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("repo.settings.deploy_keys")
	ctx.Data["PageIsSettingsKeys"] = true

	keys, err := models.ListDeployKeys(ctx.Repo.Repository.ID)
	if err != nil {
		ctx.Handle(500, "ListDeployKeys", err)
		return
	}
	ctx.Data["Deploykeys"] = keys

	ctx.HTML(200, DEPLOY_KEYS)
}

func DeployKeysPost(ctx *context.Context, form auth.AddSSHKeyForm) {
	ctx.Data["Title"] = ctx.Tr("repo.settings.deploy_keys")
	ctx.Data["PageIsSettingsKeys"] = true

	keys, err := models.ListDeployKeys(ctx.Repo.Repository.ID)
	if err != nil {
		ctx.Handle(500, "ListDeployKeys", err)
		return
	}
	ctx.Data["Deploykeys"] = keys

	if ctx.HasError() {
		ctx.HTML(200, DEPLOY_KEYS)
		return
	}

	content, err := models.CheckPublicKeyString(form.Content)
	if err != nil {
		if models.IsErrKeyUnableVerify(err) {
			ctx.Flash.Info(ctx.Tr("form.unable_verify_ssh_key"))
		} else {
			ctx.Data["HasError"] = true
			ctx.Data["Err_Content"] = true
			ctx.Flash.Error(ctx.Tr("form.invalid_ssh_key", err.Error()))
			ctx.Redirect(ctx.Repo.RepoLink + "/settings/keys")
			return
		}
	}

	key, err := models.AddDeployKey(ctx.Repo.Repository.ID, form.Title, content)
	if err != nil {
		ctx.Data["HasError"] = true
		switch {
		case models.IsErrKeyAlreadyExist(err):
			ctx.Data["Err_Content"] = true
			ctx.RenderWithErr(ctx.Tr("repo.settings.key_been_used"), DEPLOY_KEYS, &form)
		case models.IsErrKeyNameAlreadyUsed(err):
			ctx.Data["Err_Title"] = true
			ctx.RenderWithErr(ctx.Tr("repo.settings.key_name_used"), DEPLOY_KEYS, &form)
		default:
			ctx.Handle(500, "AddDeployKey", err)
		}
		return
	}

	log.Trace("Deploy key added: %d", ctx.Repo.Repository.ID)
	ctx.Flash.Success(ctx.Tr("repo.settings.add_key_success", key.Name))
	ctx.Redirect(ctx.Repo.RepoLink + "/settings/keys")
}

func DeleteDeployKey(ctx *context.Context) {
	if err := models.DeleteDeployKey(ctx.User, ctx.QueryInt64("id")); err != nil {
		ctx.Flash.Error("DeleteDeployKey: " + err.Error())
	} else {
		ctx.Flash.Success(ctx.Tr("repo.settings.deploy_key_deletion_success"))
	}

	ctx.JSON(200, map[string]interface{}{
		"redirect": ctx.Repo.RepoLink + "/settings/keys",
	})
}
