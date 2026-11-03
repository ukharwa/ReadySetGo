package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/ukharwa/readysetgo/internal/auth"
)

const userUsage = "usage: user invite | promote | demote | disable | enable | delete" +
	// readysetgo:begin module=twofactor
	" | reset-two-factor" +
	// readysetgo:end
	" EMAIL"

// user changes an account from the command line. invite emails an address
// a link to sign up (the way in when REGISTRATION=invite); promote is how
// the first admin is made, so the app never needs a default admin password;
// disable also logs the user out everywhere; delete removes the account and
// everything that belongs to it.
// readysetgo:begin module=twofactor
// reset-two-factor turns two-factor login off for someone who lost both
// their phone and their recovery codes; check it's really them first.
// readysetgo:end
func user(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	fs := flag.NewFlagSet("user", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return errors.New(userUsage)
	}
	action, address := fs.Arg(0), fs.Arg(1)

	cfg, logger, err := setup(getenv, stdout)
	if err != nil {
		return err
	}
	database, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeDB(database, logger)
	svc, err := newServices(cfg, database, logger)
	if err != nil {
		return err
	}
	accounts := svc.accounts
	if action == "invite" {
		if err := accounts.Invite(ctx, address); err != nil {
			return fmt.Errorf("invite %s: %w", address, err)
		}
		logger.Info("invitation queued; a running worker sends it")
		return nil
	}
	u, err := accounts.UserByEmail(ctx, address)
	if err != nil {
		return fmt.Errorf("%s %s: %w", action, address, err)
	}

	switch action {
	case "promote":
		err = accounts.SetRole(ctx, u.ID, auth.RoleAdmin)
	case "demote":
		err = accounts.SetRole(ctx, u.ID, auth.RoleUser)
	case "disable":
		err = accounts.SetDisabled(ctx, u.ID, true)
	case "enable":
		err = accounts.SetDisabled(ctx, u.ID, false)
	case "delete":
		err = accounts.DeleteUser(ctx, u.ID)
	// readysetgo:begin module=twofactor
	case "reset-two-factor":
		err = svc.twoFactor.Reset(ctx, u.ID)
	// readysetgo:end
	default:
		return errors.New(userUsage)
	}
	if err != nil {
		return fmt.Errorf("%s %s: %w", action, address, err)
	}
	logger.Info("user updated", "action", action)
	return nil
}
