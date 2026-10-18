package kube

import "context"

// ObjectAccess describes acquisition that also needs the original reader's
// authority. The collector's service identity alone cannot authorise disclosure.
type ObjectAccess struct{ Group, Resource, Subresource, Namespace, Name, Verb string }
type ObjectAuthorizer func(context.Context, ObjectAccess) error

// Preserve the volume adapter's public contract while sharing object checks.
type VolumeAccess = ObjectAccess
type VolumeAuthorizer = ObjectAuthorizer
