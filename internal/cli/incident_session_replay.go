package cli

import (
	"fmt"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/sessionview"
	"github.com/spf13/cobra"
)

func newSessionReplay() *cobra.Command {
	var sensitive bool
	command := &cobra.Command{Use: "replay <file>", Short: "Read a bounded session export locally without contacting Kubernetes", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		doc, err := incident.ReadSession(args[0])
		if err != nil {
			return err
		}
		var lines []string
		if sensitive {
			if doc.Authorised == nil {
				return fmt.Errorf("private annotations require a full authorised export")
			}
			lines, err = sessionview.AuthorisedLines(*doc.Authorised)
		} else {
			public := doc.Sanitised
			if public == nil {
				projected, projectErr := incidentsession.Sanitise(*doc.Authorised)
				if projectErr != nil {
					return projectErr
				}
				public = &projected
			}
			lines = sessionview.SanitisedLines(*public)
		}
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), strings.Join(lines, "\n"))
		return err
	}}
	command.Flags().BoolVar(&sensitive, "include-sensitive", false, "display private annotations from a full export; capture bytes are never printed")
	return command
}

func newSessionExportComparison() *cobra.Command {
	return &cobra.Command{Use: "compare-export <file> <before> <after>", Short: "Compare retained aliases or digests in a local full export", Args: cobra.ExactArgs(3), RunE: func(cmd *cobra.Command, args []string) error {
		doc, err := incident.ReadSession(args[0])
		if err != nil {
			return err
		}
		if doc.Authorised == nil {
			return fmt.Errorf("retained capture comparison requires a full authorised export")
		}
		before, err := incidentsession.SelectCapture(*doc.Authorised, args[1])
		if err != nil {
			return err
		}
		after, err := incidentsession.SelectCapture(*doc.Authorised, args[2])
		if err != nil {
			return err
		}
		report, err := renderSessionComparison(before, after)
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(cmd.OutOrStdout(), report)
		return err
	}}
}
