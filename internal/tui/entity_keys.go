package tui

import "fmt"

func podKey(namespace, name string) string {
	return "pod/" + namespace + "/" + name
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
