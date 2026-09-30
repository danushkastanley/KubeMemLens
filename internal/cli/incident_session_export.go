package cli

import (
	"fmt"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/sessionview"
	"github.com/spf13/cobra"
)

func (c *incidentCommands) export() *cobra.Command {
	var output string
	var sensitive, overwrite bool
	cmd := &cobra.Command{Use: "export <id>", Short: "Export a sanitised timeline or an explicitly authorised full record", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		connection, err := c.connect()
		if err != nil {
			return err
		}
		defer connection.Close()
		mode := incidentsession.ExportSanitised
		if sensitive {
			mode = incidentsession.ExportAuthorised
		}
		data, err := connection.ExportIncident(cmd.Context(), args[0], mode)
		if err != nil {
			return err
		}
		return incident.WriteSession(cmd.OutOrStdout(), output, overwrite, data)
	}}
	cmd.Flags().StringVarP(&output, "output", "o", "-", "output file, or - for stdout")
	cmd.Flags().BoolVar(&sensitive, "include-sensitive", false, "include private annotations, identities and capture bytes; requires full export permission")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "atomically replace an existing output file")
	return cmd
}

func (c *incidentCommands) show() *cobra.Command {
	return &cobra.Command{Use: "show <id>", Short: "Show the sanitised timeline and its evidence aliases", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		connection, err := c.connect()
		if err != nil {
			return err
		}
		defer connection.Close()
		data, err := connection.ExportIncident(cmd.Context(), args[0], incidentsession.ExportSanitised)
		if err != nil {
			return err
		}
		doc, err := incidentsession.DecodeExport(data)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), strings.Join(sessionview.SanitisedLines(*doc.Sanitised), "\n"))
		return err
	}}
}
