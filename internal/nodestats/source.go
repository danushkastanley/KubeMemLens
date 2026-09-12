// Package nodestats reads only a scheduled Node's bounded kubelet Summary data.
package nodestats

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/rest"
)

type NodeStatsSource interface {
	Read(context.Context) (nodecontext.Observation, error)
}

type Options struct {
	VolumeStats VolumeStatsMode
	NodeName    string
	CAFile      string
	TokenFile   string
	Timeout     time.Duration
	Now         func() time.Time
	Telemetry   *Telemetry
}

type Source struct {
	api         *http.Client
	base        string
	opts        Options
	mu          sync.Mutex
	lastAttempt time.Time
}

func New(config *rest.Config, opts Options) (*Source, error) {
	if (opts.VolumeStats != VolumeStatsDisabled && opts.VolumeStats != VolumeStatsEnabled) || config == nil || config.TLSClientConfig.Insecure || len(validation.IsDNS1123Subdomain(opts.NodeName)) != 0 || opts.CAFile == "" || opts.TokenFile == "" {
		return nil, &Error{Reason: nodecontext.InvalidTarget}
	}
	base, err := url.Parse(config.Host)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, &Error{Reason: nodecontext.InvalidTarget}
	}
	if opts.Timeout < 0 || opts.Timeout > nodecontext.RequestTimeout {
		return nil, &Error{Reason: nodecontext.InvalidTarget}
	}
	if opts.Timeout == 0 {
		opts.Timeout = nodecontext.RequestTimeout
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Telemetry == nil {
		opts.Telemetry = &Telemetry{}
	}
	copied := rest.CopyConfig(config)
	copied.DisableCompression = true
	copied.Proxy = func(*http.Request) (*url.URL, error) { return nil, nil }
	client, err := rest.HTTPClientFor(copied)
	if err != nil {
		return nil, &Error{Reason: nodecontext.Authentication}
	}
	return &Source{api: &http.Client{Transport: client.Transport, CheckRedirect: rejectRedirect}, base: strings.TrimRight(config.Host, "/"), opts: opts}, nil
}

func (s *Source) Close() { s.api.CloseIdleConnections() }

func (s *Source) Read(ctx context.Context) (nodecontext.Observation, error) {
	sample, err := s.ReadSample(ctx)
	return sample.Node, err
}

func (s *Source) ReadSample(ctx context.Context) (report Sample, err error) {
	if !s.mu.TryLock() {
		return report, &Error{Reason: nodecontext.Throttled}
	}
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return report, transportError(err)
	}
	attemptAt := s.opts.Now()
	now := attemptAt.UTC()
	// The scheduler may jitter by ten percent, but callers cannot bypass its
	// minimum spacing by issuing repeated or concurrent Read calls.
	if !s.lastAttempt.IsZero() && attemptAt.Sub(s.lastAttempt) < nodecontext.CollectionInterval*9/10 {
		return report, &Error{Reason: nodecontext.Throttled}
	}
	s.lastAttempt = attemptAt
	started := time.Now()
	bytesRead := 0
	defer func() { s.opts.Telemetry.record(err, time.Since(started), bytesRead) }()
	ctx, cancel := context.WithTimeout(ctx, s.opts.Timeout)
	defer cancel()
	identity, err := s.identity(ctx)
	if ctx.Err() != nil {
		return report, transportError(ctx.Err())
	}
	if err != nil {
		return report, err
	}
	data, err := getBytes(ctx, s.api, s.base+"/api/v1/nodes/"+url.PathEscape(s.opts.NodeName), "", maxNodeBytes)
	if err != nil {
		return report, err
	}
	node, err := decodeNode(ctx, data, s.opts.NodeName, now)
	if ctx.Err() != nil {
		return report, transportError(ctx.Err())
	}
	if err != nil {
		return report, err
	}
	if node.uid != identity.uid {
		return report, &Error{Reason: nodecontext.InvalidTarget}
	}
	client, token, err := s.kubeletClient()
	if err != nil {
		return report, err
	}
	defer client.CloseIdleConnections()
	data, err = getBytes(ctx, client, node.endpoint(), token, nodecontext.MaxSummaryBytes)
	bytesRead = len(data)
	if err != nil {
		return report, err
	}
	parsed, err := decodeSummary(ctx, data, node.name, now)
	if ctx.Err() != nil {
		return report, transportError(ctx.Err())
	}
	if err != nil {
		return report, &Error{Reason: nodecontext.InvalidResponse}
	}
	report.Node = observation(node, parsed, s.opts.Now().UTC())
	encoded, err := json.Marshal(report.Node)
	if err != nil || len(encoded) > nodecontext.MaxObservationBytes {
		return Sample{}, &Error{Reason: nodecontext.ResponseTooLarge}
	}
	if s.opts.VolumeStats == VolumeStatsEnabled {
		report.Volumes, err = s.volumeBatch(ctx, data, node.name, node.uid, report.Node.ReportedAt)
		if ctx.Err() != nil {
			return Sample{}, transportError(ctx.Err())
		}
		if err != nil {
			return Sample{}, err
		}
	}
	return report, nil
}

func observation(node nodeTarget, parsed summary, received time.Time) nodecontext.Observation {
	report := nodecontext.Observation{NodeName: node.name, NodeUID: node.uid, ReportedAt: received,
		Availability: capability.Available, Context: &node.context, Stats: &parsed.stats,
		Evidence: capability.Envelope{Source: nodecontext.Source, APIVersion: "v1alpha1", ReceivedAt: received,
			Scope: capability.NodeScope, Freshness: capability.Fresh, Completeness: capability.Complete,
			Stability: capability.ImplementationSpecific, Caveats: []string{"stats-provenance-unknown"}}}
	times := sampleTimes(parsed.stats)
	if !hasMeasurements(parsed.stats) {
		report.Availability, report.Reason, report.Stats = capability.Unreported, nodecontext.NotObserved, nil
		report.Evidence.Freshness = capability.UnknownFreshness
		times = nil
	}
	for _, at := range times {
		if report.Evidence.CapturedAt.IsZero() || at.Before(report.Evidence.CapturedAt) {
			report.Evidence.CapturedAt = at
		}
	}
	if parsed.partial || node.context.CapacityBytes == nil || node.context.AllocatableBytes == nil {
		report.Evidence.Completeness = capability.Partial
		report.Evidence.Caveats = append(report.Evidence.Caveats, "optional-fields-unreported")
	}
	if len(times) > 0 && received.Sub(report.Evidence.CapturedAt) > nodecontext.StaleAfter {
		report.Evidence.Freshness = capability.Stale
	}
	return report
}

func reasonOf(err error) nodecontext.Reason {
	var failure *Error
	if errors.As(err, &failure) {
		return failure.Reason
	}
	return nodecontext.InvalidResponse
}
