package main

import (
	"context"
	"os"

	"windowseat/internal/install"
)

func runInstall(_ context.Context, args []string) error {
	flags := newFlags("install")
	noTray := flags.Bool("no-tray", false, "install and start the service only")
	dryRun := flags.Bool("dry-run", false, "print the steps without running them")
	if err := flags.Parse(args); err != nil {
		return err
	}
	env, err := install.Current()
	if err != nil {
		return err
	}
	plan, err := install.InstallPlan(env, install.Options{NoTray: *noTray})
	if err != nil {
		return err
	}
	return install.Run(os.Stdout, plan, *dryRun)
}

func runUninstall(_ context.Context, args []string) error {
	flags := newFlags("uninstall")
	dryRun := flags.Bool("dry-run", false, "print the steps without running them")
	if err := flags.Parse(args); err != nil {
		return err
	}
	env, err := install.Current()
	if err != nil {
		return err
	}
	plan, err := install.UninstallPlan(env)
	if err != nil {
		return err
	}
	return install.Run(os.Stdout, plan, *dryRun)
}
