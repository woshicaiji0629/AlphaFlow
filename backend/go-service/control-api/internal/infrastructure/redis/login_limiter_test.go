package redis

import "testing"

func TestKeyDoesNotExposeIdentity(t *testing.T) {
	got := key("email", "admin@example.com")
	if got == "" || got == "af:control:login:email:admin@example.com" {
		t.Fatalf("unsafe key %q", got)
	}
}
