package password

import "testing"

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "correct horse battery staple" {
		t.Fatal("password was not hashed")
	}
	ok, err := VerifyPassword(hash, "correct horse battery staple")
	if err != nil || !ok {
		t.Fatalf("VerifyPassword() = %v, %v", ok, err)
	}
	ok, err = VerifyPassword(hash, "incorrect password")
	if err != nil || ok {
		t.Fatalf("VerifyPassword(incorrect) = %v, %v", ok, err)
	}
}

func TestValidatePasswordRejectsShortPassword(t *testing.T) {
	if err := ValidatePassword("too-short"); err == nil {
		t.Fatal("ValidatePassword() error = nil")
	}
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	if _, err := VerifyPassword("not-a-hash", "password"); err == nil {
		t.Fatal("VerifyPassword() error = nil")
	}
}
