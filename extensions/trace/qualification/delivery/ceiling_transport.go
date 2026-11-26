package delivery

import (
	"context"
	"net/http"
	"time"
)

// ConnectCeiling activates only an existing approved file admission. It shares
// normal transport authentication, origin restrictions and deadlines, while its
// separate parser retains nullable counters on early ceiling termination.
func ConnectCeiling(ctx context.Context, connection Connection, expected Expectation) (CeilingResult, error) {
	return connectCeiling(ctx, connection, expected, nil)
}

// ConnectCeilingReady announces readiness only after the metadata matches the
// authenticated active admission. The bounded controller starts its burst then.
// ready must be a bounded write; the caller retains its process watchdog.
func ConnectCeilingReady(ctx context.Context, connection Connection, expected Expectation, ready func() error) (CeilingResult, error) {
	if ready == nil {
		return CeilingResult{}, ErrObservation
	}
	return connectCeiling(ctx, connection, expected, ready)
}

func connectCeiling(ctx context.Context, connection Connection, expected Expectation, ready func() error) (CeilingResult, error) {
	if !validExpectation(expected) {
		return CeilingResult{}, ErrObservation
	}
	client, err := newHTTPClient(connection)
	if err != nil {
		return CeilingResult{}, err
	}
	defer client.CloseIdleConnections()
	lifetime, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	return readCeilingReadyWithClient(lifetime, client, connection.Server, connection.Token, expected, time.Now, ready)
}

func readCeilingWithClient(ctx context.Context, client *http.Client, server, token string, expected Expectation, now func() time.Time) (CeilingResult, error) {
	return readCeilingReadyWithClient(ctx, client, server, token, expected, now, nil)
}

func readCeilingReadyWithClient(ctx context.Context, client *http.Client, server, token string, expected Expectation, now func() time.Time, ready func() error) (CeilingResult, error) {
	body, active, err := openAdmittedStream(ctx, client, server, token, expected)
	if err != nil {
		return CeilingResult{}, err
	}
	defer body.Close()
	return observeCeiling(body, expected, now, active, ready)
}
