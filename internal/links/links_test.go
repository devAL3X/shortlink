package links

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeStore struct {
	links map[string]Link
	calls []string
}

func (f *fakeStore) Create(_ context.Context, url string) (Link, error) {
	l := Link{Code: NewCode(6), URL: url, CreatedAt: time.Now()}
	f.links[l.Code] = l
	f.calls = append(f.calls, "create")
	return l, nil
}

func (f *fakeStore) Get(_ context.Context, code string) (Link, error) {
	if l, ok := f.links[code]; ok {
		f.calls = append(f.calls, "get:"+code)
		return l, nil
	}
	return Link{}, ErrNotFound
}

func (f *fakeStore) List(_ context.Context) ([]Link, error) { return nil, nil }
func (f *fakeStore) UpdateURL(_ context.Context, code, url string) error {
	l, ok := f.links[code]
	if !ok {
		return ErrNotFound
	}
	l.URL = url
	f.links[code] = l
	return nil
}

func (f *fakeStore) Delete(_ context.Context, code string) error {
	if _, ok := f.links[code]; !ok {
		return ErrNotFound
	}
	delete(f.links, code)
	return nil
}

func (f *fakeStore) RecordClick(_ context.Context, _ string) error { return nil }

type fakeCache struct {
	data  map[string]string
	hits  int
	sets  int
	dels  int
}

func (f *fakeCache) Get(_ context.Context, code string) (string, error) {
	if url, ok := f.data[code]; ok {
		f.hits++
		return url, nil
	}
	return "", ErrCacheMiss
}

func (f *fakeCache) Set(_ context.Context, code, url string, _ time.Duration) error {
	f.sets++
	f.data[code] = url
	return nil
}

func (f *fakeCache) Del(_ context.Context, code string) error {
	f.dels++
	delete(f.data, code)
	return nil
}

type fakePub struct{ published []string }

func (f *fakePub) PublishClicked(_ context.Context, code string) error {
	f.published = append(f.published, code)
	return nil
}

func newTestService() (*Service, *fakeStore, *fakeCache, *fakePub) {
	st := &fakeStore{links: map[string]Link{}}
	ca := &fakeCache{data: map[string]string{}}
	pu := &fakePub{}
	return New(st, ca, pu, time.Minute, nil), st, ca, pu
}

func TestResolveCacheMissFillsCacheAndPublishes(t *testing.T) {
	svc, _, ca, pu := newTestService()
	l, err := svc.Create(context.Background(), "https://hse.ru")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	target, cached, err := svc.Resolve(context.Background(), l.Code)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if target != "https://hse.ru" {
		t.Errorf("target = %q, want https://hse.ru", target)
	}
	if cached {
		t.Error("first resolve must be a cache miss")
	}
	if ca.sets != 1 {
		t.Errorf("cache sets = %d, want 1", ca.sets)
	}
	if len(pu.published) != 1 || pu.published[0] != l.Code {
		t.Errorf("published = %v, want [%s]", pu.published, l.Code)
	}
}

func TestResolveCacheHitSkipsStore(t *testing.T) {
	svc, st, ca, pu := newTestService()
	l, _ := svc.Create(context.Background(), "https://hse.ru")
	_, _, _ = svc.Resolve(context.Background(), l.Code) // прогрев кэша

	warmupCalls := len(st.calls)

	target, cached, err := svc.Resolve(context.Background(), l.Code)
	if err != nil || !cached {
		t.Fatalf("second resolve must hit cache (cached=%v, err=%v)", cached, err)
	}
	if target != "https://hse.ru" {
		t.Errorf("target = %q", target)
	}
	for _, c := range st.calls[warmupCalls:] {
		if c == "get:"+l.Code {
			t.Error("store.Get must not be called on cache hit")
		}
	}
	if len(pu.published) != 2 {
		t.Errorf("clicks published = %d, want 2", len(pu.published))
	}
	if ca.hits != 1 {
		t.Errorf("cache hits = %d, want 1", ca.hits)
	}
}

func TestDeleteRemovesFromCache(t *testing.T) {
	svc, _, ca, _ := newTestService()
	l, _ := svc.Create(context.Background(), "https://hse.ru")
	_, _, _ = svc.Resolve(context.Background(), l.Code)

	if err := svc.Delete(context.Background(), l.Code); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if ca.dels != 1 {
		t.Errorf("cache dels = %d, want 1", ca.dels)
	}
	if _, _, err := svc.Resolve(context.Background(), l.Code); !errors.Is(err, ErrNotFound) {
		t.Errorf("resolve after delete = %v, want ErrNotFound", err)
	}
}

func TestUpdateInvalidatesCache(t *testing.T) {
	svc, _, ca, _ := newTestService()
	l, _ := svc.Create(context.Background(), "https://old.example")
	_, _, _ = svc.Resolve(context.Background(), l.Code)

	if err := svc.Update(context.Background(), l.Code, "https://new.example"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if ca.dels != 1 {
		t.Errorf("cache dels = %d, want 1", ca.dels)
	}
}

func TestNewCodeUniqueAndLength(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for range 1000 {
		c := NewCode(6)
		if len(c) != 6 {
			t.Fatalf("code length = %d, want 6", len(c))
		}
		if seen[c] {
			t.Fatalf("duplicate code %q", c)
		}
		seen[c] = true
	}
}
