package volumecontext

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	"k8s.io/apimachinery/pkg/util/validation"
)

var ErrInvalid = errors.New("invalid or unbounded volume context")
var ErrScope = errors.New("volume context does not match authorised scope")

func validName(value string) bool {
	return len(value) <= MaxNameBytes && len(validation.IsDNS1123Subdomain(value)) == 0
}

func validUID(value string) bool {
	return value != "" && validText(value, MaxUIDBytes) && !strings.ContainsAny(value, " /\\")
}

func validText(value string, limit int) bool {
	return len(value) <= limit && utf8.ValidString(value) && strings.IndexFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || unicode.In(r, unicode.Cf)
	}) == -1
}

func validateScope(scope PodScope, now time.Time) error {
	if len(validation.IsDNS1123Label(scope.Namespace)) != 0 || !validName(scope.PodName) || !validUID(scope.PodUID) ||
		!validName(scope.NodeName) || !validUID(scope.NodeUID) || scope.CreatedAt.IsZero() || scope.CreatedAt.After(now.Add(FutureSkew)) {
		return ErrInvalid
	}
	return nil
}

func validateBinding(b Binding, now time.Time) error {
	c := b.Configuration
	if !validName(b.VolumeName) || len(b.VolumeName) > 63 || c.MountCount < 0 || c.MountCount > MaxMountsPerVolume ||
		c.ReadOnlyMountCount < 0 || c.ReadOnlyMountCount > c.MountCount || (b.Driver != "" && !validName(b.Driver)) {
		return ErrInvalid
	}
	switch c.Kind {
	case PersistentClaim, EphemeralClaim:
		if b.ClaimAvailability != volumehealth.Reported {
			return validateUnavailableClaim(b)
		}
		if !validName(b.PVCName) || !validUID(b.PVCUID) || b.PVCCreatedAt.IsZero() || b.PVCCreatedAt.After(now.Add(FutureSkew)) {
			return ErrInvalid
		}
	case InlineCSI, EmptyDir, Other:
		if b.PVCName != "" || b.PVCUID != "" || !b.PVCCreatedAt.IsZero() || b.ClaimAvailability != "" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	if c.Kind != EmptyDir && (c.MemoryBacked || c.SizeLimitBytes != nil) {
		return ErrInvalid
	}
	if c.Kind == InlineCSI && b.Driver == "" {
		return ErrInvalid
	}
	if (c.Kind == EmptyDir || c.Kind == Other) && b.Driver != "" {
		return ErrInvalid
	}
	return nil
}

func validateUnavailableClaim(b Binding) error {
	if b.PVCName != "" || b.PVCUID != "" || !b.PVCCreatedAt.IsZero() || b.Driver != "" || b.Configuration.MemoryBacked || b.Configuration.SizeLimitBytes != nil {
		return ErrInvalid
	}
	switch b.ClaimAvailability {
	case volumehealth.Forbidden, volumehealth.Unavailable, volumehealth.Unreported:
		return nil
	default:
		return ErrInvalid
	}
}

func validateFilesystem(f Filesystem, now time.Time) error {
	if f.CapturedAt.IsZero() || f.CapturedAt.After(now.Add(FutureSkew)) ||
		(f.CapacityBytes == nil && f.UsedBytes == nil && f.AvailableBytes == nil && f.Inodes == nil && f.InodesUsed == nil && f.InodesFree == nil) {
		return ErrInvalid
	}
	return nil
}

func validateHealth(h volumehealth.Observation) error {
	if h.TransitionAt.After(h.ObservedAt.Add(FutureSkew)) {
		return ErrInvalid
	}
	switch h.Source {
	case volumehealth.PodSource, volumehealth.ControllerSource:
		if h.Scope != volumehealth.VolumeScope {
			return ErrInvalid
		}
	case volumehealth.BackendSource:
		if h.Scope != volumehealth.BackendScope {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	switch h.Availability {
	case volumehealth.Reported:
		if h.Reason != "" {
			return ErrInvalid
		}
	case volumehealth.Unreported, volumehealth.Disabled, volumehealth.Unsupported, volumehealth.Forbidden, volumehealth.Unavailable, volumehealth.Unknown:
		if len(h.Conditions) != 0 {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	switch h.Reason {
	case "", volumehealth.NoReport, volumehealth.AccessDenied, volumehealth.ReadFailed, volumehealth.InvalidResponse, volumehealth.BindingUnavailable, volumehealth.NotCSIVolume, volumehealth.NotScheduled:
	default:
		return ErrInvalid
	}
	if len(h.Conditions) > volumehealth.MaxConditions {
		return ErrInvalid
	}
	for _, condition := range h.Conditions {
		if condition.Status == "" || !validText(string(condition.Status), MaxStatusBytes) || !validText(condition.Reason, MaxReasonBytes) ||
			!validText(condition.Message, 1024) || !validText(condition.AccessMode, 64) || !validText(condition.VolumeMode, 64) {
			return ErrInvalid
		}
		if condition.TransitionAt.After(h.ObservedAt.Add(FutureSkew)) {
			return ErrInvalid
		}
	}
	return nil
}
