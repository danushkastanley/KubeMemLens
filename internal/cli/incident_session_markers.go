package cli

import (
	"net/url"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/spf13/cobra"
)

func (c *incidentCommands) markers() *cobra.Command {
	var source, metric, start, end, step string
	command := c.action("markers <id> <pod>", "Retain source-labelled memory history and change markers", 2, func(cmd *cobra.Command, connection client.IncidentSessions, args []string) (incidentsession.Summary, error) {
		values := url.Values{}
		for name, value := range map[string]string{"source": source, "metric": metric, "start": start, "end": end, "step": step} {
			if value != "" {
				values.Set(name, value)
			}
		}
		query, err := memoryhistory.ParseQuery(values, memoryhistory.Pod, time.Now().UTC())
		if err != nil {
			return incidentsession.Summary{}, err
		}
		return connection.MarkIncident(cmd.Context(), args[0], args[1], query)
	})
	command.Flags().StringVar(&source, "source", "local", "history source: local or prometheus")
	command.Flags().StringVar(&metric, "metric", "", "history metric; defaults follow the selected source")
	command.Flags().StringVar(&start, "start", "", "history start (RFC3339)")
	command.Flags().StringVar(&end, "end", "", "history end (RFC3339)")
	command.Flags().StringVar(&step, "step", "", "query step in seconds")
	return command
}
