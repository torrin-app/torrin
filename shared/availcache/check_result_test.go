package availcache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestDetailedDoesNotCacheMissingResults(t *testing.T) {
	c := &Cache{ttl: time.Hour, mem: map[string]entry{}}
	calls := 0
	query := func(_ context.Context, hs []string) CheckResult {
		calls++
		if calls == 1 {
			return CheckResult{Cached: map[string]bool{"aabb": true}, Reasons: map[string]string{"ccdd": "timeout"}}
		}
		if len(hs) != 1 || hs[0] != "ccdd" {
			t.Fatalf("must query only unanswered hash: %v", hs)
		}
		return CheckResult{Cached: map[string]bool{"ccdd": false}}
	}
	first := c.CheckCachedDetailed(context.Background(), "p:", []string{"AABB", "CCDD"}, query)
	if !first.Cached["aabb"] || first.Reasons["ccdd"] != "timeout" {
		t.Fatalf("first=%+v", first)
	}
	if _, ok := first.Cached["ccdd"]; ok {
		t.Fatal("failure converted to negative")
	}
	second := c.CheckCachedDetailed(context.Background(), "p:", []string{"aabb", "ccdd"}, query)
	if v, ok := second.Cached["ccdd"]; !ok || v || len(second.Reasons) != 0 {
		t.Fatalf("explicit negative not retained: %+v", second)
	}
	c.CheckCachedDetailed(context.Background(), "p:", []string{"aabb", "ccdd"}, query)
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}
func TestDetailedEmptyAndPartialResponsesRemainUnanswered(t *testing.T) {
	c := &Cache{ttl: time.Hour, mem: map[string]entry{}}
	calls := 0
	q := func(context.Context, []string) CheckResult { calls++; return CheckResult{Cached: map[string]bool{}} }
	for i := 0; i < 2; i++ {
		r := c.CheckCachedDetailed(context.Background(), "p:", []string{"aabb"}, q)
		if len(r.Cached) != 0 || r.Reasons["aabb"] != "partial_response" {
			t.Fatalf("result=%+v", r)
		}
	}
	if calls != 2 {
		t.Fatal("empty response was cached")
	}
}
func TestDetailedExcludesLegacyNegativesAndUnrequestedValues(t *testing.T) {
	c := &Cache{ttl: time.Hour, mem: map[string]entry{"p:aabb": {val: false, exp: time.Now().Add(time.Hour)}}}
	r := c.CheckCachedDetailed(context.Background(), "p:", []string{"AABB"}, func(context.Context, []string) CheckResult {
		return CheckResult{Cached: map[string]bool{"AABB": true, "foreign": true}, Reasons: map[string]string{"aabb": "contains-secret-url"}}
	})
	if !r.Cached["aabb"] || len(r.Cached) != 1 || len(r.Reasons) != 0 {
		t.Fatalf("result=%+v", r)
	}
}
func TestReasonsAreSanitized(t *testing.T) {
	if Failure([]string{"AABB"}, "https://secret.invalid/key").Reasons["aabb"] != "check_failed" {
		t.Fatal("unsafe reason")
	}
	if ErrorReason(context.DeadlineExceeded) != "timeout" || ErrorReason(context.Canceled) != "cancelled" || ErrorReason(errors.New("sensitive")) != "check_failed" {
		t.Fatal("error reason")
	}
}

type expiryRows struct {
	key  string
	val  bool
	exp  time.Time
	read bool
}

func (r *expiryRows) Close()                                       {}
func (r *expiryRows) Err() error                                   { return nil }
func (r *expiryRows) CommandTag() pgconn.CommandTag                { return pgconn.NewCommandTag("") }
func (r *expiryRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *expiryRows) Next() bool {
	if r.read {
		return false
	}
	r.read = true
	return true
}
func (r *expiryRows) Scan(dest ...any) error {
	*dest[0].(*string) = r.key
	*dest[1].(*bool) = r.val
	*dest[2].(*time.Time) = r.exp
	return nil
}
func (r *expiryRows) Values() ([]any, error) { return []any{r.key, r.val, r.exp}, nil }
func (r *expiryRows) RawValues() [][]byte    { return nil }
func (r *expiryRows) Conn() *pgx.Conn        { return nil }

type expiryDB struct{ exp time.Time }

func (d *expiryDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
func (d *expiryDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return &expiryRows{key: "diagnostics:v2:p:aabb", val: false, exp: d.exp}, nil
}
func TestDatabaseCacheRetainsOriginalExpiry(t *testing.T) {
	expiry := time.Now().Add(30 * time.Millisecond)
	c := &Cache{ttl: time.Hour, mem: map[string]entry{}, db: &expiryDB{exp: expiry}}
	calls := 0
	q := func(context.Context, []string) CheckResult {
		calls++
		return CheckResult{Cached: map[string]bool{"aabb": true}}
	}
	first := c.CheckCachedDetailed(context.Background(), "p:", []string{"aabb"}, q)
	if value, ok := first.Cached["aabb"]; !ok || value || calls != 0 {
		t.Fatalf("first=%+v calls=%d", first, calls)
	}
	if !c.mem["diagnostics:v2:p:aabb"].exp.Equal(expiry) {
		t.Fatal("database expiry extended")
	}
	time.Sleep(45 * time.Millisecond)
	second := c.CheckCachedDetailed(context.Background(), "p:", []string{"aabb"}, q)
	if !second.Cached["aabb"] || calls != 1 {
		t.Fatalf("expired negative persisted: %+v calls=%d", second, calls)
	}
}

func TestDetailedConflictingCanonicalResultsStayUnresolved(t *testing.T) {
	c := &Cache{ttl: time.Hour, mem: map[string]entry{}}
	result := c.CheckCachedDetailed(context.Background(), "p:", []string{"aabb", "ccdd"}, func(context.Context, []string) CheckResult {
		return CheckResult{Cached: map[string]bool{"AABB": true, "aabb": false, "ccdd": true}}
	})
	if _, known := result.Cached["aabb"]; known || result.Reasons["aabb"] != "invalid_response" || !result.Cached["ccdd"] {
		t.Fatalf("result=%+v", result)
	}
}
