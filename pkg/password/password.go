// Package password hashes with bcrypt and still verifies the scrypt hashes written
// by the fmctest demo ("scrypt$N$r$p$saltB64$hashB64"), so existing users can log
// in; the caller re-hashes those to bcrypt after a successful login.
package password

import (
	"crypto/subtle"
	"encoding/base64"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/scrypt"
)

func Hash(plain string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// Verify reports whether plain matches stored, and whether stored is a legacy
// hash that should be upgraded.
func Verify(plain, stored string) (ok bool, legacy bool) {
	if strings.HasPrefix(stored, "scrypt$") {
		return verifyScrypt(plain, stored), true
	}
	return bcrypt.CompareHashAndPassword([]byte(stored), []byte(plain)) == nil, false
}

func verifyScrypt(plain, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 6 {
		return false
	}
	n, err1 := strconv.Atoi(parts[1])
	r, err2 := strconv.Atoi(parts[2])
	p, err3 := strconv.Atoi(parts[3])
	salt, err4 := base64.StdEncoding.DecodeString(parts[4])
	expected, err5 := base64.StdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil || len(expected) == 0 {
		return false
	}
	actual, err := scrypt.Key([]byte(plain), salt, n, r, p, len(expected))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

// dummyHash keeps login timing the same for unknown emails.
var dummyHash, _ = Hash("fms-dummy-password-for-timing")

// VerifyDummy burns the same time as a real bcrypt check.
func VerifyDummy(plain string) {
	_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(plain))
}
