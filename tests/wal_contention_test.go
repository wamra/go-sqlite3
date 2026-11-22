package tests

import (
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	_ "github.com/ncruces/go-sqlite3/internal/testcfg"
	"github.com/ncruces/go-sqlite3/vfs"
)

// Several connections with a busy timeout write to one WAL database and none
// may fail with "database is locked". This passed on Windows even with the
// timed file lock that could leak a lock (upstream #254), so it guards the
// behaviour but does not reproduce that defect.
func TestWAL_contention(t *testing.T) {
	if !vfs.SupportsSharedMemory {
		t.Skip("skipping without shared memory")
	}

	tmp := filepath.ToSlash(filepath.Join(t.TempDir(), "test.db"))
	dsn := "file:" + tmp + "?_pragma=busy_timeout(10000)&_pragma=journal_mode(wal)"

	setup, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setup.Exec(`CREATE TABLE test (writer, n)`); err != nil {
		t.Fatal(err)
	}
	setup.Close()

	const writers, rounds = 4, 400
	start := time.Now()
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			db, err := sql.Open("sqlite3", dsn)
			if err != nil {
				errs <- err
				return
			}
			defer db.Close()
			db.SetMaxOpenConns(1)
			for n := 0; n < rounds; n++ {
				if _, err := db.Exec(`INSERT INTO test VALUES (?, ?)`, w, n); err != nil {
					errs <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("writer failed after %v: %v", time.Since(start), err)
	}

	check, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	var got int
	if err := check.QueryRow(`SELECT count(*) FROM test`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != writers*rounds && !t.Failed() {
		t.Errorf("got %d rows, want %d", got, writers*rounds)
	}
	t.Logf("%d rows in %v", got, time.Since(start))
}
