package rbac

import "testing"

func TestSignAndVerifyToken(t *testing.T) {
	secret := []byte("test-secret")
	claims := Claims{
		Subject:   "user-1",
		Perms:     PermVote | PermCreateText,
		ExpiresAt: 4102444800, // 2100-01-01
	}

	token, err := SignToken(claims, secret)
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}

	got, err := VerifyToken(token, secret)
	if err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}
	if got != claims {
		t.Fatalf("VerifyToken = %+v, want %+v", got, claims)
	}
}

func TestVerifyTokenRejectsBadSignature(t *testing.T) {
	token, err := SignToken(Claims{Subject: "user-1", ExpiresAt: 4102444800}, []byte("secret-a"))
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}

	if _, err := VerifyToken(token, []byte("secret-b")); err != ErrInvalidToken {
		t.Fatalf("VerifyToken with wrong secret = %v, want ErrInvalidToken", err)
	}
}

func TestVerifyTokenRejectsExpired(t *testing.T) {
	token, err := SignToken(Claims{Subject: "user-1", ExpiresAt: 1}, []byte("secret"))
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}

	if _, err := VerifyToken(token, []byte("secret")); err != ErrExpiredToken {
		t.Fatalf("VerifyToken with expired token = %v, want ErrExpiredToken", err)
	}
}

func TestAllows(t *testing.T) {
	root := Claims{Root: true}
	if !root.Allows(ActionEditText) {
		t.Fatal("root claims should allow every action")
	}

	scoped := Claims{Perms: PermVote}
	if !scoped.Allows(ActionVote) {
		t.Fatal("scoped claims with PermVote should allow ActionVote")
	}
	if scoped.Allows(ActionCreateText) {
		t.Fatal("scoped claims without PermCreateText should not allow ActionCreateText")
	}
}
