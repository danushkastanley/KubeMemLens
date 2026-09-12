# ADR 0005: Negotiate resource snapshot schemas

Status: accepted for feature integration; no release promotion implied.

## Context

Pod memory budgets and resize observations add structured metadata to snapshots.
Released clients and collectors reject unknown JSON fields. Optional fields
alone would therefore break rolling upgrades, live readers and incident replay.

## Decision

Clients advertise their maximum supported snapshot schema in
`X-KubeMemLens-Snapshot-Schema`. An absent header selects schema 1. A positive
version is clamped to the server's supported maximum, currently 2. Invalid
values fail before snapshot reads. Authorisation still precedes data access.

Schema 2 adds bounded, typed resource values and resize observations. Schema 1
projects out this extension while preserving identities, memory measurements,
source times and pagination. Projection copies data rather than modifying the
collector store. New agents use the schema returned with the authenticated
ingestion epoch; an epoch change renegotiates the schema without discarding the
original collected metadata. Sequence and replay protections are unchanged.

Incident and explanation documents use schema 2 when resource context is
present. Legacy-only documents remain schema 1. Capture also offers explicit
`--schema-version=1`: it removes unsupported context and records a partial-data
caveat. Replay retains strict JSON decoding, size limits and schema validation.
Recommendations retain their existing schema because they add no resource fields.

## Alternatives

Adding optional fields under schema 1 breaks released strict decoders. Relaxing
those decoders cannot repair already deployed binaries. A new Kubernetes API
version would multiply discovery and authorisation routes for a representation
change that does not add a resource or permission.

## Consequences

Mixed versions remain usable, but old readers cannot display Pod budgets or
resize state. Operators need an upgraded reader and agent to see that evidence.
A schema-1 export records the omitted context explicitly. Current replay displays
bundle caveats as quoted text; older replay binaries may not display them. No new Kubernetes
permission, endpoint, label collection or free-form status text is introduced.

## Rollout and rollback

Upgrade the collector, agents and CLI in the normal Helm lifecycle. Both upgrade
orders negotiate a common schema. Roll back binaries/chart normally; agents
renegotiate on the replacement collector epoch, including when a legacy strict
decoder rejects the newer JSON before it can check the epoch. Export schema-1 incidents before
using a legacy replay binary. Schema-2 files require a current reader and remain
local artefacts; there is no persisted database migration.
