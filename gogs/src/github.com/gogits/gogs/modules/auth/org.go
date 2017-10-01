// Copyright 2014 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package auth

import (
	"github.com/go-macaron/binding"
	"gopkg.in/macaron.v1"
)

// ________                            .__                __  .__
// \_____  \_______  _________    ____ |__|____________ _/  |_|__| ____   ____
//  /   |   \_  __ \/ ___\__  \  /    \|  \___   /\__  \\   __\  |/  _ \ /    \
// /    |    \  | \/ /_/  > __ \|   |  \  |/    /  / __ \|  | |  (  <_> )   |  \
// \_______  /__|  \___  (____  /___|  /__/_____ \(____  /__| |__|\____/|___|  /
//         \/     /_____/     \/     \/         \/     \/                    \/

type CreateOrgForm struct {
	OrgName string `binding:"Required;AlphaDashDot;MaxSize(35)" locale:"org.org_name_holder"`
}

func (f *CreateOrgForm) Validate(ctx *macaron.Context, errs binding.Errors) binding.Errors {
	return validate(errs, ctx.Data, f, ctx.Locale)
}

type UpdateOrgSettingForm struct {
	Name            string `binding:"Required;AlphaDashDot;MaxSize(35)" locale:"org.org_name_holder"`
	FullName        string `binding:"MaxSize(100)"`
	Description     string `binding:"MaxSize(255)"`
	Website         string `binding:"Url;MaxSize(100)"`
	Location        string `binding:"MaxSize(50)"`
	MaxRepoCreation int
}

func (f *UpdateOrgSettingForm) Validate(ctx *macaron.Context, errs binding.Errors) binding.Errors {
	return validate(errs, ctx.Data, f, ctx.Locale)
}

// ___________
// \__    ___/___ _____    _____
//   |    |_/ __ \\__  \  /     \
//   |    |\  ___/ / __ \|  Y Y  \
//   |____| \___  >____  /__|_|  /
//              \/     \/      \/

type CreateTeamForm struct {
	TeamName    string `binding:"Required;AlphaDashDot;MaxSize(30)"`
	Description string `binding:"MaxSize(255)"`
	Permission  string
}

func (f *CreateTeamForm) Validate(ctx *macaron.Context, errs binding.Errors) binding.Errors {
	return validate(errs, ctx.Data, f, ctx.Locale)
}
