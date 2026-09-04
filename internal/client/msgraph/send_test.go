package msgraph

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

// sendRT answers the three calls the draft send path makes and records the
// method, path and decoded MIME of each.
type sendRT struct {
	createStatus int
	sendStatus   int
	assignedID   string
	isDraft      string // "true" unless a test says the item already left

	calls    []string
	draftRaw string
	sendRaw  string
}

func (s *sendRT) RoundTrip(req *http.Request) (*http.Response, error) {
	path := req.Method + " " + req.URL.Path
	s.calls = append(s.calls, path)

	body := ""
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		if decoded, err := base64.StdEncoding.DecodeString(string(b)); err == nil {
			body = string(decoded)
		}
	}

	json := func(status int, payload string) (*http.Response, error) {
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(payload)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}, nil
	}

	switch {
	case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/sendMail"):
		s.sendRaw = body
		return json(202, `{}`)
	case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/me/messages"):
		s.draftRaw = body
		if s.createStatus != 0 && s.createStatus >= 300 {
			return json(s.createStatus, `{"error":{"code":"ErrorAccessDenied","message":"no"}}`)
		}
		return json(201, `{"id":"DRAFT_ID","internetMessageId":"`+s.assignedID+`"}`)
	case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/send"):
		if s.sendStatus != 0 && s.sendStatus >= 300 {
			return json(s.sendStatus, `{"error":{"code":"ErrorItemNotFound","message":"gone"}}`)
		}
		return json(202, `{}`)
	case req.Method == http.MethodGet:
		return json(200, `{"internetMessageId":"`+s.assignedID+`","isDraft":`+s.isDraft+`}`)
	case req.Method == http.MethodDelete:
		return json(204, ``)
	}
	return json(404, `{}`)
}

func (s *sendRT) did(method, suffix string) bool {
	for _, c := range s.calls {
		if strings.HasPrefix(c, method+" ") && strings.HasSuffix(c, suffix) {
			return true
		}
	}
	return false
}

func newSendClient(rt *sendRT) *Client {
	if rt.isDraft == "" {
		rt.isDraft = "true"
	}
	return &Client{Email: "sender@outlook.com", hc: &http.Client{Transport: rt}, folderIDs: map[string]string{}}
}

func send(c *Client, headers map[string]string, parent *models.EmailMessageData) (string, error) {
	return c.SendMessage(
		context.Background(),
		"",
		[]string{"partner@example.com"}, nil, nil,
		"<minted@outlook.com>",
		"quick learning question",
		"body", "",
		parent,
		nil,
		headers,
	)
}

// The whole point of the change: the caller learns the Message-ID Exchange
// stamped, not the one we minted, because that is the only value the recipient
// ever sees.
func TestSendMessageReturnsTheMessageIDExchangeAssigned(t *testing.T) {
	rt := &sendRT{assignedID: "<AS8P123@AS8P123.eurprd04.prod.outlook.com>"}
	got, err := send(newSendClient(rt), map[string]string{"X-Mailtrace-Verify": "tok"}, nil)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if got != "<AS8P123@AS8P123.eurprd04.prod.outlook.com>" {
		t.Errorf("message id = %q, want the id Graph assigned to the draft", got)
	}
	if !rt.did("POST", "/me/messages") || !rt.did("POST", "/send") {
		t.Errorf("expected a draft create followed by a send, got %v", rt.calls)
	}
	if rt.did("POST", "/sendMail") {
		t.Error("sendMail must not be used when a draft can be created")
	}
}

// The draft must not carry a Message-ID of ours: Exchange would keep ours on
// the item and re-stamp it at submission, so we would read back an id the
// recipient never sees.
func TestSendMessageDraftCarriesNoMintedMessageID(t *testing.T) {
	rt := &sendRT{assignedID: "<assigned@outlook.com>"}
	if _, err := send(newSendClient(rt), nil, nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if strings.Contains(rt.draftRaw, "Message-ID:") {
		t.Errorf("draft MIME should not set Message-ID:\n%s", rt.draftRaw)
	}
	if !strings.Contains(rt.draftRaw, "Subject: quick learning question") {
		t.Errorf("draft MIME lost the subject:\n%s", rt.draftRaw)
	}
}

// Threading and the verify header still have to reach the draft; Exchange
// keeps In-Reply-To even though it drops the custom header in transit.
func TestSendMessageDraftKeepsThreadingAndCustomHeaders(t *testing.T) {
	rt := &sendRT{assignedID: "<assigned@outlook.com>"}
	_, err := send(newSendClient(rt), map[string]string{"X-Mailtrace-Verify": "tok"}, &models.EmailMessageData{MessageID: "parent@x"})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	for _, want := range []string{"In-Reply-To: <parent@x>", "References: <parent@x>", "X-Mailtrace-Verify: tok"} {
		if !strings.Contains(rt.draftRaw, want) {
			t.Errorf("draft MIME missing %q:\n%s", want, rt.draftRaw)
		}
	}
}

// A mailbox that cannot create drafts must still send. Nothing was created, so
// falling back cannot double-send.
func TestSendMessageFallsBackToSendMailWhenTheDraftCannotBeCreated(t *testing.T) {
	rt := &sendRT{createStatus: 403}
	got, err := send(newSendClient(rt), nil, nil)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if got != "<minted@outlook.com>" {
		t.Errorf("fallback should report the minted id, got %q", got)
	}
	if !rt.did("POST", "/sendMail") {
		t.Errorf("expected the sendMail fallback, got %v", rt.calls)
	}
	if !strings.Contains(rt.sendRaw, "Message-ID: <minted@outlook.com>") {
		t.Errorf("fallback MIME must carry our Message-ID:\n%s", rt.sendRaw)
	}
}

// A draft whose send is refused would otherwise sit in the customer's Drafts
// folder as a message they never wrote.
func TestSendMessageDiscardsTheDraftWhenTheSendFails(t *testing.T) {
	rt := &sendRT{assignedID: "<assigned@outlook.com>", sendStatus: 404}
	if _, err := send(newSendClient(rt), nil, nil); err == nil {
		t.Fatal("a refused send must return an error")
	}
	if !rt.did("DELETE", "/me/messages/DRAFT_ID") {
		t.Errorf("expected the draft to be discarded, got %v", rt.calls)
	}
	if rt.did("POST", "/sendMail") {
		t.Error("a created draft must never fall back to sendMail; that would send twice")
	}
}

// Graph does not always return internetMessageId on the create response.
func TestSendMessageRereadsTheAssignedIDWhenCreateOmitsIt(t *testing.T) {
	// create answers with no internetMessageId; the follow-up GET has it.
	rt := &sendRT{assignedID: ""}
	c := newSendClient(rt)
	c.hc = &http.Client{Transport: &rereadRT{sendRT: rt, reread: "<reread@outlook.com>"}}
	got, err := send(c, nil, nil)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if got != "<reread@outlook.com>" {
		t.Errorf("message id = %q, want the re-read id", got)
	}
}

// rereadRT answers the create with no internetMessageId and the follow-up GET
// with one.
type rereadRT struct {
	*sendRT
	reread string
}

func (r *rereadRT) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodGet {
		r.calls = append(r.calls, req.Method+" "+req.URL.Path)
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"internetMessageId":"` + r.reread + `"}`)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}, nil
	}
	return r.sendRT.RoundTrip(req)
}

// An ambiguous send failure may mean the message went out after all. Deleting
// the item then would take the customer's Sent Items copy with it.
func TestSendMessageLeavesAnItemThatIsNoLongerADraft(t *testing.T) {
	rt := &sendRT{assignedID: "<assigned@outlook.com>", sendStatus: 500, isDraft: "false"}
	if _, err := send(newSendClient(rt), nil, nil); err == nil {
		t.Fatal("a refused send must return an error")
	}
	if rt.did("DELETE", "/me/messages/DRAFT_ID") {
		t.Error("an item that already left the Drafts folder must not be deleted")
	}
}
