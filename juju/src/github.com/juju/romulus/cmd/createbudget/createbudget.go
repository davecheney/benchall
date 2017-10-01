// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package createbudget

import (
	"fmt"
	"strconv"

	"github.com/juju/cmd"
	"github.com/juju/errors"
	"gopkg.in/macaroon-bakery.v1/httpbakery"

	api "github.com/juju/romulus/api/budget"
	rcmd "github.com/juju/romulus/cmd"
)

type createBudgetCommand struct {
	rcmd.HttpCommand
	Name  string
	Value string
}

// NewCreateBudgetCommand returns a new createBudgetCommand
func NewCreateBudgetCommand() cmd.Command {
	return &createBudgetCommand{}
}

const doc = `
Create a new budget with monthly limit.

Example:
 juju create-budget qa 42
     Creates a budget named 'qa' with a limit of 42.
`

// Info implements cmd.Command.Info.
func (c *createBudgetCommand) Info() *cmd.Info {
	return &cmd.Info{
		Name:    "create-budget",
		Purpose: "create a new budget",
		Doc:     doc,
	}
}

// Init implements cmd.Command.Init.
func (c *createBudgetCommand) Init(args []string) error {
	if len(args) < 2 {
		return errors.New("name and value required")
	}
	c.Name, c.Value = args[0], args[1]
	if _, err := strconv.ParseInt(c.Value, 10, 32); err != nil {
		return errors.New("budget value needs to be a whole number")
	}
	return cmd.CheckEmpty(args[2:])
}

// Run implements cmd.Command.Run and has most of the logic for the run command.
func (c *createBudgetCommand) Run(ctx *cmd.Context) error {
	defer c.Close()
	client, err := c.NewClient()
	if err != nil {
		return errors.Annotate(err, "failed to create an http client")
	}
	api, err := newAPIClient(client)
	if err != nil {
		return errors.Annotate(err, "failed to create an api client")
	}
	resp, err := api.CreateBudget(c.Name, c.Value)
	if err != nil {
		return errors.Annotate(err, "failed to create the budget")
	}
	fmt.Fprintf(ctx.Stdout, resp)
	return nil
}

var newAPIClient = newAPIClientImpl

func newAPIClientImpl(c *httpbakery.Client) (apiClient, error) {
	client := api.NewClient(c)
	return client, nil
}

type apiClient interface {
	CreateBudget(name string, limit string) (string, error)
}
