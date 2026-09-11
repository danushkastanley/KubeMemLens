package tui

import "fmt"

func podKey(namespace, name string) string {
	return "pod/" + namespace + "/" + name
}

func entityKey(ref entityRef) string {
	switch ref.kind {
	case entityNode:
		return "node/" + ref.nodeName
	case entityNamespace:
		return "namespace/" + ref.namespace
	case entityWorkload:
		return "workload/" + ref.namespace + "/" + ref.workloadKind + "/" + ref.name
	case entityPod:
		return podKey(ref.namespace, ref.podName)
	case entityContainer:
		return "container/" + ref.namespace + "/" + ref.podName + "/" + ref.containerName
	default:
		return ""
	}
}

func (m appModel) selectedEntityKey() string {
	keys := m.visibleEntityKeys()
	selected := m.viewports[m.view].selected
	if selected < 0 || selected >= len(keys) {
		return ""
	}
	return keys[selected]
}

func (m appModel) visibleEntityKeys() []string {
	if m.restricted() {
		rows := m.visibleObservationRows()
		keys := make([]string, len(rows))
		for i, row := range rows {
			keys[i] = row.Key()
		}
		return keys
	}
	switch m.view {
	case viewNodes:
		items := m.visibleNodes()
		keys := make([]string, len(items))
		for index, item := range items {
			keys[index] = "node/" + item.name
		}
		return keys
	case viewNamespaces:
		items := m.visibleNamespaces()
		keys := make([]string, len(items))
		for index, item := range items {
			keys[index] = "namespace/" + item.Namespace
		}
		return keys
	case viewWorkloads:
		items := m.visibleWorkloads()
		keys := make([]string, len(items))
		for index, item := range items {
			keys[index] = fmt.Sprintf("workload/%s/%s/%s", item.Namespace, item.Kind, item.Name)
		}
		return keys
	case viewPods:
		items := m.visiblePods()
		keys := make([]string, len(items))
		for index, item := range items {
			keys[index] = podKey(item.Namespace, item.PodName)
		}
		return keys
	case viewContainers:
		items := m.visibleContainers()
		keys := make([]string, len(items))
		for index, item := range items {
			keys[index] = "container/" + item.Namespace + "/" + item.PodName + "/" + item.ContainerName
		}
		return keys
	default:
		return nil
	}
}
