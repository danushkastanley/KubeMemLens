package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/buildinfo"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/spf13/cobra"
)

type doctorReport struct {
	Build      buildinfo.Info           `json:"build"`
	Connection string                   `json:"connection"`
	Checks     []doctorCheck            `json:"checks"`
	Nodes      []api.NodeSnapshotStatus `json:"nodes,omitempty"`
	Mapping    doctorMapping            `json:"mapping"`
}

type doctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

type doctorMapping struct {
	Containers int     `json:"containers"`
	Mapped     int     `json:"mapped"`
	Unmapped   int     `json:"unmapped"`
	Coverage   float64 `json:"coverage"`
}

func newDoctorCommand(collectorOptions collectorOptionsProvider) *cobra.Command {
	var output string
	var strict bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose KubeMemLens connectivity, freshness, and mapping coverage",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if output != "text" && output != "json" {
				return fmt.Errorf("invalid output %q, want text or json", output)
			}
			opts, err := withReadScope(collectorOptions(), "", true)
			if err != nil {
				return err
			}
			report, checkErr := buildDoctorReport(cmd.Context(), opts)
			if output == "json" {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(report); err != nil {
					return err
				}
			} else {
				fmt.Fprint(cmd.OutOrStdout(), renderDoctorReport(report))
			}
			if checkErr != nil || report.shouldFail(strict) {
				return errors.New("doctor checks failed")
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "text", "output format: text or json")
	cmd.Flags().BoolVar(&strict, "strict", false, "return a failure when any warning is present")
	return cmd
}

func buildDoctorReport(ctx context.Context, opts client.Options) (doctorReport, error) {
	report := doctorReport{
		Build:      buildinfo.Current(runtime.Version(), runtime.GOOS, runtime.GOARCH),
		Connection: client.Describe(opts),
		Checks: []doctorCheck{{
			Name:    "build",
			Status:  "pass",
			Summary: buildinfo.Current(runtime.Version(), runtime.GOOS, runtime.GOARCH).String(),
		}},
	}
	reader, description, err := client.NewSnapshotReader(ctx, opts)
	report.Connection = description
	if err != nil {
		report.addCheck("connection", "fail", client.ConnectionError(opts, description, err).Error())
		return report, err
	}
	if err := reader.Health(ctx); err != nil {
		report.addCheck("connection", "fail", client.ConnectionError(opts, description, err).Error())
		return report, err
	}
	report.addCheck("connection", "pass", "collector read endpoint is healthy through "+description)

	nodes, err := reader.Nodes(ctx)
	if err != nil {
		report.addCheck("agent coverage", "fail", err.Error())
		return report, err
	}
	report.Nodes = nodes
	staleNodes := 0
	for _, node := range nodes {
		if node.Stale {
			staleNodes++
		}
	}
	switch {
	case len(nodes) == 0:
		report.addCheck("agent coverage", "fail", "collector has not received a snapshot from any node")
	case staleNodes > 0:
		report.addCheck("agent coverage", "fail", fmt.Sprintf("%d of %d reporting nodes have stale snapshots", staleNodes, len(nodes)))
	default:
		report.addCheck("agent coverage", "pass", fmt.Sprintf("%d node snapshots are fresh", len(nodes)))
	}
	report.addEnvironmentChecks(nodes)

	containers, err := reader.Containers(ctx)
	if err != nil {
		report.addCheck("mapping coverage", "fail", err.Error())
		return report, err
	}
	for _, container := range containers {
		if container.Namespace != "" && container.PodName != "" && container.ContainerName != "" {
			report.Mapping.Mapped++
		}
	}
	report.Mapping.Containers = len(containers)
	report.Mapping.Unmapped = len(containers) - report.Mapping.Mapped
	if len(containers) > 0 {
		report.Mapping.Coverage = float64(report.Mapping.Mapped) / float64(len(containers))
	}
	switch {
	case len(containers) == 0:
		report.addCheck("mapping coverage", "warn", "no current container cgroups are available to assess")
	case report.Mapping.Unmapped > 0:
		report.addCheck("mapping coverage", "warn", fmt.Sprintf("mapped %d of %d container cgroups (%.1f%%)", report.Mapping.Mapped, len(containers), report.Mapping.Coverage*100))
	default:
		report.addCheck("mapping coverage", "pass", fmt.Sprintf("mapped all %d container cgroups", len(containers)))
	}

	debug, err := reader.DebugStore(ctx)
	if err != nil {
		report.addCheck("store consistency", "fail", err.Error())
		return report, err
	}
	if debug.TotalContainers != len(containers) {
		report.addCheck("store consistency", "fail", fmt.Sprintf("debug store reports %d containers but list returned %d", debug.TotalContainers, len(containers)))
	} else {
		report.addCheck("store consistency", "pass", fmt.Sprintf("collector reports %d containers, %d Pods, and %d namespaces", debug.TotalContainers, debug.Pods, debug.Namespaces))
	}
	capacitySummary := fmt.Sprintf("%d/%d node records, %d/%d containers, and %d-byte response ceiling",
		debug.NodeRecords, debug.MaxNodes, debug.TotalContainers, debug.MaxContainers, debug.MaxResponseBytes)
	switch {
	case debug.MaxNodes <= 0 || debug.MaxContainers <= 0 || debug.MaxResponseBytes <= 0:
		report.addCheck("collector bounds", "fail", "collector did not report valid storage and response bounds")
	case debug.NodeRecords >= debug.MaxNodes || debug.TotalContainers >= debug.MaxContainers:
		report.addCheck("collector bounds", "warn", "collector is at a configured capacity ceiling: "+capacitySummary)
	case debug.NodeRecords*10 >= debug.MaxNodes*9 || debug.TotalContainers*10 >= debug.MaxContainers*9:
		report.addCheck("collector bounds", "warn", "collector is above 90% of a configured capacity ceiling: "+capacitySummary)
	default:
		report.addCheck("collector bounds", "pass", capacitySummary)
	}
	return report, nil
}

func (r *doctorReport) addEnvironmentChecks(nodes []api.NodeSnapshotStatus) {
	if len(nodes) == 0 {
		return
	}
	missingCgroup := 0
	unsupportedCgroup := 0
	readErrors := 0
	runtimes := map[string]struct{}{}
	unknownRuntime := false
	missingNodeContext := 0
	memoryPressureNodes := 0
	unknownPressureNodes := 0
	missingWorkloadContext := 0
	workloadContextErrors := 0
	for _, node := range nodes {
		switch node.Environment.CgroupVersion {
		case "v2":
		case "":
			missingCgroup++
		default:
			unsupportedCgroup++
		}
		readErrors += node.Environment.CgroupReadErrors
		if len(node.Environment.ContainerRuntimes) == 0 {
			unknownRuntime = true
		}
		for _, runtimeName := range node.Environment.ContainerRuntimes {
			runtimes[runtimeName] = struct{}{}
			if runtimeName == "" || runtimeName == "unknown" {
				unknownRuntime = true
			}
		}
		if !node.Environment.NodeContextAvailable {
			missingNodeContext++
		} else if node.Environment.MemoryPressureStatus == "True" {
			memoryPressureNodes++
		} else if node.Environment.MemoryPressureStatus != "False" {
			unknownPressureNodes++
		}
		if !node.Environment.WorkloadContextAvailable {
			missingWorkloadContext++
		}
		workloadContextErrors += node.Environment.WorkloadContextErrors
	}
	switch {
	case unsupportedCgroup > 0:
		r.addCheck("cgroup mode", "fail", fmt.Sprintf("%d reporting nodes are not using supported cgroup v2", unsupportedCgroup))
	case missingCgroup > 0:
		r.addCheck("cgroup mode", "warn", fmt.Sprintf("%d reporting nodes did not report their cgroup version", missingCgroup))
	default:
		r.addCheck("cgroup mode", "pass", "all reporting nodes use cgroup v2")
	}
	if readErrors > 0 {
		r.addCheck("cgroup access", "fail", fmt.Sprintf("agents reported %d cgroup read or walk errors", readErrors))
	} else if missingCgroup > 0 {
		r.addCheck("cgroup access", "warn", "cgroup read access was not reported by every node")
	} else {
		r.addCheck("cgroup access", "pass", "agents reported no cgroup read or walk errors")
	}
	runtimeList := sortedMapKeys(runtimes)
	if unknownRuntime {
		r.addCheck("runtime layout", "warn", "one or more container runtimes could not be identified")
	} else {
		r.addCheck("runtime layout", "pass", "identified "+strings.Join(runtimeList, ", "))
	}
	switch {
	case missingNodeContext > 0:
		r.addCheck("node pressure", "warn", fmt.Sprintf("%d agents could not read their Kubernetes Node condition; check nodes/get RBAC", missingNodeContext))
	case memoryPressureNodes > 0:
		r.addCheck("node pressure", "warn", fmt.Sprintf("%d reporting nodes have MemoryPressure=True", memoryPressureNodes))
	case unknownPressureNodes > 0:
		r.addCheck("node pressure", "warn", fmt.Sprintf("%d reporting nodes have unknown MemoryPressure state", unknownPressureNodes))
	default:
		r.addCheck("node pressure", "pass", "all reporting nodes have MemoryPressure=False")
	}
	switch {
	case workloadContextErrors > 0:
		r.addCheck("workload context", "warn", fmt.Sprintf("agents reported %d top-level owner resolution errors; check replicasets/get and jobs/get RBAC", workloadContextErrors))
	case missingWorkloadContext > 0:
		r.addCheck("workload context", "warn", fmt.Sprintf("%d agents did not report top-level workload context", missingWorkloadContext))
	default:
		r.addCheck("workload context", "pass", "top-level workload owner resolution is available")
	}
}

func (r *doctorReport) addCheck(name, status, summary string) {
	r.Checks = append(r.Checks, doctorCheck{Name: name, Status: status, Summary: summary})
}

func (r doctorReport) shouldFail(strict bool) bool {
	for _, check := range r.Checks {
		if check.Status == "fail" || (strict && check.Status == "warn") {
			return true
		}
	}
	return false
}

func renderDoctorReport(report doctorReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "KubeMemLens doctor\n\nConnection: %s\n\nChecks:\n", report.Connection)
	for _, check := range report.Checks {
		fmt.Fprintf(&b, "  %-4s  %-18s %s\n", strings.ToUpper(check.Status), check.Name, singleLine(check.Summary))
	}
	if len(report.Nodes) > 0 {
		sort.Slice(report.Nodes, func(i, j int) bool { return report.Nodes[i].NodeName < report.Nodes[j].NodeName })
		b.WriteString("\nNodes:\n")
		for _, node := range report.Nodes {
			state := "fresh"
			if node.Stale {
				state = "stale"
			}
			runtimes := strings.Join(node.Environment.ContainerRuntimes, ",")
			if runtimes == "" {
				runtimes = "unknown"
			}
			cgroup := node.Environment.CgroupVersion
			if cgroup == "" {
				cgroup = "unknown"
			}
			driver := node.Environment.CgroupDriver
			if driver == "" {
				driver = "unknown"
			}
			pressure := node.Environment.MemoryPressureStatus
			if pressure == "" {
				pressure = "unknown"
			}
			fmt.Fprintf(&b, "  %-24s %-5s age=%s containers=%d cgroup=%s/%s runtime=%s pressure=%s readErrors=%d ownerErrors=%d\n",
				node.NodeName, state, doctorAge(node.CapturedAt), node.ContainerCount,
				cgroup, driver, runtimes, pressure, node.Environment.CgroupReadErrors, node.Environment.WorkloadContextErrors,
			)
		}
	}
	fmt.Fprintf(&b, "\nMapping: %d/%d (%.1f%%); unmapped=%d\n", report.Mapping.Mapped, report.Mapping.Containers, report.Mapping.Coverage*100, report.Mapping.Unmapped)
	return b.String()
}

func sortedMapKeys(values map[string]struct{}) []string {
	items := make([]string, 0, len(values))
	for value := range values {
		if value != "" {
			items = append(items, value)
		}
	}
	sort.Strings(items)
	return items
}

func doctorAge(capturedAt time.Time) string {
	if capturedAt.IsZero() {
		return "unknown"
	}
	age := time.Since(capturedAt)
	if age < 0 {
		age = 0
	}
	return age.Round(time.Second).String()
}

func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
