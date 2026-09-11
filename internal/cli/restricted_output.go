package cli

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/observationview"
	"sigs.k8s.io/yaml"
)

func writeRestrictedRows(w io.Writer, rows []observationview.Row, opts topOptions, now time.Time) error {
	switch opts.Output {
	case "json":
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(rows)
	case "yaml":
		data, err := yaml.Marshal(rows)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	case "csv":
		writer := csv.NewWriter(w)
		if !opts.NoHeaders {
			if err := writer.Write([]string{"mode", "namespace", "kind", "name", "pod", "node", "workingSetBytes", "sampledAt", "state", "source", "apiVersion"}); err != nil {
				return err
			}
		}
		for _, row := range rows {
			value := ""
			if row.Bytes() != nil {
				value = strconv.FormatUint(*row.Bytes(), 10)
			}
			evidence := observationview.Evidence(row)
			at := ""
			if !evidence.CapturedAt.IsZero() {
				at = evidence.CapturedAt.UTC().Format(time.RFC3339Nano)
			}
			if err := writer.Write([]string{string(row.Mode), row.Namespace, row.Kind, row.Name, row.PodName, row.NodeName, value, at, observationview.State(row, now), string(evidence.Source), evidence.APIVersion}); err != nil {
				return err
			}
		}
		writer.Flush()
		return writer.Error()
	default:
		writer := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if !opts.NoHeaders {
			fmt.Fprintln(writer, "MODE\tNAMESPACE\tKIND\tNAME\tPOD\tNODE\tWORKING SET\tSAMPLE AGE\tSTATE\tSOURCE")
		}
		for _, row := range rows {
			evidence := observationview.Evidence(row)
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", row.Mode, row.Namespace, row.Kind, row.Name, row.PodName, row.NodeName,
				observationview.Memory(row), observationview.Age(evidence.CapturedAt, now), observationview.State(row, now), string(evidence.Source)+" "+evidence.APIVersion)
		}
		return writer.Flush()
	}
}
