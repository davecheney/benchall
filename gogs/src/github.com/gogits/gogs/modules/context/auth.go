// Copyright 2014 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package context

import (
	"net/url"

	"github.com/go-macaron/csrf"
	"gopkg.in/macaron.v1"

	"github.com/gogits/gogs/modules/auth"
	"github.com/gogits/gogs/modules/setting"
)

type ToggleOptions struct {
	SignInRequired  bool
	SignOutRequired bool
	AdminRequired   bool
	DisableCSRF     bool
}

func Toggle(options *ToggleOptions) macaron.Handler {
	return func(ctx *Context) {
		// Cannot view any page before installation.
		if !setting.InstallLock {
			ctx.Redirect(setting.AppSubUrl + "/install")
			return
		}

		// Checking non-logged users landing page.
		if !ctx.IsSigned && ctx.Req.RequestURI == "/" && setting.LandingPageUrl != setting.LANDING_PAGE_HOME {
			ctx.Redirect(setting.AppSubUrl + string(setting.LandingPageUrl))
			return
		}

		// Redirect to dashboard if user tries to visit any non-login page.
		if options.SignOutRequired && ctx.IsSigned && ctx.Req.RequestURI != "/" {
			ctx.Redirect(setting.AppSubUrl + "/")
			return
		}

		if !options.SignOutRequired && !options.DisableCSRF && ctx.Req.Method == "POST" && !auth.IsAPIPath(ctx.Req.URL.Path) {
			csrf.Validate(ctx.Context, ctx.csrf)
			if ctx.Written() {
				return
			}
		}

		if options.SignInRequired {
			if !ctx.IsSigned {
				// Restrict API calls with error message.
				if auth.IsAPIPath(ctx.Req.URL.Path) {
					ctx.JSON(403, map[string]string{
						"message": "Only signed in user is allowed to call APIs.",
					})
					return
				}

				ctx.SetCookie("redirect_to", url.QueryEscape(setting.AppSubUrl+ctx.Req.RequestURI), 0, setting.AppSubUrl)
				ctx.Redirect(setting.AppSubUrl + "/user/login")
				return
			} else if !ctx.User.IsActive && setting.Service.RegisterEmailConfirm {
				ctx.Data["Title"] = ctx.Tr("auth.active_your_account")
				ctx.HTML(200, "user/auth/activate")
				return
			}
		}

		// Redirect to log in page if auto-signin info is provided and has not signed in.
		if !options.SignOutRequired && !ctx.IsSigned && !auth.IsAPIPath(ctx.Req.URL.Path) &&
			len(ctx.GetCookie(setting.CookieUserName)) > 0 {
			ctx.SetCookie("redirect_to", url.QueryEscape(setting.AppSubUrl+ctx.Req.RequestURI), 0, setting.AppSubUrl)
			ctx.Redirect(setting.AppSubUrl + "/user/login")
			return
		}

		if options.AdminRequired {
			if !ctx.User.IsAdmin {
				ctx.Error(403)
				return
			}
			ctx.Data["PageIsAdmin"] = true
		}
	}
}
