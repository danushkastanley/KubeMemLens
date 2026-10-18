package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/tracereport"
	"github.com/spf13/cobra"
)

func (c *incidentCommands) traceReference() *cobra.Command {
	var path, output string
	var confirmed bool
	cmd := &cobra.Command{Use: "trace-reference <id>", Short: "Attach an operator-supplied reference to a local trace report", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if !confirmed {
			return fmt.Errorf("acknowledge sending report timings, counters and digests with --confirm-reference; report text stays local")
		}
		if output != "text" && output != "json" {
			return fmt.Errorf("output must be text or json")
		}
		data, err := readTraceReportFile(path)
		if err != nil {
			return err
		}
		connection, err := c.connect()
		if err != nil {
			return err
		}
		defer connection.Close()
		result, err := connection.ReferenceIncidentTrace(cmd.Context(), args[0], data)
		if err != nil {
			return err
		}
		if output == "json" {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		}
		return printIncidentStatus(cmd.OutOrStdout(), result)
	}
	cmd.Flags().StringVar(&path, "report", "", "existing local trace report; its exact bytes remain outside the collector")
	cmd.Flags().BoolVar(&confirmed, "confirm-reference", false, "send the derived private reference; source authenticity and original target remain unverified")
	cmd.Flags().StringVarP(&output, "output", "o", "text", "output format: text or json")
	_ = cmd.MarkFlagRequired("report")
	return cmd
}

func newSessionTraceVerification() *cobra.Command {
	var path string
	cmd := &cobra.Command{Use: "verify-trace <incident-export> <trace-alias-or-digest>", Short: "Check a local trace report against an authorised incident export", Args: cobra.ExactArgs(2)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		document, err := incident.ReadSession(args[0])
		if err != nil {
			return err
		}
		if document.Authorised == nil {
			return fmt.Errorf("trace verification requires an authorised incident export containing private reference digests")
		}
		ref, err := incidentsession.SelectTraceReference(*document.Authorised, args[1])
		if err != nil {
			return err
		}
		data, err := readTraceReportFile(path)
		if err != nil {
			return err
		}
		if tracereport.VerifyReference(ref, data) != nil {
			return fmt.Errorf("trace report does not match the selected incident reference")
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "Report bytes match the incident reference. Source authenticity remains unverified.")
		return err
	}
	cmd.Flags().StringVar(&path, "report", "", "existing local trace report to verify without contacting a cluster")
	_ = cmd.MarkFlagRequired("report")
	return cmd
}

func readTraceReportFile(path string) ([]byte, error) {
	if path == "" || path == "-" {
		return nil, fmt.Errorf("select a named regular trace report file")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > tracereport.MaxBytes {
		return nil, fmt.Errorf("select a regular trace report file of at most 32 KiB")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open trace report")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("trace report file changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, tracereport.MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read trace report")
	}
	if _, err := tracereport.Describe(data); err != nil {
		return nil, fmt.Errorf("trace report is invalid or outside the supported schema and size bounds")
	}
	return data, nil
}
