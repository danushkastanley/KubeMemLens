.schemaVersion == 3 and (.finding.severity | length > 0) and
(.finding.confidence | length > 0) and (.finding.caveats | length > 0) and
(.finding.evidenceWindow | type == "object")
