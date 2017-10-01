// Copyright 2014 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package routers

import (
	"fmt"

	"github.com/Unknwon/paginater"

	"github.com/gogits/gogs/models"
	"github.com/gogits/gogs/modules/base"
	"github.com/gogits/gogs/modules/context"
	"github.com/gogits/gogs/modules/setting"
	"github.com/gogits/gogs/routers/user"
)

const (
	HOME          base.TplName = "home"
	EXPLORE_REPOS base.TplName = "explore/repos"
	EXPLORE_USERS base.TplName = "explore/users"
)

func Home(ctx *context.Context) {
	if ctx.IsSigned {
		if !ctx.User.IsActive && setting.Service.RegisterEmailConfirm {
			ctx.Data["Title"] = ctx.Tr("auth.active_your_account")
			ctx.HTML(200, user.ACTIVATE)
		} else {
			user.Dashboard(ctx)
		}
		return
	}

	// Check auto-login.
	uname := ctx.GetCookie(setting.CookieUserName)
	if len(uname) != 0 {
		ctx.Redirect(setting.AppSubUrl + "/user/login")
		return
	}

	ctx.Data["PageIsHome"] = true
	ctx.HTML(200, HOME)
}

type RepoSearchOptions struct {
	Counter  func() int64
	Ranger   func(int, int) ([]*models.Repository, error)
	Private  bool
	PageSize int
	OrderBy  string
	TplName  base.TplName
}

func RenderRepoSearch(ctx *context.Context, opts *RepoSearchOptions) {
	page := ctx.QueryInt("page")
	if page <= 1 {
		page = 1
	}

	var (
		repos []*models.Repository
		count int64
		err   error
	)

	keyword := ctx.Query("q")
	if len(keyword) == 0 {
		repos, err = opts.Ranger(page, opts.PageSize)
		if err != nil {
			ctx.Handle(500, "opts.Ranger", err)
			return
		}
		count = opts.Counter()
	} else {
		repos, count, err = models.SearchRepositoryByName(&models.SearchRepoOptions{
			Keyword:  keyword,
			OrderBy:  opts.OrderBy,
			Private:  opts.Private,
			Page:     page,
			PageSize: opts.PageSize,
		})
		if err != nil {
			ctx.Handle(500, "SearchRepositoryByName", err)
			return
		}
	}
	ctx.Data["Keyword"] = keyword
	ctx.Data["Total"] = count
	ctx.Data["Page"] = paginater.New(int(count), opts.PageSize, page, 5)

	for _, repo := range repos {
		if err = repo.GetOwner(); err != nil {
			ctx.Handle(500, "GetOwner", fmt.Errorf("%d: %v", repo.ID, err))
			return
		}
	}
	ctx.Data["Repos"] = repos

	ctx.HTML(200, opts.TplName)
}

func ExploreRepos(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("explore")
	ctx.Data["PageIsExplore"] = true
	ctx.Data["PageIsExploreRepositories"] = true

	RenderRepoSearch(ctx, &RepoSearchOptions{
		Counter:  models.CountPublicRepositories,
		Ranger:   models.GetRecentUpdatedRepositories,
		PageSize: setting.ExplorePagingNum,
		OrderBy:  "updated_unix DESC",
		TplName:  EXPLORE_REPOS,
	})
}

type UserSearchOptions struct {
	Type     models.UserType
	Counter  func() int64
	Ranger   func(int, int) ([]*models.User, error)
	PageSize int
	OrderBy  string
	TplName  base.TplName
}

func RenderUserSearch(ctx *context.Context, opts *UserSearchOptions) {
	page := ctx.QueryInt("page")
	if page <= 1 {
		page = 1
	}

	var (
		users []*models.User
		count int64
		err   error
	)

	keyword := ctx.Query("q")
	if len(keyword) == 0 {
		users, err = opts.Ranger(page, opts.PageSize)
		if err != nil {
			ctx.Handle(500, "opts.Ranger", err)
			return
		}
		count = opts.Counter()
	} else {
		users, count, err = models.SearchUserByName(&models.SearchUserOptions{
			Keyword:  keyword,
			Type:     opts.Type,
			OrderBy:  opts.OrderBy,
			Page:     page,
			PageSize: opts.PageSize,
		})
		if err != nil {
			ctx.Handle(500, "SearchUserByName", err)
			return
		}
	}
	ctx.Data["Keyword"] = keyword
	ctx.Data["Total"] = count
	ctx.Data["Page"] = paginater.New(int(count), opts.PageSize, page, 5)
	ctx.Data["Users"] = users

	ctx.HTML(200, opts.TplName)
}

func ExploreUsers(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("explore")
	ctx.Data["PageIsExplore"] = true
	ctx.Data["PageIsExploreUsers"] = true

	RenderUserSearch(ctx, &UserSearchOptions{
		Type:     models.USER_TYPE_INDIVIDUAL,
		Counter:  models.CountUsers,
		Ranger:   models.Users,
		PageSize: setting.ExplorePagingNum,
		OrderBy:  "updated_unix DESC",
		TplName:  EXPLORE_USERS,
	})
}

func NotFound(ctx *context.Context) {
	ctx.Data["Title"] = "Page Not Found"
	ctx.Handle(404, "home.NotFound", nil)
}
