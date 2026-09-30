package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/spf13/cobra"
)

type incidentCommands struct {
	options   collectorOptionsProvider
	namespace string
}

type incidentAction func(*cobra.Command, client.IncidentSessions, []string) (incidentsession.Summary, error)

func newIncidentSessionCommand(options collectorOptionsProvider) *cobra.Command {
	c := &incidentCommands{options: options}
	root := &cobra.Command{Use: "session", Short: "Keep a bounded incident timeline with authorised evidence"}
	root.PersistentFlags().StringVarP(&c.namespace, "namespace", "n", "default", "namespace owning the incident session")
	root.AddCommand(c.action("start", "Start an owner-only incident session", 0, func(cmd *cobra.Command, client client.IncidentSessions, _ []string) (incidentsession.Summary, error) {
		return client.StartIncident(cmd.Context())
	}))
	root.AddCommand(c.action("status <id>", "Inspect session lifetime, limits and latest action", 1, func(cmd *cobra.Command, client client.IncidentSessions, args []string) (incidentsession.Summary, error) {
		return client.InspectIncident(cmd.Context(), args[0])
	}))
	root.AddCommand(c.action("close <id>", "Close a session while keeping it available until expiry", 1, func(cmd *cobra.Command, client client.IncidentSessions, args []string) (incidentsession.Summary, error) {
		return client.CloseIncident(cmd.Context(), args[0])
	}))
	root.AddCommand(c.action("capture <id> <pod>", "Retain an authorised Pod memory capture", 2, func(cmd *cobra.Command, client client.IncidentSessions, args []string) (incidentsession.Summary, error) {
		return client.CaptureIncident(cmd.Context(), args[0], args[1])
	}))
	root.AddCommand(c.annotation(), c.markers(), c.compare(), c.traceReference(), c.export(), c.show(), c.delete(), newSessionReplay(), newSessionExportComparison(), newSessionTraceVerification())
	return root
}

func (c *incidentCommands) connect() (*client.IncidentClient, error) {
	options, err := withReadScope(c.options(), c.namespace, false)
	if err != nil {
		return nil, err
	}
	return client.NewIncidentSessionConnection(options)
}

func (c *incidentCommands) action(use, description string, count int, run incidentAction) *cobra.Command {
	var output string
	command := &cobra.Command{Use: use, Short: description, Args: cobra.ExactArgs(count), RunE: func(cmd *cobra.Command, args []string) error {
		if output != "text" && output != "json" {
			return fmt.Errorf("output must be text or json")
		}
		connection, err := c.connect()
		if err != nil {
			return err
		}
		defer connection.Close()
		result, err := run(cmd, connection, args)
		if err != nil {
			return err
		}
		if output == "json" {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		}
		return printIncidentStatus(cmd.OutOrStdout(), result)
	}}
	command.Flags().StringVarP(&output, "output", "o", "text", "output format: text or json")
	return command
}

func printIncidentStatus(w io.Writer, s incidentsession.Summary) error {
	_, err := fmt.Fprintf(w, "Session %s: %s; %d entries; expires %s\n", s.ID, s.Latest.Kind, s.Entries, s.ExpiresAt.Format(time.RFC3339))
	if err != nil {
		return err
	}
	if s.Latest.GapReason != "" {
		if _, err := fmt.Fprintln(w, "Evidence gap:", strings.ReplaceAll(s.Latest.GapReason, "-", " ")); err != nil {
			return err
		}
	}
	if s.Latest.ClockUncertain {
		if _, err := fmt.Fprintln(w, "Clock uncertainty is recorded in the timeline."); err != nil {
			return err
		}
	}
	if s.LimitReached {
		_, err = fmt.Fprintln(w, "Session limit reached. Existing evidence is retained; closing capacity is reserved.")
	}
	return err
}

func (c *incidentCommands) annotation() *cobra.Command {
	var path string
	command := c.action("annotate <id>", "Record a private single-line annotation from a file or stdin", 1, func(cmd *cobra.Command, connection client.IncidentSessions, args []string) (incidentsession.Summary, error) {
		var reader io.Reader = cmd.InOrStdin()
		if path != "-" {
			file, err := os.Open(path)
			if err != nil {
				return incidentsession.Summary{}, fmt.Errorf("cannot open annotation file")
			}
			defer file.Close()
			reader = file
		}
		data, err := io.ReadAll(io.LimitReader(reader, 515))
		if err != nil {
			return incidentsession.Summary{}, fmt.Errorf("cannot read annotation")
		}
		note := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
		if incidentsession.ValidateAnnotation(note) != nil {
			return incidentsession.Summary{}, fmt.Errorf("annotation must be one line of at most 512 UTF-8 bytes")
		}
		return connection.AnnotateIncident(cmd.Context(), args[0], note)
	})
	command.Flags().StringVar(&path, "note-file", "", "annotation file, or - for stdin; annotations may contain secrets and are omitted from sanitised exports")
	_ = command.MarkFlagRequired("note-file")
	return command
}

func (c *incidentCommands) delete() *cobra.Command {
	return &cobra.Command{Use: "delete <id>", Short: "Delete an owned session and all retained evidence", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		connection, err := c.connect()
		if err != nil {
			return err
		}
		defer connection.Close()
		if err := connection.DeleteIncident(cmd.Context(), args[0]); err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "Session deleted.")
		return err
	}}
}
