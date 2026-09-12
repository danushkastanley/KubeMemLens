package incident

// VolumeSchemaVersion reserves volume-enriched Pod incidents. Existing deep,
// restricted and Node document versions keep their meanings. Capture and replay
// must be implemented before this version is accepted by the document reader.
const VolumeSchemaVersion = 5
