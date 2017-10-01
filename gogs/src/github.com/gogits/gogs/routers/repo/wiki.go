// Copyright 2015 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package repo

import (
	"io/ioutil"
	"strings"
	"time"

	"github.com/gogits/git-module"

	"github.com/gogits/gogs/models"
	"github.com/gogits/gogs/modules/auth"
	"github.com/gogits/gogs/modules/base"
	"github.com/gogits/gogs/modules/context"
	"github.com/gogits/gogs/modules/markdown"
)

const (
	WIKI_START base.TplName = "repo/wiki/start"
	WIKI_VIEW  base.TplName = "repo/wiki/view"
	WIKI_NEW   base.TplName = "repo/wiki/new"
	WIKI_PAGES base.TplName = "repo/wiki/pages"
)

func MustEnableWiki(ctx *context.Context) {
	if !ctx.Repo.Repository.EnableWiki {
		ctx.Handle(404, "MustEnableWiki", nil)
		return
	}

	if ctx.Repo.Repository.EnableExternalWiki {
		ctx.Redirect(ctx.Repo.Repository.ExternalWikiURL)
		return
	}
}

type PageMeta struct {
	Name    string
	URL     string
	Updated time.Time
}

func renderWikiPage(ctx *context.Context, isViewPage bool) (*git.Repository, string) {
	wikiRepo, err := git.OpenRepository(ctx.Repo.Repository.WikiPath())
	if err != nil {
		ctx.Handle(500, "OpenRepository", err)
		return nil, ""
	}
	commit, err := wikiRepo.GetBranchCommit("master")
	if err != nil {
		ctx.Handle(500, "GetBranchCommit", err)
		return nil, ""
	}

	// Get page list.
	if isViewPage {
		entries, err := commit.ListEntries()
		if err != nil {
			ctx.Handle(500, "ListEntries", err)
			return nil, ""
		}
		pages := make([]PageMeta, 0, len(entries))
		for i := range entries {
			if entries[i].Type == git.OBJECT_BLOB {
				name := strings.TrimSuffix(entries[i].Name(), ".md")
				pages = append(pages, PageMeta{
					Name: name,
					URL:  models.ToWikiPageURL(name),
				})
			}
		}
		ctx.Data["Pages"] = pages
	}

	pageURL := ctx.Params(":page")
	if len(pageURL) == 0 {
		pageURL = "Home"
	}
	ctx.Data["PageURL"] = pageURL

	pageName := models.ToWikiPageName(pageURL)
	ctx.Data["old_title"] = pageName
	ctx.Data["Title"] = pageName
	ctx.Data["title"] = pageName
	ctx.Data["RequireHighlightJS"] = true

	blob, err := commit.GetBlobByPath(pageName + ".md")
	if err != nil {
		if git.IsErrNotExist(err) {
			ctx.Redirect(ctx.Repo.RepoLink + "/wiki/_pages")
		} else {
			ctx.Handle(500, "GetBlobByPath", err)
		}
		return nil, ""
	}
	r, err := blob.Data()
	if err != nil {
		ctx.Handle(500, "Data", err)
		return nil, ""
	}
	data, err := ioutil.ReadAll(r)
	if err != nil {
		ctx.Handle(500, "ReadAll", err)
		return nil, ""
	}
	if isViewPage {
		ctx.Data["content"] = string(markdown.Render(data, ctx.Repo.RepoLink, ctx.Repo.Repository.ComposeMetas()))
	} else {
		ctx.Data["content"] = string(data)
	}

	return wikiRepo, pageName
}

func Wiki(ctx *context.Context) {
	ctx.Data["PageIsWiki"] = true

	if !ctx.Repo.Repository.HasWiki() {
		ctx.Data["Title"] = ctx.Tr("repo.wiki")
		ctx.HTML(200, WIKI_START)
		return
	}

	wikiRepo, pageName := renderWikiPage(ctx, true)
	if ctx.Written() {
		return
	}

	// Get last change information.
	lastCommit, err := wikiRepo.GetCommitByPath(pageName + ".md")
	if err != nil {
		ctx.Handle(500, "GetCommitByPath", err)
		return
	}
	ctx.Data["Author"] = lastCommit.Author

	ctx.HTML(200, WIKI_VIEW)
}

func WikiPages(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("repo.wiki.pages")
	ctx.Data["PageIsWiki"] = true

	if !ctx.Repo.Repository.HasWiki() {
		ctx.Redirect(ctx.Repo.RepoLink + "/wiki")
		return
	}

	wikiRepo, err := git.OpenRepository(ctx.Repo.Repository.WikiPath())
	if err != nil {
		ctx.Handle(500, "OpenRepository", err)
		return
	}
	commit, err := wikiRepo.GetBranchCommit("master")
	if err != nil {
		ctx.Handle(500, "GetBranchCommit", err)
		return
	}

	entries, err := commit.ListEntries()
	if err != nil {
		ctx.Handle(500, "ListEntries", err)
		return
	}
	pages := make([]PageMeta, 0, len(entries))
	for i := range entries {
		if entries[i].Type == git.OBJECT_BLOB {
			c, err := wikiRepo.GetCommitByPath(entries[i].Name())
			if err != nil {
				ctx.Handle(500, "GetCommit", err)
				return
			}
			name := strings.TrimSuffix(entries[i].Name(), ".md")
			pages = append(pages, PageMeta{
				Name:    name,
				URL:     models.ToWikiPageURL(name),
				Updated: c.Author.When,
			})
		}
	}
	ctx.Data["Pages"] = pages

	ctx.HTML(200, WIKI_PAGES)
}

func NewWiki(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("repo.wiki.new_page")
	ctx.Data["PageIsWiki"] = true
	ctx.Data["RequireSimpleMDE"] = true

	if !ctx.Repo.Repository.HasWiki() {
		ctx.Data["title"] = "Home"
	}

	ctx.HTML(200, WIKI_NEW)
}

func NewWikiPost(ctx *context.Context, form auth.NewWikiForm) {
	ctx.Data["Title"] = ctx.Tr("repo.wiki.new_page")
	ctx.Data["PageIsWiki"] = true
	ctx.Data["RequireSimpleMDE"] = true

	if ctx.HasError() {
		ctx.HTML(200, WIKI_NEW)
		return
	}

	if err := ctx.Repo.Repository.AddWikiPage(ctx.User, form.Title, form.Content, form.Message); err != nil {
		if models.IsErrWikiAlreadyExist(err) {
			ctx.Data["Err_Title"] = true
			ctx.RenderWithErr(ctx.Tr("repo.wiki.page_already_exists"), WIKI_NEW, &form)
		} else {
			ctx.Handle(500, "AddWikiPage", err)
		}
		return
	}

	ctx.Redirect(ctx.Repo.RepoLink + "/wiki/" + models.ToWikiPageURL(form.Title))
}

func EditWiki(ctx *context.Context) {
	ctx.Data["PageIsWiki"] = true
	ctx.Data["PageIsWikiEdit"] = true
	ctx.Data["RequireSimpleMDE"] = true

	if !ctx.Repo.Repository.HasWiki() {
		ctx.Redirect(ctx.Repo.RepoLink + "/wiki")
		return
	}

	renderWikiPage(ctx, false)
	if ctx.Written() {
		return
	}

	ctx.HTML(200, WIKI_NEW)
}

func EditWikiPost(ctx *context.Context, form auth.NewWikiForm) {
	ctx.Data["Title"] = ctx.Tr("repo.wiki.new_page")
	ctx.Data["PageIsWiki"] = true
	ctx.Data["RequireSimpleMDE"] = true

	if ctx.HasError() {
		ctx.HTML(200, WIKI_NEW)
		return
	}

	if err := ctx.Repo.Repository.EditWikiPage(ctx.User, form.OldTitle, form.Title, form.Content, form.Message); err != nil {
		ctx.Handle(500, "EditWikiPage", err)
		return
	}

	ctx.Redirect(ctx.Repo.RepoLink + "/wiki/" + models.ToWikiPageURL(form.Title))
}

func DeleteWikiPagePost(ctx *context.Context) {
	pageURL := ctx.Params(":page")
	if len(pageURL) == 0 {
		pageURL = "Home"
	}

	pageName := models.ToWikiPageName(pageURL)
	if err := ctx.Repo.Repository.DeleteWikiPage(ctx.User, pageName); err != nil {
		ctx.Handle(500, "DeleteWikiPage", err)
		return
	}

	ctx.JSON(200, map[string]interface{}{
		"redirect": ctx.Repo.RepoLink + "/wiki/",
	})
}
