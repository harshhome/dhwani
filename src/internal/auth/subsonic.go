package auth

import (
	"crypto/md5"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
)

type Credentials struct {
	Username string
	Password string
}

func ValidateSubsonicAuth(r *http.Request, creds Credentials) error {
	q := r.URL.Query()
	u := q.Get("u")
	if subtle.ConstantTimeCompare([]byte(u), []byte(creds.Username)) != 1 {
		return fmt.Errorf("invalid username")
	}

	if p := q.Get("p"); p != "" {
		provided, err := decodePasswordParam(p)
		if err != nil {
			return err
		}
		if subtle.ConstantTimeCompare([]byte(provided), []byte(creds.Password)) != 1 {
			return fmt.Errorf("invalid password")
		}
		return nil
	}

	t := q.Get("t")
	s := q.Get("s")
	if t == "" || s == "" {
		return fmt.Errorf("missing credentials: provide p or t+s")
	}

	sum := md5.Sum([]byte(creds.Password + s))
	expected := hex.EncodeToString(sum[:])
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(t)), []byte(expected)) != 1 {
		return fmt.Errorf("invalid token")
	}
	return nil
}

func decodePasswordParam(p string) (string, error) {
	if strings.HasPrefix(p, "enc:") {
		h := strings.TrimPrefix(p, "enc:")
		b, err := hex.DecodeString(h)
		if err != nil {
			return "", fmt.Errorf("invalid enc password")
		}
		return string(b), nil
	}
	return p, nil
}
