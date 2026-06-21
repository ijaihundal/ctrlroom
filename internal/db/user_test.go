package db

import (
	"context"
	"errors"
	"testing"
)

func TestCountUsers_Empty(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	n, err := CountUsers(context.Background(), db)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("got %d users, want 0", n)
	}
}

func TestCreateUser_PopulatesFields(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	u, err := CreateUser(context.Background(), db, "alice", "hashed-pw")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if u.ID == "" {
		t.Error("ID is empty")
	}
	if u.Username != "alice" {
		t.Errorf("Username = %q, want %q", u.Username, "alice")
	}
	if u.PasswordHash != "hashed-pw" {
		t.Errorf("PasswordHash = %q, want %q", u.PasswordHash, "hashed-pw")
	}
	if u.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
}

func TestCreateUser_IncrementsCount(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	if _, err := CreateUser(context.Background(), db, "alice", "h"); err != nil {
		t.Fatalf("create first: %v", err)
	}
	if _, err := CreateUser(context.Background(), db, "bob", "h"); err != nil {
		t.Fatalf("create second: %v", err)
	}
	n, err := CountUsers(context.Background(), db)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("got %d, want 2", n)
	}
}

func TestGetUserByUsername_AndID_RoundTrip(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	created, err := CreateUser(context.Background(), db, "alice", "hashed-pw")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	byName, err := GetUserByUsername(context.Background(), db, "alice")
	if err != nil {
		t.Fatalf("get by username: %v", err)
	}
	if byName.ID != created.ID {
		t.Errorf("by username ID = %q, want %q", byName.ID, created.ID)
	}

	byID, err := GetUserByID(context.Background(), db, created.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if byID.Username != created.Username {
		t.Errorf("by id Username = %q, want %q", byID.Username, created.Username)
	}
	if byID.PasswordHash != created.PasswordHash {
		t.Errorf("by id PasswordHash = %q, want %q", byID.PasswordHash, created.PasswordHash)
	}
}

func TestGetUserByUsername_MissingReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	_, err := GetUserByUsername(context.Background(), db, "ghost")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got err = %v, want ErrNotFound", err)
	}
}

func TestGetUserByID_MissingReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	_, err := GetUserByID(context.Background(), db, "01ARYZ6S41YZDEXXXXXXX")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got err = %v, want ErrNotFound", err)
	}
}

func TestCreateUser_DuplicateUsernameErrors(t *testing.T) {
	t.Parallel()
	db := testDB(t)

	if _, err := CreateUser(context.Background(), db, "alice", "h"); err != nil {
		t.Fatalf("create first: %v", err)
	}
	if _, err := CreateUser(context.Background(), db, "alice", "h"); err == nil {
		t.Error("duplicate username: got nil error, want non-nil")
	}
}
