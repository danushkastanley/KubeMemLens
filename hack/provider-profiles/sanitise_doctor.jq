.connection = "redacted" |
.nodes = ((.nodes // []) | map(del(.nodeName))) |
.checks = ((.checks // []) | map(
  if .name == "workload context" then .name = "workload metadata" else . end
))
