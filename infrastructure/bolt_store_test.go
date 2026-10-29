//go:build integration

package infrastructure

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/UrielJaloto/music-ops/services"
)

type movableClock struct {
	mutex sync.Mutex
	now   time.Time
}

func (clock *movableClock) Now() time.Time {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	return clock.now
}

func (clock *movableClock) advance(duration time.Duration) {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	clock.now = clock.now.Add(duration)
}

func openTestStore(t *testing.T, path string) *BoltStore {
	t.Helper()
	store, err := OpenBoltStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestBoltCache_GetPut(t *testing.T) {
	clock := &movableClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewBoltCache(openTestStore(t, filepath.Join(t.TempDir(), "state.db")), clock)
	ctx := context.Background()

	_, isMissHit, missErr := cache.Get(ctx, "lastfm", "song")
	putErr := cache.Put(ctx, "lastfm", "song", []byte("rock"), time.Hour)
	value, isHit, hitErr := cache.Get(ctx, "lastfm", "song")
	overwriteErr := cache.Put(ctx, "lastfm", "song", []byte("pop"), time.Hour)
	overwritten, _, _ := cache.Get(ctx, "lastfm", "song")

	if isMissHit || missErr != nil || putErr != nil || overwriteErr != nil {
		t.Fatalf("miss = %v errors = %v %v %v", isMissHit, missErr, putErr, overwriteErr)
	}
	if !isHit || hitErr != nil || string(value) != "rock" || string(overwritten) != "pop" {
		t.Fatalf("value = %q overwritten = %q hit = %v err = %v", value, overwritten, isHit, hitErr)
	}
}

func TestBoltCache_Expiry(t *testing.T) {
	cases := []struct {
		name      string
		elapsed   time.Duration
		isHit     bool
		isDeleted bool
	}{
		{name: "BeforeExpiry_Hit", elapsed: time.Hour - time.Nanosecond, isHit: true},
		{name: "AtExpiry_MissAndDeleted", elapsed: time.Hour, isDeleted: true},
		{name: "AfterExpiry_MissAndDeleted", elapsed: 2 * time.Hour, isDeleted: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			clock := &movableClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
			store := openTestStore(t, filepath.Join(t.TempDir(), "state.db"))
			cache := NewBoltCache(store, clock)
			if err := cache.Put(context.Background(), "advisor", "k", []byte("v"), time.Hour); err != nil {
				t.Fatal(err)
			}
			clock.advance(testCase.elapsed)

			_, isHit, err := cache.Get(context.Background(), "advisor", "k")

			_, isStored, _ := store.read(context.Background(), "advisor", "k")
			if err != nil || isHit != testCase.isHit || isStored == testCase.isDeleted {
				t.Fatalf("hit = %v stored = %v err = %v", isHit, isStored, err)
			}
		})
	}
}

func TestBoltCache_ConcurrentWriters(t *testing.T) {
	clock := &movableClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewBoltCache(openTestStore(t, filepath.Join(t.TempDir(), "state.db")), clock)
	var group sync.WaitGroup
	errs := make(chan error, 50)

	for index := range 50 {
		group.Go(func() {
			errs <- cache.Put(context.Background(), "lastfm", strconv.Itoa(index), []byte(strconv.Itoa(index)), time.Hour)
		})
	}
	group.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for index := range 50 {
		value, isHit, err := cache.Get(context.Background(), "lastfm", strconv.Itoa(index))
		if err != nil || !isHit || string(value) != strconv.Itoa(index) {
			t.Fatalf("key %d value = %q hit = %v err = %v", index, value, isHit, err)
		}
	}
}

func TestBoltCache_CorruptEnvelope(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "state.db"))
	if err := store.write(context.Background(), "lastfm", "bad", []byte("{not json")); err != nil {
		t.Fatal(err)
	}

	_, isHit, err := NewBoltCache(store, &movableClock{}).Get(context.Background(), "lastfm", "bad")

	if err == nil || isHit {
		t.Fatalf("hit = %v err = %v", isHit, err)
	}
}

func TestBoltStore_CancelledContext(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "state.db"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, readErr := store.read(ctx, "b", "k")
	writeErr := store.write(ctx, "b", "k", []byte("v"))
	deleteErr := store.delete(ctx, "b", "k")

	if !errors.Is(readErr, context.Canceled) || !errors.Is(writeErr, context.Canceled) || !errors.Is(deleteErr, context.Canceled) {
		t.Fatalf("errors = %v %v %v", readErr, writeErr, deleteErr)
	}
}

func TestOpenBoltStore_SecondOpenWhileLocked_ReturnsErrLibraryBusy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	openTestStore(t, path)

	_, err := OpenBoltStore(path)

	if !errors.Is(err, services.ErrLibraryBusy) {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenBoltStore_MissingFolder(t *testing.T) {
	_, err := OpenBoltStore(filepath.Join(t.TempDir(), "missing", "state.db"))

	if err == nil || errors.Is(err, services.ErrLibraryBusy) {
		t.Fatalf("err = %v", err)
	}
}

func TestJournalRepository_RoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := OpenBoltStore(path)
	if err != nil {
		t.Fatal(err)
	}
	journal := NewJournalRepository(store)
	ctx := context.Background()

	isConvertedBefore, convertedErr := journal.IsConverted(ctx, "t1")
	_, isPublishedBefore, publishedErr := journal.IsPublished(ctx, "t1")
	recordConvertedErr := journal.RecordConverted(ctx, "t1", "staged/t1.m4a")
	recordPublishedErr := journal.RecordPublished(ctx, "t1", "Artist - Song.m4a")
	closeErr := store.Close()
	reopened := NewJournalRepository(openTestStore(t, path))
	isConverted, _ := reopened.IsConverted(ctx, "t1")
	target, isPublished, _ := reopened.IsPublished(ctx, "t1")
	isOtherConverted, _ := reopened.IsConverted(ctx, "t2")

	if isConvertedBefore || isPublishedBefore || convertedErr != nil || publishedErr != nil || recordConvertedErr != nil || recordPublishedErr != nil || closeErr != nil {
		t.Fatal("unexpected state before recording")
	}
	if !isConverted || !isPublished || target != "Artist - Song.m4a" || isOtherConverted {
		t.Fatalf("converted = %v published = %v target = %q other = %v", isConverted, isPublished, target, isOtherConverted)
	}
}

func TestJournalRepository_CorruptRecord(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "state.db"))
	if err := store.write(context.Background(), publishedBucket, "t1", []byte("[")); err != nil {
		t.Fatal(err)
	}

	_, _, err := NewJournalRepository(store).IsPublished(context.Background(), "t1")

	if err == nil {
		t.Fatal("corrupt record accepted")
	}
}

func TestReportStorage_Create(t *testing.T) {
	output := t.TempDir()

	writer, err := NewReportStorage(output).Create(context.Background(), "20261016-083005-abcd", "report.md")
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := io.WriteString(writer, "# Report")
	closeErr := writer.Close()

	content, readErr := os.ReadFile(filepath.Join(output, ".music-ops", "runs", "20261016-083005-abcd", "report.md"))
	if writeErr != nil || closeErr != nil || readErr != nil || string(content) != "# Report" {
		t.Fatalf("content = %q errors = %v %v %v", content, writeErr, closeErr, readErr)
	}
}

func TestReportStorage_Create_Errors(t *testing.T) {
	output := filepath.Join(t.TempDir(), "file")
	writeTestFile(t, output, "x")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, folderErr := NewReportStorage(output).Create(context.Background(), "run", "report.md")
	_, cancelErr := NewReportStorage(t.TempDir()).Create(ctx, "run", "report.md")

	if folderErr == nil || !errors.Is(cancelErr, context.Canceled) {
		t.Fatalf("errors = %v %v", folderErr, cancelErr)
	}
}

func TestReportStorage_Location(t *testing.T) {
	location := NewReportStorage("E:/Music").Location("run-1", "report.md")

	if location != filepath.Join("E:/Music", ".music-ops", "runs", "run-1", "report.md") {
		t.Fatalf("location = %s", location)
	}
}
