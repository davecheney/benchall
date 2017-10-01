// Copyright 2014 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package auth

import (
	"github.com/go-macaron/binding"
	"gopkg.in/macaron.v1"
)

type AuthenticationForm struct {
	ID                int64
	Type              int    `binding:"Range(2,5)"`
	Name              string `binding:"Required;MaxSize(30)"`
	Host              string
	Port              int
	BindDN            string
	BindPassword      string
	UserBase          string
	UserDN            string
	AttributeUsername string
	AttributeName     string
	AttributeSurname  string
	AttributeMail     string
	AttributesInBind  bool
	Filter            string
	AdminFilter       string
	IsActive          bool
	SMTPAuth          string
	SMTPHost          string
	SMTPPort          int
	AllowedDomains    string
	TLS               bool
	SkipVerify        bool
	PAMServiceName    string
}

func (f *AuthenticationForm) Validate(ctx *macaron.Context, errs binding.Errors) binding.Errors {
	return validate(errs, ctx.Data, f, ctx.Locale)
}
