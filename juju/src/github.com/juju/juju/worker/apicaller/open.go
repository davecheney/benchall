// Copyright 2012-2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package apicaller

import (
	"time"

	"github.com/juju/errors"
	"github.com/juju/utils"

	"github.com/juju/juju/agent"
	"github.com/juju/juju/api"
	apiagent "github.com/juju/juju/api/agent"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/worker"
)

var (
	apiOpen = api.Open

	checkProvisionedStrategy = utils.AttemptStrategy{
		Total: 1 * time.Minute,
		Delay: 5 * time.Second,
	}
)

// OpenAPIState opens the API using the given information. The agent's
// password is changed if the fallback password was used to connect to
// the API.
func OpenAPIState(a agent.Agent) (_ api.Connection, err error) {
	agentConfig := a.CurrentConfig()
	info, ok := agentConfig.APIInfo()
	if !ok {
		return nil, errors.New("API info not available")
	}
	st, usedOldPassword, err := openAPIStateUsingInfo(info, agentConfig.OldPassword())
	if err != nil {
		return nil, err
	}
	defer func() {
		// NOTE(fwereade): we may close and overwrite st below,
		// so we need to double-check what we need to do here.
		if err != nil && st != nil {
			if err := st.Close(); err != nil {
				logger.Errorf("while closing API connection: %v", err)
			}
		}
	}()

	tag := agentConfig.Tag()
	entity, err := st.Agent().Entity(tag)
	if params.IsCodeUnauthorized(err) {
		logger.Errorf("agent terminating due to error returned during entity lookup: %v", err)
		return nil, worker.ErrTerminateAgent
	} else if err != nil {
		return nil, err
	}

	if entity.Life() == params.Dead {
		// The entity is Dead, so the password cannot (and should not) be updated.
		return st, nil
	}

	if !usedOldPassword {
		// Call set password with the current password.  If we've recently
		// become a controller, this will fix up our credentials in mongo.
		if err := entity.SetPassword(info.Password); err != nil {
			return nil, errors.Annotate(err, "can't reset agent password")
		}
	} else {
		// We succeeded in connecting with the fallback
		// password, so we need to create a new password
		// for the future.
		logger.Debugf("replacing insecure password")
		newPassword, err := utils.RandomPassword()
		if err != nil {
			return nil, err
		}
		err = setAgentPassword(newPassword, info.Password, a, entity)
		if err != nil {
			return nil, err
		}

		// Reconnect to the API with the new password.
		if err := st.Close(); err != nil {
			logger.Errorf("while closing API connection with old password: %v", err)
		}
		info.Password = newPassword

		// NOTE(fwereade): this is where we rebind st. If you accidentally make
		// it a local variable you will break this func in a subtle and currently-
		// untested way.
		st, err = apiOpen(info, api.DialOpts{})
		if err != nil {
			return nil, err
		}
	}

	return st, nil
}

func setAgentPassword(newPw, oldPw string, a agent.Agent, entity *apiagent.Entity) error {
	// Change the configuration *before* setting the entity
	// password, so that we avoid the possibility that
	// we might successfully change the entity's
	// password but fail to write the configuration,
	// thus locking us out completely.
	if err := a.ChangeConfig(func(c agent.ConfigSetter) error {
		c.SetPassword(newPw)
		c.SetOldPassword(oldPw)
		return nil
	}); err != nil {
		return err
	}
	return entity.SetPassword(newPw)
}

// OpenAPIStateUsingInfo opens the API using the given API
// information, and returns the opened state and the api entity with
// the given tag.
func OpenAPIStateUsingInfo(info *api.Info, oldPassword string) (api.Connection, error) {
	st, _, err := openAPIStateUsingInfo(info, oldPassword)
	return st, err
}

func openAPIStateUsingInfo(info *api.Info, oldPassword string) (api.Connection, bool, error) {
	// We let the API dial fail immediately because the
	// runner's loop outside the caller of openAPIState will
	// keep on retrying. If we block for ages here,
	// then the worker that's calling this cannot
	// be interrupted.
	st, err := apiOpen(info, api.DialOpts{})
	usedOldPassword := false
	if params.IsCodeUnauthorized(err) {
		// We've perhaps used the wrong password, so
		// try again with the fallback password.
		infoCopy := *info
		info = &infoCopy
		info.Password = oldPassword
		usedOldPassword = true
		st, err = apiOpen(info, api.DialOpts{})
	}
	// The provisioner may take some time to record the agent's
	// machine instance ID, so wait until it does so.
	if params.IsCodeNotProvisioned(err) {
		for a := checkProvisionedStrategy.Start(); a.Next(); {
			st, err = apiOpen(info, api.DialOpts{})
			if !params.IsCodeNotProvisioned(err) {
				break
			}
		}
	}
	if err != nil {
		if params.IsCodeNotProvisioned(err) || params.IsCodeUnauthorized(err) {
			logger.Errorf("agent terminating due to error returned during API open: %v", err)
			return nil, false, worker.ErrTerminateAgent
		}
		return nil, false, err
	}

	return st, usedOldPassword, nil
}
