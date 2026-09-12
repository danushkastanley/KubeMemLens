package api

// VolumeSnapshotSchemaVersion adds the separately bounded private volume batch.
const VolumeSnapshotSchemaVersion = 4

// VolumeHealthSnapshotSchemaVersion adds explicitly historical health evidence.
// Schema 4 readers receive current health only and keep their strict decoder.
const VolumeHealthSnapshotSchemaVersion = 5
