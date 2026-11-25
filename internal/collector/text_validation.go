package collector

import (
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

// validateSnapshotText rejects control and format characters and invalid UTF-8 in every
// string an agent supplies. These values reach metric labels, log lines and
// the terminal, where a newline or an escape sequence would forge output.
func validateSnapshotText(snapshot api.AgentSnapshot) error {
	environment := snapshot.Environment
	if err := printableFields(
		textField{"nodeName", snapshot.NodeName},
		textField{"environment.cgroupVersion", environment.CgroupVersion},
		textField{"environment.cgroupDriver", environment.CgroupDriver},
		textField{"environment.memoryPressureStatus", environment.MemoryPressureStatus},
	); err != nil {
		return err
	}
	for i, runtime := range environment.ContainerRuntimes {
		if err := printableFields(textField{fmt.Sprintf("environment.containerRuntimes[%d]", i), runtime}); err != nil {
			return err
		}
	}
	for i, container := range snapshot.Containers {
		context := container.Context
		fields := []textField{
			{"namespace", container.Namespace}, {"podName", container.PodName}, {"podUID", container.PodUID},
			{"containerName", container.ContainerName}, {"containerID", container.ContainerID}, {"cgroupPath", container.CgroupPath},
			{"context.qosClass", context.QoSClass}, {"context.lastTerminationReason", context.LastTerminationReason},
			{"context.podPhase", context.PodPhase}, {"context.ownerKind", context.OwnerKind}, {"context.ownerName", context.OwnerName},
			{"context.workloadKind", context.WorkloadKind}, {"context.workloadName", context.WorkloadName},
			{"context.nodeMemoryPressure", context.NodeMemoryPressure}, {"context.runtimeClassName", context.RuntimeClassName},
		}
		for key, value := range context.Labels {
			fields = append(fields, textField{"context.labels key", key}, textField{"context.labels value", value})
		}
		if err := printableFields(fields...); err != nil {
			return fmt.Errorf("containers[%d].%w", i, err)
		}
	}
	return nil
}

type textField struct{ name, value string }

func printableFields(fields ...textField) error {
	for _, field := range fields {
		if !utf8.ValidString(field.value) {
			return fmt.Errorf("%s is not valid UTF-8", field.name)
		}
		for _, r := range field.value {
			// Format characters include the bidirectional overrides and
			// zero-width marks, which reorder or hide text in a terminal.
			if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
				return fmt.Errorf("%s contains a control character", field.name)
			}
		}
	}
	return nil
}
