package server

import (
	"context"
	"errors"
	"testing"

	"github.com/torrin-app/torrin/shared/auth"
)

type fakeUsenetUsers struct {
	jobNZB map[string][]byte
}

func (f fakeUsenetUsers) GetJobNZB(_ context.Context, infoHash string) ([]byte, bool) {
	data, ok := f.jobNZB[infoHash]
	return data, ok
}
func (fakeUsenetUsers) GetUsenetCreds(context.Context, string) (*auth.UsenetCreds, error) {
	return nil, errors.New("none")
}
func (fakeUsenetUsers) GetByID(context.Context, string) (*auth.User, error) {
	return nil, errors.New("none")
}

type fakeNZBStore struct {
	data map[string][]byte
}

func (f fakeNZBStore) GetBytes(_ context.Context, key string) ([]byte, error) {
	if data, ok := f.data[key]; ok {
		return data, nil
	}
	return nil, errors.New("not found")
}

func TestUsenetBackendNZBSource(t *testing.T) {
	hash := "abc"
	key := "nzb/abc.nzb"
	dbData := []byte("from-db")
	storeData := []byte("from-store")

	t.Run("prefers postgres over the object store", func(t *testing.T) {
		u := &usenetBackend{users: fakeUsenetUsers{jobNZB: map[string][]byte{hash: dbData}}}
		store := fakeNZBStore{data: map[string][]byte{key: storeData}}
		got, ok := u.nzb(context.Background(), hash, key, store)
		if !ok || string(got) != "from-db" {
			t.Fatalf("got %q ok=%v, want from-db", got, ok)
		}
	})

	t.Run("falls back to the object store when postgres is empty", func(t *testing.T) {
		u := &usenetBackend{users: fakeUsenetUsers{jobNZB: map[string][]byte{}}}
		store := fakeNZBStore{data: map[string][]byte{key: storeData}}
		got, ok := u.nzb(context.Background(), hash, key, store)
		if !ok || string(got) != "from-store" {
			t.Fatalf("got %q ok=%v, want from-store", got, ok)
		}
	})

	t.Run("not found in either -> not ok", func(t *testing.T) {
		u := &usenetBackend{users: fakeUsenetUsers{jobNZB: map[string][]byte{}}}
		store := fakeNZBStore{data: map[string][]byte{}}
		if _, ok := u.nzb(context.Background(), hash, key, store); ok {
			t.Fatal("want not ok")
		}
	})
}
