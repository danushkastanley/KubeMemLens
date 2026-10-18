package profiler

// Lines renders guidance without interpolating workload metadata into commands.
func Lines(results []Result) []string {
	lines := []string{"Profiler handoffs (commands are never executed):"}
	for _, r := range results {
		lines = append(lines, "", r.Namespace+"/"+r.Pod+"/"+r.Container+" ["+r.State+"]", r.Reason)
		if r.Handoff == nil {
			continue
		}
		h := r.Handoff
		lines = append(lines, "Basis: "+h.Basis, "Prerequisites:")
		lines = append(lines, h.Prerequisites...)
		lines = append(lines, "Risks:")
		lines = append(lines, h.Risks...)
		lines = append(lines, "Reviewable local commands:")
		lines = append(lines, h.Commands...)
		lines = append(lines, "Verify:")
		lines = append(lines, h.Verification...)
		lines = append(lines, h.References...)
	}
	return lines
}
