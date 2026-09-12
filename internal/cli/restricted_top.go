package cli

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/observation"
	"github.com/danushkastanley/kube-memlens/internal/observationview"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
)

func runRestrictedTop(cmd *cobra.Command, session client.EvidenceSession, scope capability.Scope, opts topOptions, selector labels.Selector, fields fields.Selector) error {
	order := observationview.ByMemory
	switch opts.SortBy {
	case "total":
	case "name":
		order = observationview.ByName
	case "namespace":
		order = observationview.ByNamespace
	default:
		return fmt.Errorf("sorting by %s requires deep evidence; restricted mode supports total working set, name and namespace", opts.SortBy)
	}
	var lastRead time.Time
	refresh := func() error {
		batch, err := session.Observations.Current(cmd.Context())
		if err != nil {
			return err
		}
		rows := restrictedRows(batch, scope, selector, fields)
		observationview.Sort(rows, order)
		var frame bytes.Buffer
		if err := writeRestrictedRows(&frame, rows, opts, time.Now()); err != nil {
			return err
		}
		if opts.Watch && !lastRead.IsZero() {
			if _, err := fmt.Fprint(cmd.OutOrStdout(), "\x1b[H\x1b[2J"); err != nil {
				return err
			}
		}
		if _, err := frame.WriteTo(cmd.OutOrStdout()); err != nil {
			return err
		}
		lastRead = time.Now()
		return nil
	}
	if err := refresh(); err != nil {
		return err
	}
	if !opts.Watch {
		return nil
	}
	ticker := time.NewTicker(opts.WatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-cmd.Context().Done():
			return nil
		case <-ticker.C:
			if err := refresh(); err != nil {
				var queryErr *capability.SelectionError
				if !errors.As(err, &queryErr) {
					return err
				}
				if client.IsForbidden(err) {
					if _, clearErr := fmt.Fprint(cmd.OutOrStdout(), "\x1b[H\x1b[2J"); clearErr != nil {
						return clearErr
					}
					return err
				}
				if cmd.Context().Err() != nil {
					return nil
				}
				if _, writeErr := fmt.Fprintf(cmd.OutOrStdout(), "\nRefresh unavailable; last successful read %s ago: %s\n", observationview.Age(lastRead, time.Now()), err); writeErr != nil {
					return writeErr
				}
			}
		}
	}
}

func restrictedRows(batch observation.Batch, scope capability.Scope, selector labels.Selector, fieldSelector fields.Selector) []observationview.Row {
	result := []observationview.Row{}
	for _, row := range observationview.Rows(batch) {
		if row.Scope != scope || !fieldSelector.Matches(fieldValues(row.Name, row.Namespace, row.NodeName, row.Phase, row.Kind, row.PodName)) {
			continue
		}
		matches := true
		if selector != nil && !selector.Empty() {
			matches = labelMatches(selector, row.Labels)
			if scope == capability.WorkloadScope {
				matches = restrictedWorkloadLabels(batch, row, selector)
			}
		}
		if matches {
			result = append(result, row)
		}
	}
	return result
}

func restrictedWorkloadLabels(batch observation.Batch, row observationview.Row, selector labels.Selector) bool {
	for _, pod := range batch.Pods {
		kind, name := pod.Context.WorkloadKind, pod.Context.WorkloadName
		if name == "" {
			kind, name = "Pod", pod.Name
		}
		if pod.Namespace == row.Namespace && kind == row.Kind && name == row.Name && labelMatches(selector, pod.Context.Labels) {
			return true
		}
	}
	return false
}
