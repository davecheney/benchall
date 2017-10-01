// Copyright 2014 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package user

import (
	api "github.com/gogits/go-gogs-client"

	"github.com/gogits/gogs/models"
	"github.com/gogits/gogs/modules/context"
)

// https://github.com/gogits/go-gogs-client/wiki/Users#list-access-tokens-for-a-user
func ListAccessTokens(ctx *context.APIContext) {
	tokens, err := models.ListAccessTokens(ctx.User.Id)
	if err != nil {
		ctx.Error(500, "ListAccessTokens", err)
		return
	}

	apiTokens := make([]*api.AccessToken, len(tokens))
	for i := range tokens {
		apiTokens[i] = &api.AccessToken{tokens[i].Name, tokens[i].Sha1}
	}
	ctx.JSON(200, &apiTokens)
}

// https://github.com/gogits/go-gogs-client/wiki/Users#create-a-access-token
func CreateAccessToken(ctx *context.APIContext, form api.CreateAccessTokenOption) {
	t := &models.AccessToken{
		UID:  ctx.User.Id,
		Name: form.Name,
	}
	if err := models.NewAccessToken(t); err != nil {
		ctx.Error(500, "NewAccessToken", err)
		return
	}
	ctx.JSON(201, &api.AccessToken{t.Name, t.Sha1})
}
