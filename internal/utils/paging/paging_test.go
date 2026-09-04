package paging

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMergedCursorRoundTrip(t *testing.T) {
	at := time.Date(2026, 6, 9, 11, 42, 0, 123456000, time.FixedZone("CEST", 2*3600))
	id := uuid.New()
	tok := EncodeMerged(at, 7, id)
	if tok == nil {
		t.Fatal("want a token for a real position")
	}
	gotAt, gotSource, gotID, xerr := DecodeMergedCursor(*tok)
	if xerr != nil {
		t.Fatalf("decode: %v", xerr)
	}
	if !gotAt.Equal(at) || gotSource != 7 || gotID != id {
		t.Fatalf("round trip lost data: %v %d %s", gotAt, gotSource, gotID)
	}
	if EncodeMerged(at, 7, uuid.Nil) != nil {
		t.Fatal("the zero id means no next page and must encode as nil")
	}
}

func TestMergedCursorRejectsMalformedTokens(t *testing.T) {
	if _, _, _, xerr := DecodeMergedCursor(""); xerr != nil {
		t.Fatalf("an empty token is the first page, got %v", xerr)
	}
	for _, tok := range []string{
		"2026-06-09T11:42:00Z",                            // a bare timestamp is not a cursor
		"t1_" + (*EncodeTime(time.Now(), uuid.New()))[3:], // wrong version
		"m1_!!!", // not base64
		"m1_" + (*EncodeMerged(time.Now(), 1, uuid.New()))[3:] + "x", // trailing garbage
	} {
		if _, _, _, xerr := DecodeMergedCursor(tok); xerr == nil {
			t.Fatalf("%q must be rejected", tok)
		}
	}
}
