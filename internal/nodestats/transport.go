package nodestats

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

type Error struct {
	Reason nodecontext.Reason
	cause  error
}

// Error deliberately omits the cause, which can contain URLs or file paths.
func (e *Error) Error() string { return "node-context read failed: " + string(e.Reason) }
func (e *Error) Unwrap() error { return e.cause }

func transportError(err error) *Error {
	var authority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var certificate x509.CertificateInvalidError
	var timed net.Error
	reason := nodecontext.Unreachable
	switch {
	case errors.As(err, &authority), errors.As(err, &hostname), errors.As(err, &certificate):
		reason = nodecontext.UntrustedTLS
	case errors.Is(err, context.DeadlineExceeded):
		reason = nodecontext.TimedOut
	case errors.As(err, &timed) && timed.Timeout():
		reason = nodecontext.TimedOut
	case errors.Is(err, context.Canceled):
		reason = nodecontext.SourceUnavailable
	}
	return &Error{Reason: reason, cause: err}
}

func readFileBounded(path string, maximum int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maximum {
		return nil, errJSON
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(data)) > maximum {
		return nil, errJSON
	}
	return data, nil
}

func (s *Source) kubeletClient() (*http.Client, string, error) {
	ca, err := readFileBounded(s.opts.CAFile, 1<<20)
	if err != nil {
		return nil, "", &Error{Reason: nodecontext.UntrustedTLS}
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, "", &Error{Reason: nodecontext.UntrustedTLS}
	}
	token, err := readFileBounded(s.opts.TokenFile, 16<<10)
	if err != nil {
		return nil, "", &Error{Reason: nodecontext.Authentication}
	}
	bearer := strings.TrimSpace(string(token))
	if bearer == "" || strings.ContainsAny(bearer, " \t\r\n") {
		return nil, "", &Error{Reason: nodecontext.Authentication}
	}
	transport := &http.Transport{
		Proxy: nil, DisableCompression: true, MaxConnsPerHost: 1,
		TLSClientConfig:     &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: s.opts.Timeout, ResponseHeaderTimeout: s.opts.Timeout,
	}
	return &http.Client{Transport: transport, CheckRedirect: rejectRedirect}, bearer, nil
}

func rejectRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func getBytes(ctx context.Context, client *http.Client, url, bearer string, maximum int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, &Error{Reason: nodecontext.InvalidTarget}
	}
	request.Header.Set("Accept", "application/json")
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	return readResponse(client, request, maximum, http.StatusOK)
}

func readResponse(client *http.Client, request *http.Request, maximum int64, expectedStatus int) ([]byte, error) {
	response, err := client.Do(request)
	if err != nil {
		return nil, transportError(err)
	}
	defer response.Body.Close()
	if response.StatusCode != expectedStatus {
		return nil, statusError(response.StatusCode)
	}
	encoding := strings.TrimSpace(response.Header.Get("Content-Encoding"))
	media, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if mediaErr != nil || media != "application/json" || (encoding != "" && !strings.EqualFold(encoding, "identity")) {
		return nil, &Error{Reason: nodecontext.InvalidResponse}
	}
	if response.ContentLength > maximum {
		return nil, &Error{Reason: nodecontext.ResponseTooLarge}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return data, &Error{Reason: nodecontext.InvalidResponse, cause: err}
	}
	if err != nil {
		return data, transportError(err)
	}
	if int64(len(data)) > maximum {
		return data, &Error{Reason: nodecontext.ResponseTooLarge}
	}
	return data, nil
}

func statusError(status int) *Error {
	reason := nodecontext.SourceUnavailable
	switch status {
	case http.StatusUnauthorized:
		reason = nodecontext.Authentication
	case http.StatusForbidden:
		reason = nodecontext.Forbidden
	case http.StatusTooManyRequests:
		reason = nodecontext.Throttled
	case http.StatusNotFound:
		reason = nodecontext.Unsupported
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		reason = nodecontext.InvalidTarget
	}
	return &Error{Reason: reason}
}
