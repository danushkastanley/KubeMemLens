package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/traceclient"
	"github.com/spf13/cobra"
)

func newTraceTargetCommand(action string, options collectorOptionsProvider, factory traceFactory) *cobra.Command {
	var flags traceTargetFlags
	var export traceExportFlags
	descriptions := map[string]string{"preflight": "Check an exact container lifetime without creating a trace", "create": "Create a pending admission; watch it before its short expiry", "run": "Preflight, run and summarise one bounded trace"}
	cmd := &cobra.Command{Use: action + " POD", Short: descriptions[action], Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		intent, err := flags.intent()
		if err != nil {
			return err
		}
		if err := export.validate(false); err != nil {
			return err
		}
		c, err := factory(options())
		if err != nil {
			return err
		}
		defer c.Close()
		ctx, stop := traceContext(cmd)
		defer stop()
		// Discovery precedes even the target read: no extension means no trace action.
		if err := c.Discover(ctx); err != nil {
			return err
		}
		selection, err := c.Select(ctx, flags.namespace, args[0], flags.container)
		if err != nil {
			return err
		}
		if action == "create" {
			plan, err := c.Preflight(ctx, selection, intent)
			if err != nil {
				return err
			}
			a, err := c.Create(ctx, plan)
			if err != nil {
				if a.ID() != "" {
					return cleanupFailedAdmission(cmd, c, a, err)
				}
				return err
			}
			if ctx.Err() != nil {
				return cleanupFailedAdmission(cmd, c, a, ctx.Err())
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "admitted %s\nnamespace: %s\npending expiry: %s\nUse trace watch before expiry; activation is a separate action.\n", a.ID(), a.Namespace(), a.ExpiresAt().Format(time.RFC3339Nano))
			if err != nil {
				return cleanupFailedAdmission(cmd, c, a, err)
			}
			return nil
		}
		session, err := traceclient.NewSession(c, selection, intent)
		if err != nil {
			return err
		}
		if err := session.Prepare(ctx); err != nil {
			return err
		}
		if action == "preflight" {
			report, _ := session.PreflightReport()
			return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
		}
		if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "Preflight passed for %s/%s container %s; %s, %s, at most %d events.\nDevelopment profile: resource qualification incomplete. Raw paths omitted.\n", selection.Namespace, selection.Pod, selection.Container, intent.Kind, intent.Bounds.Duration, intent.Bounds.Events); err != nil {
			return err
		}
		return executeTraceSession(cmd, ctx, session, export)
	}
	flags.add(cmd)
	if action == "run" {
		export.add(cmd)
	}
	return cmd
}

func cleanupFailedAdmission(cmd *cobra.Command, c *traceclient.Client, a traceclient.Admission, original error) error {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	cleanup, err := c.Cancel(ctx, a)
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "cleanup: %s\n", cleanup)
	if err != nil {
		return fmt.Errorf("%w; cleanup was not confirmed: %v", original, err)
	}
	return original
}
