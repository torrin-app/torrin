package billing

import (
	"context"
	"errors"
	"testing"

	"github.com/torrin-app/torrin/shared/auth"
)

type fakeUsers struct {
	byEmail map[string]*auth.User
	bySub   map[string]*auth.User
	created []string
}

func (f *fakeUsers) GetByEmail(_ context.Context, email string) (*auth.User, error) {
	if u, ok := f.byEmail[email]; ok {
		return u, nil
	}
	return nil, errors.New("not found")
}

func (f *fakeUsers) GetBySubscription(_ context.Context, subID string) (*auth.User, error) {
	if u, ok := f.bySub[subID]; ok {
		return u, nil
	}
	return nil, errors.New("not found")
}

func (f *fakeUsers) CreateUser(_ context.Context, email string, _ ...string) (*auth.User, error) {
	u := &auth.User{ID: "new-" + email, Email: email}
	f.created = append(f.created, email)
	return u, nil
}

func TestResolveSaleUser(t *testing.T) {
	real := &auth.User{ID: "real", Email: "app@x.com"}
	billing := &auth.User{ID: "ghost", Email: "billing@x.com"}
	existing := &auth.User{ID: "existing", Email: "app@x.com"}

	t.Run("subscription match wins over billing email", func(t *testing.T) {
		f := &fakeUsers{
			byEmail: map[string]*auth.User{"billing@x.com": billing},
			bySub:   map[string]*auth.User{"SUB1": real},
		}
		u, created, err := resolveSaleUser(context.Background(), f, "billing@x.com", "SUB1")
		if err != nil || created || u.ID != "real" {
			t.Fatalf("want real account by sub, got id=%v created=%v err=%v", u, created, err)
		}
		if len(f.created) != 0 {
			t.Fatal("must not create a user when sub matches")
		}
	})

	t.Run("no sub match falls back to email", func(t *testing.T) {
		f := &fakeUsers{byEmail: map[string]*auth.User{"app@x.com": existing}}
		u, created, err := resolveSaleUser(context.Background(), f, "app@x.com", "UNKNOWN")
		if err != nil || created || u.ID != "existing" {
			t.Fatalf("want existing account by email, got id=%v created=%v err=%v", u, created, err)
		}
	})

	t.Run("empty sub uses email", func(t *testing.T) {
		f := &fakeUsers{byEmail: map[string]*auth.User{"app@x.com": existing}}
		u, _, err := resolveSaleUser(context.Background(), f, "app@x.com", "")
		if err != nil || u.ID != "existing" {
			t.Fatalf("want existing by email, got id=%v err=%v", u, err)
		}
	})

	t.Run("unknown email creates a user", func(t *testing.T) {
		f := &fakeUsers{}
		u, created, err := resolveSaleUser(context.Background(), f, "brand@new.com", "NOPE")
		if err != nil || !created || u.Email != "brand@new.com" {
			t.Fatalf("want a created user, got id=%v created=%v err=%v", u, created, err)
		}
		if len(f.created) != 1 {
			t.Fatalf("expected one CreateUser call, got %d", len(f.created))
		}
	})
}
