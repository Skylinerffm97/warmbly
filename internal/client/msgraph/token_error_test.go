package msgraph

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
	"golang.org/x/oauth2"
)

// escapeRT stands in for the network behind the oauth2 transport. A token
// refresh that fails must short-circuit before Graph is dialled, so any call
// reaching here is itself a failure.
type escapeRT struct{ t *testing.T }

func (e escapeRT) RoundTrip(req *http.Request) (*http.Response, error) {
	e.t.Errorf("request escaped to %s: a failed token refresh must not reach the provider", req.URL)
	return nil, errors.New("unexpected network call")
}

// tokenRefusal boots a real client whose token endpoint answers every refresh
// with status and body. The mailbox's access token is already expired, so the
// first API call refreshes, is refused, and returns through the same path
// production takes: oauth2.Transport -> http.Client -> *url.Error.
func tokenRefusal(t *testing.T, status int, body string) (*Client, *int32) {
	t.Helper()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	return newRefusingClient(t, srv.URL), &calls
}

func newRefusingClient(t *testing.T, tokenURL string) *Client {
	t.Helper()

	c := &Client{Email: "sender@outlook.com"}
	cfg := oauth2.Config{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		// Pinned so the refusal is one request, not the auth-style probe's two.
		Endpoint: oauth2.Endpoint{TokenURL: tokenURL, AuthStyle: oauth2.AuthStyleInParams},
	}
	expired := &oauth2.Token{
		AccessToken:  "stale",
		RefreshToken: "refresh-token",
		Expiry:       time.Now().Add(-time.Hour),
	}
	if merr := c.Init(context.Background(), expired, cfg); merr != nil {
		t.Fatalf("Init: %v", merr.Message)
	}
	tr, ok := c.hc.Transport.(*oauth2.Transport)
	if !ok {
		t.Fatalf("Init did not build an oauth2 transport, got %T", c.hc.Transport)
	}
	tr.Base = escapeRT{t}
	return c
}

func mailErrorOf(t *testing.T, err error) *errx.MailError {
	t.Helper()
	if err == nil {
		t.Fatal("call succeeded, want a mail error")
	}
	var mailErr *errx.MailError
	if !errors.As(err, &mailErr) {
		t.Fatalf("error %v (%T) is not a *errx.MailError, so the sync loop cannot classify it", err, err)
	}
	return mailErr
}

const invalidGrantBody = `{"error":"invalid_grant","error_description":"AADSTS50173: The provided grant has expired due to it being revoked."}`

// The incident: a revoked grant reported as an unreachable server, with a
// promised retry that could never succeed. Every call shape has to agree,
// because the mailbox reaches the same dead grant through all of them.
func TestRevokedGrantIsAnAuthenticationErrorOnEveryCallPath(t *testing.T) {
	ctx := context.Background()

	t.Run("fetch message", func(t *testing.T) {
		c, calls := tokenRefusal(t, http.StatusBadRequest, invalidGrantBody)
		_, err := c.FetchMessage(ctx, FolderInbox, "message-id")
		if got := mailErrorOf(t, err).Code; got != errx.MailErrorCodeAuthenticationFailed {
			t.Errorf("code = %s, want %s", got, errx.MailErrorCodeAuthenticationFailed)
		}
		if atomic.LoadInt32(calls) == 0 {
			t.Error("the token endpoint was never called, so no refresh was exercised")
		}
	})

	t.Run("list messages", func(t *testing.T) {
		c, _ := tokenRefusal(t, http.StatusBadRequest, invalidGrantBody)
		_, _, err := c.ListMessagesSince(ctx, FolderInbox, time.Now().Add(-24*time.Hour), "", 10)
		if got := mailErrorOf(t, err).Code; got != errx.MailErrorCodeAuthenticationFailed {
			t.Errorf("code = %s, want %s", got, errx.MailErrorCodeAuthenticationFailed)
		}
	})

	t.Run("send", func(t *testing.T) {
		c, _ := tokenRefusal(t, http.StatusBadRequest, invalidGrantBody)
		_, err := c.SendMessage(ctx, "", []string{"partner@example.com"}, nil, nil,
			"<minted@outlook.com>", "subject", "body", "", nil, nil, nil)
		if got := mailErrorOf(t, err).Code; got != errx.MailErrorCodeAuthenticationFailed {
			t.Errorf("code = %s, want %s", got, errx.MailErrorCodeAuthenticationFailed)
		}
	})
}

// A revoked grant must ask the customer to reconnect rather than promise a
// retry, which is the difference between the two resolve methods.
func TestRevokedGrantAsksForReauthentication(t *testing.T) {
	c, _ := tokenRefusal(t, http.StatusBadRequest, invalidGrantBody)
	_, err := c.FetchMessage(context.Background(), FolderInbox, "message-id")
	mailErr := mailErrorOf(t, err)
	if mailErr.ResolveMethod != errx.MailErrorResolveMethodAuth {
		t.Errorf("resolve method = %s, want %s", mailErr.ResolveMethod, errx.MailErrorResolveMethodAuth)
	}
}

// The other half of the classification: a provider having a moment must not
// cost a customer their mailbox. Each of these reaches the consumer as a
// server error, which retries, instead of an auth error, which deactivates.
func TestTransientTokenRefusalsStayRetryable(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"throttled", http.StatusTooManyRequests, `{"error":"temporarily_unavailable"}`},
		{"token endpoint 500", http.StatusInternalServerError, `{}`},
		{"token endpoint 503", http.StatusServiceUnavailable, `{"error":"temporarily_unavailable"}`},
		{"provider server_error", http.StatusBadRequest, `{"error":"server_error","error_description":"try again"}`},
		{"unrecognised refusal", http.StatusBadRequest, `{"error":"something_new"}`},
		// An expired app secret answers this for every Outlook mailbox on the
		// install at once; deactivating them all would be the worse outage.
		{"our app credentials expired", http.StatusUnauthorized, `{"error":"invalid_client","error_description":"secret expired"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := tokenRefusal(t, tc.status, tc.body)
			_, err := c.FetchMessage(context.Background(), FolderInbox, "message-id")
			mailErr := mailErrorOf(t, err)
			if mailErr.Code != errx.MailErrorCodeServerUnreachable {
				t.Fatalf("code = %s, want %s: this deactivates the mailbox", mailErr.Code, errx.MailErrorCodeServerUnreachable)
			}
			if mailErr.ResolveMethod != errx.MailErrorResolveMethodRetry {
				t.Errorf("resolve method = %s, want %s", mailErr.ResolveMethod, errx.MailErrorResolveMethodRetry)
			}
		})
	}
}

// A token endpoint that cannot be reached at all is the case the old code was
// written for, and it still has to read as a transport failure. Port 1 is
// refused immediately and needs no DNS, so this stays hermetic and fast.
func TestUnreachableTokenEndpointIsAServerError(t *testing.T) {
	c := newRefusingClient(t, "http://127.0.0.1:1/token")
	_, err := c.FetchMessage(context.Background(), FolderInbox, "message-id")
	if got := mailErrorOf(t, err).Code; got != errx.MailErrorCodeServerUnreachable {
		t.Errorf("code = %s, want %s", got, errx.MailErrorCodeServerUnreachable)
	}
}
