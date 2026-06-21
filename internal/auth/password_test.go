package auth

import (
	"errors"
	"strings"
	"testing"

	"github.com/ijaihundal/ctrlroom/internal/config"
)

func testConfig() *config.Config {
	return &config.Config{
		Env:           "dev",
		SessionTTL:    0,
		Argon2Memory:  1024,
		Argon2Time:    1,
		Argon2Threads: 1,
	}
}

func TestHash_ProducesArgon2idEncoding(t *testing.T) {
	t.Parallel()
	cfg := testConfig()

	got, err := Hash("password123", cfg)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if got == "" {
		t.Fatal("hash is empty")
	}
	if !strings.HasPrefix(got, "$argon2id$") {
		t.Errorf("hash = %q, want prefix $argon2id$", got)
	}
}

func TestVerify_CorrectPassword(t *testing.T) {
	t.Parallel()
	cfg := testConfig()

	encoded, err := Hash("correct-horse-battery-staple", cfg)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	ok, err := Verify(encoded, "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Error("verify returned false for correct password")
	}
}

func TestVerify_WrongPassword(t *testing.T) {
	t.Parallel()
	cfg := testConfig()

	encoded, err := Hash("right-password", cfg)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	ok, err := Verify(encoded, "totally-different")
	if err != nil {
		t.Fatalf("verify err = %v, want nil", err)
	}
	if ok {
		t.Error("verify returned true for wrong password")
	}
}

func TestVerify_MalformedHashReturnsError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		hash   string
		errMsg string
	}{
		{
			name:   "missing segment",
			hash:   "$argon2id$v=19$m=1024,t=1,p=1$AAAA",
			errMsg: "expected 6 segments",
		},
		{
			name:   "wrong algorithm",
			hash:   "$argon2d$v=19$m=1024,t=1,p=1$AAAA$BBBB",
			errMsg: "not argon2id",
		},
		{
			name:   "wrong version",
			hash:   "$argon2id$v=18$m=1024,t=1,p=1$AAAA$BBBB",
			errMsg: "unsupported version",
		},
		{
			name:   "bad params",
			hash:   "$argon2id$v=19$nope$AAAA$BBBB",
			errMsg: "parse params",
		},
		{
			name:   "bad salt b64",
			hash:   "$argon2id$v=19$m=1024,t=1,p=1$@@bad@@$BBBB",
			errMsg: "decode salt",
		},
		{
			name:   "bad hash b64",
			hash:   "$argon2id$v=19$m=1024,t=1,p=1$AAAA$@@bad@@",
			errMsg: "decode hash",
		},
		{
			name:   "empty",
			hash:   "",
			errMsg: "expected 6 segments",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ok, err := Verify(tc.hash, "anything")
			if ok {
				t.Error("verify returned true for malformed hash")
			}
			if err == nil {
				t.Fatal("verify returned nil error for malformed hash")
			}
			if !errors.Is(err, ErrInvalidHashFormat) {
				t.Errorf("err = %v, want ErrInvalidHashFormat wrap", err)
			}
			if !strings.Contains(err.Error(), tc.errMsg) {
				t.Errorf("err = %q, want substring %q", err.Error(), tc.errMsg)
			}
		})
	}
}

func TestVerify_WrappedErrInvalidHashFormat(t *testing.T) {
	t.Parallel()
	_, err := Verify("garbage", "anything")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidHashFormat) {
		t.Errorf("err = %v, want errors.Is ErrInvalidHashFormat", err)
	}
}

func TestHash_RandomSaltProducesDifferentOutput(t *testing.T) {
	t.Parallel()
	cfg := testConfig()

	a, err := Hash("same-password", cfg)
	if err != nil {
		t.Fatalf("hash a: %v", err)
	}
	b, err := Hash("same-password", cfg)
	if err != nil {
		t.Fatalf("hash b: %v", err)
	}
	if a == b {
		t.Error("two hashes of same password are identical; salt not random?")
	}

	ok, err := Verify(a, "same-password")
	if err != nil || !ok {
		t.Errorf("verify a: ok=%v err=%v", ok, err)
	}
	ok, err = Verify(b, "same-password")
	if err != nil || !ok {
		t.Errorf("verify b: ok=%v err=%v", ok, err)
	}
}
