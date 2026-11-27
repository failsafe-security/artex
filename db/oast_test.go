package db

import (
	"testing"
	"time"
)

func oastDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	if err := d.EnsureOASTTables(); err != nil {
		t.Fatalf("ensure OAST tables: %v", err)
	}
	return d
}

func TestOASTTokenLifecycle(t *testing.T) {
	d := oastDB(t)
	defer d.Close()

	token := "test" + time.Now().Format("150405000000")
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM oast_interactions WHERE token=$1`, token)
		_, _ = d.Exec(`DELETE FROM oast_tokens WHERE token=$1`, token)
	})

	expires := time.Now().Add(time.Hour).Truncate(time.Millisecond)
	if err := d.InsertOASTToken(token, 123, "label", expires); err != nil {
		t.Fatal(err)
	}
	if err := d.InsertOASTToken(token, 456, "duplicate", expires); err == nil {
		t.Fatal("expected a duplicate token insert to fail")
	}

	got, ok, err := d.LookupOASTToken(token)
	if err != nil || !ok {
		t.Fatalf("lookup: ok=%v err=%v", ok, err)
	}
	if got.TaskID != 123 || got.Label != "label" || !got.ExpiresAt.Equal(expires) {
		t.Fatalf("unexpected token: %+v", got)
	}

	if _, ok, err := d.LookupOASTToken("missingtoken"); err != nil || ok {
		t.Fatalf("missing token: ok=%v err=%v", ok, err)
	}
}

func TestOASTInteractionAppendAndPage(t *testing.T) {
	d := oastDB(t)
	defer d.Close()

	token := "test" + time.Now().Format("150405000001")
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM oast_interactions WHERE token=$1`, token)
		_, _ = d.Exec(`DELETE FROM oast_tokens WHERE token=$1`, token)
	})

	var ids []int64
	for _, body := range []string{"one", "two", "three"} {
		id, err := d.InsertOASTInteraction(OASTInteraction{
			Token: token, TaskID: 5, Proto: "http", Method: "GET",
			Host: "oast.test", Path: "/collect", Peer: "127.0.0.1",
			UserAgent: "ua", Body: body,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	first, err := d.ListOASTInteractions(token, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[0].ID != ids[0] || first[1].ID != ids[1] {
		t.Fatalf("first page: %+v", first)
	}
	if first[0].Body != "one" || first[0].TaskID != 5 || first[0].UserAgent != "ua" {
		t.Fatalf("unexpected row: %+v", first[0])
	}

	rest, err := d.ListOASTInteractions(token, ids[1], 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 1 || rest[0].ID != ids[2] || rest[0].Body != "three" {
		t.Fatalf("second page: %+v", rest)
	}
}
