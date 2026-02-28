package auth

import (
	"crypto/md5"
	"encoding/hex"
	"net/http/httptest"
	"testing"
)

func TestValidateSubsonicAuthPasswordPlainAndEnc(t *testing.T) {
	creds := Credentials{Username: "u1", Password: "p1"}

	reqPlain := httptest.NewRequest("GET", "/rest/ping.view?u=u1&p=p1", nil)
	if err := ValidateSubsonicAuth(reqPlain, creds); err != nil {
		t.Fatalf("plain password auth failed: %v", err)
	}

	reqEnc := httptest.NewRequest("GET", "/rest/ping.view?u=u1&p=enc:7031", nil) // "p1"
	if err := ValidateSubsonicAuth(reqEnc, creds); err != nil {
		t.Fatalf("enc password auth failed: %v", err)
	}
}

func TestValidateSubsonicAuthToken(t *testing.T) {
	creds := Credentials{Username: "u1", Password: "p1"}
	salt := "abc123"
	sum := md5.Sum([]byte(creds.Password + salt))
	token := hex.EncodeToString(sum[:])

	req := httptest.NewRequest("GET", "/rest/ping.view?u=u1&t="+token+"&s="+salt, nil)
	if err := ValidateSubsonicAuth(req, creds); err != nil {
		t.Fatalf("token auth failed: %v", err)
	}
}

func TestValidateSubsonicAuthFailures(t *testing.T) {
	creds := Credentials{Username: "u1", Password: "p1"}

	reqBadUser := httptest.NewRequest("GET", "/rest/ping.view?u=wrong&p=p1", nil)
	if err := ValidateSubsonicAuth(reqBadUser, creds); err == nil {
		t.Fatalf("expected invalid username error")
	}

	reqBadEnc := httptest.NewRequest("GET", "/rest/ping.view?u=u1&p=enc:zz", nil)
	if err := ValidateSubsonicAuth(reqBadEnc, creds); err == nil {
		t.Fatalf("expected invalid enc password error")
	}

	reqMissingCreds := httptest.NewRequest("GET", "/rest/ping.view?u=u1", nil)
	if err := ValidateSubsonicAuth(reqMissingCreds, creds); err == nil {
		t.Fatalf("expected missing credentials error")
	}

	reqBadToken := httptest.NewRequest("GET", "/rest/ping.view?u=u1&t=deadbeef&s=salt", nil)
	if err := ValidateSubsonicAuth(reqBadToken, creds); err == nil {
		t.Fatalf("expected invalid token error")
	}
}
