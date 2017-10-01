// Copyright 2014 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package repo

import (
	"io"
	"path"

	"github.com/gogits/git-module"

	"github.com/gogits/gogs/modules/base"
	"github.com/gogits/gogs/modules/context"
)

func ServeData(ctx *context.Context, name string, reader io.Reader) error {
	buf := make([]byte, 1024)
	n, _ := reader.Read(buf)
	if n > 0 {
		buf = buf[:n]
	}

	_, isTextFile := base.IsTextFile(buf)
	if !isTextFile {
		_, isImageFile := base.IsImageFile(buf)
		if !isImageFile {
			ctx.Resp.Header().Set("Content-Disposition", "attachment; filename="+path.Base(ctx.Repo.TreeName))
			ctx.Resp.Header().Set("Content-Transfer-Encoding", "binary")
		}
	} else {
		ctx.Resp.Header().Set("Content-Type", "text/plain")
	}
	ctx.Resp.Write(buf)
	_, err := io.Copy(ctx.Resp, reader)
	return err
}

func ServeBlob(ctx *context.Context, blob *git.Blob) error {
	dataRc, err := blob.Data()
	if err != nil {
		return err
	}

	return ServeData(ctx, ctx.Repo.TreeName, dataRc)
}

func SingleDownload(ctx *context.Context) {
	blob, err := ctx.Repo.Commit.GetBlobByPath(ctx.Repo.TreeName)
	if err != nil {
		if git.IsErrNotExist(err) {
			ctx.Handle(404, "GetBlobByPath", nil)
		} else {
			ctx.Handle(500, "GetBlobByPath", err)
		}
		return
	}
	if err = ServeBlob(ctx, blob); err != nil {
		ctx.Handle(500, "ServeBlob", err)
	}
}
