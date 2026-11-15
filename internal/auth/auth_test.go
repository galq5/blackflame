package auth

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/galq/blackflame/internal/store"
)

func testDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureDefaults(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db, func() { db.Close() }
}

func TestSetupLoginAndSession(t *testing.T) {
	db, cleanup := testDB(t)
	defer cleanup()
	ctx := context.Background()
	manager, err := New(ctx, db, "")
	if err != nil {
		t.Fatal(err)
	}
	configured, err := manager.PasswordConfigured(ctx)
	if err != nil || configured {
		t.Fatalf("expected unconfigured: %v %v", configured, err)
	}
	if err := manager.SetPassword(ctx, "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetPassword(ctx, "another password"); err != ErrAlreadyConfigured {
		t.Fatalf("expected already configured, got %v", err)
	}
	var stored string
	if err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, passwordSetting).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == "correct horse battery staple" || len(stored) < 20 {
		t.Fatalf("password was not hashed: %q", stored)
	}
	if _, err := manager.Login(ctx, "wrong password", "127.0.0.1"); err != ErrInvalidPassword {
		t.Fatalf("expected invalid password, got %v", err)
	}
	session, err := manager.Login(ctx, "correct horse battery staple", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if session.CSRF == "" || session.Role != "admin" {
		t.Fatalf("unexpected session: %+v", session)
	}
	loaded, err := manager.Authenticate(ctx, session.ID)
	if err != nil || loaded.ID != session.ID {
		t.Fatalf("authenticate: %v %+v", err, loaded)
	}
	if err := manager.Logout(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Authenticate(ctx, session.ID); err != ErrNotAuthenticated {
		t.Fatalf("expected unauthenticated, got %v", err)
	}
}

func TestEnvironmentPassword(t *testing.T) {
	db, cleanup := testDB(t)
	defer cleanup()
	ctx := context.Background()
	manager, err := New(ctx, db, "env-password")
	if err != nil {
		t.Fatal(err)
	}
	configured, err := manager.PasswordConfigured(ctx)
	if err != nil || !configured {
		t.Fatalf("expected configured: %v %v", configured, err)
	}
	if _, err := manager.Login(ctx, "env-password", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Login(ctx, "wrong", "127.0.0.1"); err != ErrInvalidPassword {
		t.Fatalf("expected invalid password, got %v", err)
	}
}

func TestConcurrentPasswordSetup(t *testing.T) {
	db, cleanup := testDB(t)
	defer cleanup()
	ctx := context.Background()
	manager, err := New(ctx, db, "")
	if err != nil {
		t.Fatal(err)
	}

	type attempt struct {
		password string
		err      error
	}
	start := make(chan struct{})
	results := make(chan attempt, 2)
	for _, password := range []string{"first password", "second password"} {
		go func() {
			<-start
			results <- attempt{password, manager.SetPassword(ctx, password)}
		}()
	}
	close(start)

	var winner string
	for range 2 {
		result := <-results
		switch result.err {
		case nil:
			if winner != "" {
				t.Fatal("both setup attempts succeeded")
			}
			winner = result.password
		case ErrAlreadyConfigured:
		default:
			t.Fatalf("setup failed: %v", result.err)
		}
	}
	if winner == "" {
		t.Fatal("neither setup attempt succeeded")
	}
	if _, err := manager.Login(ctx, winner, "127.0.0.1"); err != nil {
		t.Fatalf("winning password did not authenticate: %v", err)
	}
}
