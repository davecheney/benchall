// Copyright 2014 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package repo

import (
	"github.com/gogits/git-module"

	"github.com/gogits/gogs/models"
	"github.com/gogits/gogs/modules/context"
	"github.com/gogits/gogs/routers/repo"
)

// https://github.com/gogits/go-gogs-client/wiki/Repositories-Contents#download-raw-content
func GetRawFile(ctx *context.APIContext) {
	if !ctx.Repo.HasAccess() {
		ctx.Status(404)
		return
	}

	blob, err := ctx.Repo.Commit.GetBlobByPath(ctx.Repo.TreeName)
	if err != nil {
		if git.IsErrNotExist(err) {
			ctx.Status(404)
		} else {
			ctx.Error(500, "GetBlobByPath", err)
		}
		return
	}
	if err = repo.ServeBlob(ctx.Context, blob); err != nil {
		ctx.Error(500, "ServeBlob", err)
	}
}

// https://github.com/gogits/go-gogs-client/wiki/Repositories-Contents#download-archive
func GetArchive(ctx *context.APIContext) {
	repoPath := models.RepoPath(ctx.Params(":username"), ctx.Params(":reponame"))
	gitRepo, err := git.OpenRepository(repoPath)
	if err != nil {
		ctx.Error(500, "OpenRepository", err)
		return
	}
	ctx.Repo.GitRepo = gitRepo

	repo.Download(ctx.Context)
}
