// Copyright 2015 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package user

import (
	api "github.com/gogits/go-gogs-client"

	"github.com/gogits/gogs/models"
	"github.com/gogits/gogs/modules/context"
	"github.com/gogits/gogs/routers/api/v1/convert"
)

func responseApiUsers(ctx *context.APIContext, users []*models.User) {
	apiUsers := make([]*api.User, len(users))
	for i := range users {
		apiUsers[i] = convert.ToUser(users[i])
	}
	ctx.JSON(200, &apiUsers)
}

func listUserFollowers(ctx *context.APIContext, u *models.User) {
	users, err := u.GetFollowers(ctx.QueryInt("page"))
	if err != nil {
		ctx.Error(500, "GetUserFollowers", err)
		return
	}
	responseApiUsers(ctx, users)
}

func ListMyFollowers(ctx *context.APIContext) {
	listUserFollowers(ctx, ctx.User)
}

// https://github.com/gogits/go-gogs-client/wiki/Users-Followers#list-followers-of-a-user
func ListFollowers(ctx *context.APIContext) {
	u := GetUserByParams(ctx)
	if ctx.Written() {
		return
	}
	listUserFollowers(ctx, u)
}

func listUserFollowing(ctx *context.APIContext, u *models.User) {
	users, err := u.GetFollowing(ctx.QueryInt("page"))
	if err != nil {
		ctx.Error(500, "GetFollowing", err)
		return
	}
	responseApiUsers(ctx, users)
}

func ListMyFollowing(ctx *context.APIContext) {
	listUserFollowing(ctx, ctx.User)
}

// https://github.com/gogits/go-gogs-client/wiki/Users-Followers#list-users-followed-by-another-user
func ListFollowing(ctx *context.APIContext) {
	u := GetUserByParams(ctx)
	if ctx.Written() {
		return
	}
	listUserFollowing(ctx, u)
}

func checkUserFollowing(ctx *context.APIContext, u *models.User, followID int64) {
	if u.IsFollowing(followID) {
		ctx.Status(204)
	} else {
		ctx.Status(404)
	}
}

// https://github.com/gogits/go-gogs-client/wiki/Users-Followers#check-if-you-are-following-a-user
func CheckMyFollowing(ctx *context.APIContext) {
	target := GetUserByParams(ctx)
	if ctx.Written() {
		return
	}
	checkUserFollowing(ctx, ctx.User, target.Id)
}

// https://github.com/gogits/go-gogs-client/wiki/Users-Followers#check-if-one-user-follows-another
func CheckFollowing(ctx *context.APIContext) {
	u := GetUserByParams(ctx)
	if ctx.Written() {
		return
	}
	target := GetUserByParamsName(ctx, ":target")
	if ctx.Written() {
		return
	}
	checkUserFollowing(ctx, u, target.Id)
}

// https://github.com/gogits/go-gogs-client/wiki/Users-Followers#follow-a-user
func Follow(ctx *context.APIContext) {
	target := GetUserByParams(ctx)
	if ctx.Written() {
		return
	}
	if err := models.FollowUser(ctx.User.Id, target.Id); err != nil {
		ctx.Error(500, "FollowUser", err)
		return
	}
	ctx.Status(204)
}

// https://github.com/gogits/go-gogs-client/wiki/Users-Followers#unfollow-a-user
func Unfollow(ctx *context.APIContext) {
	target := GetUserByParams(ctx)
	if ctx.Written() {
		return
	}
	if err := models.UnfollowUser(ctx.User.Id, target.Id); err != nil {
		ctx.Error(500, "UnfollowUser", err)
		return
	}
	ctx.Status(204)
}
