package tests

import (
	"path/filepath"
	"testing"

	"github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/embed"
	_ "github.com/ncruces/go-sqlite3/internal/testcfg"
	"github.com/ncruces/go-sqlite3/vfs"
)

// A transaction that grows the WAL past the first WAL-index region (about
// 4,062 frames) needs a second 32 KiB region of the shared memory file.
// On Windows that region starts at an offset that is not a multiple of the
// 64 KiB allocation granularity (upstream issue #330).
func TestWAL_large_transaction(t *testing.T) {
	if !vfs.SupportsSharedMemory {
		t.Skip("skipping without shared memory")
	}
	t.Parallel()

	file := filepath.Join(t.TempDir(), "test.db")

	db, err := sqlite3.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	err = db.Exec(`
		PRAGMA journal_mode=wal;
		PRAGMA wal_autocheckpoint=0;
		CREATE TABLE test (col);
	`)
	if err != nil {
		t.Fatal(err)
	}

	// 3 x 5,000 rows of 4 KiB: about 15,000 frames, four index regions.
	for i := 0; i < 3; i++ {
		err = db.Exec(`
			BEGIN;
			WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < 5000)
			INSERT INTO test SELECT randomblob(4000) FROM n;
			COMMIT;
		`)
		if err != nil {
			t.Fatal(err)
		}
	}

	// A second connection maps the same regions and reads through the WAL.
	db2, err := sqlite3.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	stmt, _, err := db2.Prepare(`SELECT count(*) FROM test`)
	if err != nil {
		t.Fatal(err)
	}
	if !stmt.Step() {
		t.Fatal(stmt.Err())
	}
	if got := stmt.ColumnInt(0); got != 15000 {
		t.Errorf("got %d rows, want 15000", got)
	}
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}

	stmt2, _, err := db2.Prepare(`PRAGMA wal_checkpoint(truncate)`)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt2.Close()
	if !stmt2.Step() {
		t.Fatal(stmt2.Err())
	}
	if busy := stmt2.ColumnInt(0); busy != 0 {
		t.Logf("checkpoint busy=%d", busy)
	}
}
