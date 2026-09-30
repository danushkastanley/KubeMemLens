package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/buildinfo"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/traceclient"
	"github.com/danushkastanley/kube-memlens/internal/tracereport"
	"github.com/spf13/cobra"
)

type traceExportFlags struct {
	output             string
	confirm, overwrite bool
}

func (f *traceExportFlags) add(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.output, "export", "", "explicit redacted report destination; '-' writes to stdout")
	cmd.Flags().BoolVar(&f.confirm, "confirm-export", false, "acknowledge that exported timings and counters remain sensitive operational data")
	cmd.Flags().BoolVar(&f.overwrite, "overwrite", false, "explicitly replace an existing export file")
}
func (f traceExportFlags) validate(required bool) error {
	if required && f.output == "" {
		return fmt.Errorf("trace export requires --export FILE and --confirm-export")
	}
	if f.output != "" && !f.confirm {
		return fmt.Errorf("export contains sensitive operational timings and counters; acknowledge with --confirm-export")
	}
	if f.output == "" && (f.confirm || f.overwrite) {
		return fmt.Errorf("export options require --export FILE")
	}
	return nil
}
func newTraceIDCommand(action string, options collectorOptionsProvider, factory traceFactory) *cobra.Command {
	var namespace string
	var export traceExportFlags
	descriptions := map[string]string{"watch": "Activate and observe an existing admission once", "cancel": "Cancel an exact admission and report the cleanup receipt", "export": "Watch an existing admission and explicitly export its terminal report"}
	cmd := &cobra.Command{Use: action + " ID", Short: descriptions[action], Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := export.validate(action == "export"); err != nil {
			return err
		}
		c, err := factory(options())
		if err != nil {
			return err
		}
		defer c.Close()
		ctx, stop := traceContext(cmd)
		defer stop()
		// Cancellation must remain possible when discovery is degraded.
		if action == "cancel" {
			cleanup, cancelErr := c.CancelID(ctx, namespace, args[0])
			_, outputErr := fmt.Fprintf(cmd.OutOrStdout(), "cleanup: %s\n", cleanup)
			if cancelErr != nil {
				return cancelErr
			}
			return outputErr
		}
		if err := c.Discover(ctx); err != nil {
			return err
		}
		a, err := c.Inspect(ctx, namespace, args[0])
		if err != nil {
			return err
		}
		session, err := traceclient.NewAttachedSession(c, a)
		if err != nil {
			return err
		}
		return executeTraceSession(cmd, ctx, session, export)
	}
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "admission namespace")
	if action != "cancel" {
		export.add(cmd)
	}
	return cmd
}

func executeTraceSession(cmd *cobra.Command, ctx context.Context, session *traceclient.Session, export traceExportFlags) error {
	if err := session.Start(ctx); err != nil {
		return err
	}
	var last traceclient.State
	var admissionShown, metadataShown bool
	var outputErr error
	for {
		select {
		case <-session.Updates():
			value := session.Snapshot()
			if value.AdmissionID != "" && !admissionShown && outputErr == nil {
				admissionShown = true
				_, outputErr = fmt.Fprintf(cmd.ErrOrStderr(), "admission: %s\n", value.AdmissionID)
			}
			if value.Result.Metadata.SessionID != "" && !metadataShown && outputErr == nil {
				metadataShown = true
				_, outputErr = fmt.Fprintln(cmd.ErrOrStderr(), "stream metadata validated")
			}
			if value.State != last && outputErr == nil {
				last = value.State
				_, outputErr = fmt.Fprintf(cmd.ErrOrStderr(), "trace: %s\n", value.State)
			}
			if outputErr != nil {
				session.Cancel()
			}
		case <-session.Done():
			value := session.Snapshot()
			if outputErr != nil {
				return outputErr
			}
			// Summary output never interleaves with a JSON export to stdout.
			out := cmd.OutOrStdout()
			if export.output == "-" {
				out = cmd.ErrOrStderr()
			}
			_, err := fmt.Fprintf(out, "state: %s\ncleanup: %s\ntransport complete: %t\nvalidated event frames: %d\n", value.State, value.Cleanup, value.Result.TransportComplete, value.Result.DeliveredEvents)
			if err != nil {
				return err
			}
			if summary, known := value.Result.Summary(); known {
				_, err = fmt.Fprintf(out, "termination: %s\nincomplete evidence: %t\nengine produced: %s; sampled: %s; lost: %s; rejected: %s\n", summary.Termination, summary.Incomplete, traceCount(summary.EngineCounts.Produced), traceCount(summary.EngineCounts.Sampled), traceCount(summary.EngineCounts.Lost), traceCount(summary.EngineCounts.Rejected))
			} else {
				_, err = fmt.Fprintln(out, "terminal summary: unavailable; engine counts and observation windows are unknown")
			}
			if err != nil {
				return err
			}
			if export.output != "" {
				document, err := tracereport.New(value, buildinfo.Version, time.Now())
				if err != nil {
					return err
				}
				if err := incident.WriteTrace(cmd.OutOrStdout(), export.output, export.overwrite, document); err != nil {
					return err
				}
			}
			if value.Failure != nil {
				return value.Failure
			}
			if value.CleanupFailure != nil {
				return value.CleanupFailure
			}
			return nil
		}
	}
}
func traceCount(value *uint64) string {
	if value == nil {
		return "unreported"
	}
	return fmt.Sprint(*value)
}
