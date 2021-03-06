// Copyright 2018 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/jwt"
	"github.com/nats-io/nkeys"
)

const (
	uJWT = "eyJ0eXAiOiJqd3QiLCJhbGciOiJlZDI1NTE5In0.eyJqdGkiOiJRVzRWWktISEJCUkFaSkFWREg3UjVDSk1RQ1pHWDZJM1FJWEJSMkdWSjRHSVRMRlJRMlpBIiwiaWF0IjoxNTQyMzg1NjMxLCJpc3MiOiJBQ1E1VkpLS1dEM0s1QzdSVkFFMjJNT1hESkFNTEdFTUZJM1NDR1JWUlpKSlFUTU9QTjMzQlhVSyIsIm5hbWUiOiJkZXJlayIsInN1YiI6IlVEMkZMTEdGRVJRVlFRM1NCS09OTkcyUU1JTVRaUUtLTFRVM0FWRzVJM0VRRUZIQlBHUEUyWFFTIiwidHlwZSI6InVzZXIiLCJuYXRzIjp7InB1YiI6e30sInN1YiI6e319fQ.6PmFNn3x0AH3V05oemO28riP63+QTvk9g/Qtt6wBcXJqgW6YSVxk6An1MjvTn1tH7S9tJ0zOIGp7/OLjP1tbBQ"
	aJWT = "eyJ0eXAiOiJqd3QiLCJhbGciOiJlZDI1NTE5In0.eyJqdGkiOiJJSTdKSU5JUENVWTZEU1JDSUpZT1daR0k0UlRGNUdCNjVZUUtSNE9RVlBCQlpBNFhCQlhRIiwiaWF0IjoxNTQyMzMxNzgwLCJpc3MiOiJPRDJXMkk0TVZSQTVUR1pMWjJBRzZaSEdWTDNPVEtGV1FKRklYNFROQkVSMjNFNlA0NlMzNDVZWSIsIm5hbWUiOiJmb28iLCJzdWIiOiJBQ1E1VkpLS1dEM0s1QzdSVkFFMjJNT1hESkFNTEdFTUZJM1NDR1JWUlpKSlFUTU9QTjMzQlhVSyIsInR5cGUiOiJhY2NvdW50IiwibmF0cyI6e319.Dg2A1NCJWvXhBQZN9QNHAq1KqsFIKxzLhYvD5yH0DYZPC0gXtdhLkwJ5uiooki6YvzR8UNQZ9XuWgDpNpwryDg"
)

var (
	uSeed = []byte("SUAIO3FHUX5PNV2LQIIP7TZ3N4L7TX3W53MQGEIVYFIGA635OZCKEYHFLM")
	oSeed = []byte("SOAL7GTNI66CTVVNXBNQMG6V2HTDRWC3HGEP7D2OUTWNWSNYZDXWFOX4SU")
	aSeed = []byte("SAANRM6JVDEYZTR6DXCWUSDDJHGOHAFITXEQBSEZSY5JENTDVRZ6WNKTTY")
)

func opTrustBasicSetup() *Server {
	kp, _ := nkeys.FromSeed(oSeed)
	pub, _ := kp.PublicKey()
	opts := defaultServerOptions
	opts.TrustedNkeys = []string{string(pub)}
	s, _, _, _ := rawSetup(opts)
	return s
}

func buildMemAccResolver(s *Server) {
	kp, _ := nkeys.FromSeed(aSeed)
	pub, _ := kp.PublicKey()
	mr := &MemAccResolver{}
	mr.Store(string(pub), aJWT)
	s.mu.Lock()
	s.accResolver = mr
	s.mu.Unlock()
}

func addAccountToMemResolver(s *Server, pub, jwt string) {
	s.mu.Lock()
	s.accResolver.(*MemAccResolver).Store(pub, jwt)
	s.mu.Unlock()
}

func TestJWTUser(t *testing.T) {
	s := opTrustBasicSetup()
	defer s.Shutdown()

	// Check to make sure we would have an authTimer
	if !s.info.AuthRequired {
		t.Fatalf("Expect the server to require auth")
	}

	c, cr, _ := newClientForServer(s)

	// Don't send jwt field, should fail.
	go c.parse([]byte("CONNECT {\"verbose\":true,\"pedantic\":true}\r\nPING\r\n"))
	l, _ := cr.ReadString('\n')
	if !strings.HasPrefix(l, "-ERR ") {
		t.Fatalf("Expected an error")
	}

	c, cr, _ = newClientForServer(s)

	// PING needed to flush the +OK/-ERR to us.
	// This should fail too since no account resolver is defined.
	cs := fmt.Sprintf("CONNECT {\"jwt\":%q,\"sig\":\"%s\",\"verbose\":true,\"pedantic\":true}\r\nPING\r\n", uJWT, "xxx")
	go c.parse([]byte(cs))
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "-ERR ") {
		t.Fatalf("Expected an error")
	}

	// Ok now let's walk through and make sure all is good.
	// We will set the account resolver by hand to a memory resolver.
	buildMemAccResolver(s)

	c, cr, l = newClientForServer(s)

	// Sign Nonce
	kp, _ := nkeys.FromSeed(uSeed)

	var info nonceInfo
	json.Unmarshal([]byte(l[5:]), &info)
	sigraw, _ := kp.Sign([]byte(info.Nonce))
	sig := base64.StdEncoding.EncodeToString(sigraw)

	// PING needed to flush the +OK/-ERR to us.
	// This should fail too since no account resolver is defined.
	cs = fmt.Sprintf("CONNECT {\"jwt\":%q,\"sig\":\"%s\",\"verbose\":true,\"pedantic\":true}\r\nPING\r\n", uJWT, sig)
	go c.parse([]byte(cs))
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "+OK") {
		t.Fatalf("Expected an OK, got: %v", l)
	}
}

func TestJWTUserBadTrusted(t *testing.T) {
	s := opTrustBasicSetup()
	defer s.Shutdown()

	// Check to make sure we would have an authTimer
	if !s.info.AuthRequired {
		t.Fatalf("Expect the server to require auth")
	}
	// Now place bad trusted key
	s.mu.Lock()
	s.trustedNkeys = []string{"bad"}
	s.mu.Unlock()

	buildMemAccResolver(s)

	c, cr, l := newClientForServer(s)

	// Sign Nonce
	kp, _ := nkeys.FromSeed(uSeed)

	var info nonceInfo
	json.Unmarshal([]byte(l[5:]), &info)
	sigraw, _ := kp.Sign([]byte(info.Nonce))
	sig := base64.StdEncoding.EncodeToString(sigraw)

	// PING needed to flush the +OK/-ERR to us.
	// This should fail too since no account resolver is defined.
	cs := fmt.Sprintf("CONNECT {\"jwt\":%q,\"sig\":\"%s\",\"verbose\":true,\"pedantic\":true}\r\nPING\r\n", uJWT, sig)
	go c.parse([]byte(cs))
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "-ERR ") {
		t.Fatalf("Expected an error")
	}
}

// Test that if a user tries to connect with an expired user JWT we do the right thing.
func TestJWTUserExpired(t *testing.T) {
	// Create a new user that we will make sure has expired.
	nkp, _ := nkeys.CreateUser()
	pub, _ := nkp.PublicKey()
	nuc := jwt.NewUserClaims(string(pub))
	nuc.IssuedAt = time.Now().Add(-10 * time.Second).Unix()
	nuc.Expires = time.Now().Add(-2 * time.Second).Unix()

	akp, _ := nkeys.FromSeed(aSeed)
	jwt, err := nuc.Encode(akp)
	if err != nil {
		t.Fatalf("Error generating user JWT: %v", err)
	}

	s := opTrustBasicSetup()
	defer s.Shutdown()
	buildMemAccResolver(s)

	c, cr, l := newClientForServer(s)

	// Sign Nonce
	var info nonceInfo
	json.Unmarshal([]byte(l[5:]), &info)
	sigraw, _ := nkp.Sign([]byte(info.Nonce))
	sig := base64.StdEncoding.EncodeToString(sigraw)

	// PING needed to flush the +OK/-ERR to us.
	// This should fail too since no account resolver is defined.
	cs := fmt.Sprintf("CONNECT {\"jwt\":%q,\"sig\":\"%s\",\"verbose\":true,\"pedantic\":true}\r\nPING\r\n", jwt, sig)
	go c.parse([]byte(cs))
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "-ERR ") {
		t.Fatalf("Expected an error")
	}
}

func TestJWTUserExpiresAfterConnect(t *testing.T) {
	// Create a new user that we will make sure has expired.
	nkp, _ := nkeys.CreateUser()
	pub, _ := nkp.PublicKey()
	nuc := jwt.NewUserClaims(string(pub))
	nuc.IssuedAt = time.Now().Unix()
	nuc.Expires = time.Now().Add(time.Second).Unix()

	akp, _ := nkeys.FromSeed(aSeed)
	jwt, err := nuc.Encode(akp)
	if err != nil {
		t.Fatalf("Error generating user JWT: %v", err)
	}

	s := opTrustBasicSetup()
	defer s.Shutdown()
	buildMemAccResolver(s)

	c, cr, l := newClientForServer(s)

	// Sign Nonce
	var info nonceInfo
	json.Unmarshal([]byte(l[5:]), &info)
	sigraw, _ := nkp.Sign([]byte(info.Nonce))
	sig := base64.StdEncoding.EncodeToString(sigraw)

	// PING needed to flush the +OK/-ERR to us.
	// This should fail too since no account resolver is defined.
	cs := fmt.Sprintf("CONNECT {\"jwt\":%q,\"sig\":\"%s\",\"verbose\":true,\"pedantic\":true}\r\nPING\r\n", jwt, sig)
	go c.parse([]byte(cs))
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "+OK") {
		t.Fatalf("Expected an OK, got: %v", l)
	}
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "PONG") {
		t.Fatalf("Expected a PONG")
	}

	// Now we should expire after 1 second or so.
	time.Sleep(time.Second)

	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "-ERR ") {
		t.Fatalf("Expected an error")
	}
	if !strings.Contains(l, "Expired") {
		t.Fatalf("Expected 'Expired' to be in the error")
	}
}

func TestJWTUserPermissionClaims(t *testing.T) {
	nkp, _ := nkeys.CreateUser()
	pub, _ := nkp.PublicKey()
	nuc := jwt.NewUserClaims(string(pub))

	nuc.Permissions.Pub.Allow.Add("foo")
	nuc.Permissions.Pub.Allow.Add("bar")
	nuc.Permissions.Pub.Deny.Add("baz")
	nuc.Permissions.Sub.Allow.Add("foo")
	nuc.Permissions.Sub.Allow.Add("bar")
	nuc.Permissions.Sub.Deny.Add("baz")

	akp, _ := nkeys.FromSeed(aSeed)
	jwt, err := nuc.Encode(akp)
	if err != nil {
		t.Fatalf("Error generating user JWT: %v", err)
	}

	s := opTrustBasicSetup()
	defer s.Shutdown()
	buildMemAccResolver(s)

	c, cr, l := newClientForServer(s)

	// Sign Nonce
	var info nonceInfo
	json.Unmarshal([]byte(l[5:]), &info)
	sigraw, _ := nkp.Sign([]byte(info.Nonce))
	sig := base64.StdEncoding.EncodeToString(sigraw)

	// PING needed to flush the +OK/-ERR to us.
	// This should fail too since no account resolver is defined.
	cs := fmt.Sprintf("CONNECT {\"jwt\":%q,\"sig\":\"%s\",\"verbose\":true,\"pedantic\":true}\r\nPING\r\n", jwt, sig)
	go c.parse([]byte(cs))
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "+OK") {
		t.Fatalf("Expected an OK, got: %v", l)
	}
	// Now check client to make sure permissions transferred.
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.perms == nil {
		t.Fatalf("Expected client permissions to be set")
	}

	if lpa := c.perms.pub.allow.Count(); lpa != 2 {
		t.Fatalf("Expected 2 publish allow subjects, got %d", lpa)
	}
	if lpd := c.perms.pub.deny.Count(); lpd != 1 {
		t.Fatalf("Expected 1 publish deny subjects, got %d", lpd)
	}
	if lsa := c.perms.sub.allow.Count(); lsa != 2 {
		t.Fatalf("Expected 2 subscribe allow subjects, got %d", lsa)
	}
	if lsd := c.perms.sub.deny.Count(); lsd != 1 {
		t.Fatalf("Expected 1 subscribe deny subjects, got %d", lsd)
	}
}

func TestJWTAccountExpired(t *testing.T) {
	s := opTrustBasicSetup()
	defer s.Shutdown()
	buildMemAccResolver(s)

	okp, _ := nkeys.FromSeed(oSeed)

	// Create an account that will be expired.
	akp, _ := nkeys.CreateAccount()
	apub, _ := akp.PublicKey()
	nac := jwt.NewAccountClaims(string(apub))
	nac.IssuedAt = time.Now().Add(-10 * time.Second).Unix()
	nac.Expires = time.Now().Add(-2 * time.Second).Unix()
	ajwt, err := nac.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}

	addAccountToMemResolver(s, string(apub), ajwt)

	// Create a new user
	nkp, _ := nkeys.CreateUser()
	pub, _ := nkp.PublicKey()
	nuc := jwt.NewUserClaims(string(pub))
	jwt, err := nuc.Encode(akp)
	if err != nil {
		t.Fatalf("Error generating user JWT: %v", err)
	}

	c, cr, l := newClientForServer(s)

	// Sign Nonce
	var info nonceInfo
	json.Unmarshal([]byte(l[5:]), &info)
	sigraw, _ := nkp.Sign([]byte(info.Nonce))
	sig := base64.StdEncoding.EncodeToString(sigraw)

	// PING needed to flush the +OK/-ERR to us.
	// This should fail since the account is expired.
	cs := fmt.Sprintf("CONNECT {\"jwt\":%q,\"sig\":\"%s\",\"verbose\":true,\"pedantic\":true}\r\nPING\r\n", jwt, sig)
	go c.parse([]byte(cs))
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "-ERR ") {
		t.Fatalf("Expected an error")
	}
}

func TestJWTAccountExpiresAfterConnect(t *testing.T) {
	s := opTrustBasicSetup()
	defer s.Shutdown()
	buildMemAccResolver(s)

	okp, _ := nkeys.FromSeed(oSeed)

	// Create an account that will expire.
	akp, _ := nkeys.CreateAccount()
	apub, _ := akp.PublicKey()
	nac := jwt.NewAccountClaims(string(apub))
	nac.IssuedAt = time.Now().Unix()
	nac.Expires = time.Now().Add(time.Second).Unix()
	ajwt, err := nac.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}

	addAccountToMemResolver(s, string(apub), ajwt)

	// Create a new user
	nkp, _ := nkeys.CreateUser()
	pub, _ := nkp.PublicKey()
	nuc := jwt.NewUserClaims(string(pub))
	jwt, err := nuc.Encode(akp)
	if err != nil {
		t.Fatalf("Error generating user JWT: %v", err)
	}

	c, cr, l := newClientForServer(s)

	// Sign Nonce
	var info nonceInfo
	json.Unmarshal([]byte(l[5:]), &info)
	sigraw, _ := nkp.Sign([]byte(info.Nonce))
	sig := base64.StdEncoding.EncodeToString(sigraw)

	// PING needed to flush the +OK/-ERR to us.
	cs := fmt.Sprintf("CONNECT {\"jwt\":%q,\"sig\":\"%s\",\"verbose\":true,\"pedantic\":true}\r\nPING\r\n", jwt, sig)
	go c.parse([]byte(cs))
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "+OK") {
		t.Fatalf("Expected an OK, got: %v", l)
	}
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "PONG") {
		t.Fatalf("Expected a PONG")
	}

	// Now we should expire after 1 second or so.
	time.Sleep(time.Second)

	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "-ERR ") {
		t.Fatalf("Expected an error")
	}
	if !strings.Contains(l, "Expired") {
		t.Fatalf("Expected 'Expired' to be in the error")
	}

	// Now make sure that accounts that have expired return an error.
	c, cr, l = newClientForServer(s)

	// Sign Nonce
	json.Unmarshal([]byte(l[5:]), &info)
	sigraw, _ = nkp.Sign([]byte(info.Nonce))
	sig = base64.StdEncoding.EncodeToString(sigraw)

	// PING needed to flush the +OK/-ERR to us.
	cs = fmt.Sprintf("CONNECT {\"jwt\":%q,\"sig\":\"%s\",\"verbose\":true,\"pedantic\":true}\r\nPING\r\n", jwt, sig)
	go c.parse([]byte(cs))
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "-ERR ") {
		t.Fatalf("Expected an error")
	}
}

func TestJWTAccountRenew(t *testing.T) {
	s := opTrustBasicSetup()
	defer s.Shutdown()
	buildMemAccResolver(s)

	okp, _ := nkeys.FromSeed(oSeed)

	// Create an account that has expired.
	akp, _ := nkeys.CreateAccount()
	apub, _ := akp.PublicKey()
	nac := jwt.NewAccountClaims(string(apub))
	nac.IssuedAt = time.Now().Add(-10 * time.Second).Unix()
	nac.Expires = time.Now().Add(-2 * time.Second).Unix()
	ajwt, err := nac.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}

	addAccountToMemResolver(s, string(apub), ajwt)

	// Create a new user
	nkp, _ := nkeys.CreateUser()
	pub, _ := nkp.PublicKey()
	nuc := jwt.NewUserClaims(string(pub))
	ujwt, err := nuc.Encode(akp)
	if err != nil {
		t.Fatalf("Error generating user JWT: %v", err)
	}

	c, cr, l := newClientForServer(s)

	// Sign Nonce
	var info nonceInfo
	json.Unmarshal([]byte(l[5:]), &info)
	sigraw, _ := nkp.Sign([]byte(info.Nonce))
	sig := base64.StdEncoding.EncodeToString(sigraw)

	// PING needed to flush the +OK/-ERR to us.
	// This should fail since the account is expired.
	cs := fmt.Sprintf("CONNECT {\"jwt\":%q,\"sig\":\"%s\",\"verbose\":true,\"pedantic\":true}\r\nPING\r\n", ujwt, sig)
	go c.parse([]byte(cs))
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "-ERR ") {
		t.Fatalf("Expected an error")
	}

	// Now update with new expiration
	nac.IssuedAt = time.Now().Unix()
	nac.Expires = time.Now().Add(5 * time.Second).Unix()
	ajwt, err = nac.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}

	// Update the account
	addAccountToMemResolver(s, string(apub), ajwt)
	acc := s.LookupAccount(string(apub))
	if acc == nil {
		t.Fatalf("Expected to retrive the account")
	}
	s.updateAccountClaims(acc, nac)

	// Now make sure we can connect.
	c, cr, l = newClientForServer(s)

	// Sign Nonce
	json.Unmarshal([]byte(l[5:]), &info)
	sigraw, _ = nkp.Sign([]byte(info.Nonce))
	sig = base64.StdEncoding.EncodeToString(sigraw)

	// PING needed to flush the +OK/-ERR to us.
	// This should fail too since no account resolver is defined.
	cs = fmt.Sprintf("CONNECT {\"jwt\":%q,\"sig\":\"%s\",\"verbose\":true,\"pedantic\":true}\r\nPING\r\n", ujwt, sig)
	go c.parse([]byte(cs))
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "+OK") {
		t.Fatalf("Expected an OK, got: %v", l)
	}
}

func TestJWTAccountRenewFromResolver(t *testing.T) {
	s := opTrustBasicSetup()
	defer s.Shutdown()
	buildMemAccResolver(s)

	okp, _ := nkeys.FromSeed(oSeed)

	// Create an account that has expired.
	akp, _ := nkeys.CreateAccount()
	apub, _ := akp.PublicKey()
	nac := jwt.NewAccountClaims(string(apub))
	nac.IssuedAt = time.Now().Add(-10 * time.Second).Unix()
	nac.Expires = time.Now().Add(time.Second).Unix()
	ajwt, err := nac.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}

	addAccountToMemResolver(s, string(apub), ajwt)
	// Force it to be loaded by the server and start the expiration timer.
	acc := s.LookupAccount(string(apub))
	if acc == nil {
		t.Fatalf("Could not retrieve account for %q", apub)
	}

	// Create a new user
	nkp, _ := nkeys.CreateUser()
	pub, _ := nkp.PublicKey()
	nuc := jwt.NewUserClaims(string(pub))
	ujwt, err := nuc.Encode(akp)
	if err != nil {
		t.Fatalf("Error generating user JWT: %v", err)
	}

	c, cr, l := newClientForServer(s)

	// Sign Nonce
	var info nonceInfo
	json.Unmarshal([]byte(l[5:]), &info)
	sigraw, _ := nkp.Sign([]byte(info.Nonce))
	sig := base64.StdEncoding.EncodeToString(sigraw)

	// Wait for expiration.
	time.Sleep(time.Second)

	// PING needed to flush the +OK/-ERR to us.
	// This should fail since the account is expired.
	cs := fmt.Sprintf("CONNECT {\"jwt\":%q,\"sig\":\"%s\",\"verbose\":true,\"pedantic\":true}\r\nPING\r\n", ujwt, sig)
	go c.parse([]byte(cs))
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "-ERR ") {
		t.Fatalf("Expected an error")
	}

	// Now update with new expiration
	nac.IssuedAt = time.Now().Unix()
	nac.Expires = time.Now().Add(5 * time.Second).Unix()
	ajwt, err = nac.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}

	// Update the account
	addAccountToMemResolver(s, string(apub), ajwt)
	// Make sure the too quick update suppression does not bite us.
	acc.updated = time.Now().Add(-1 * time.Hour)

	// Do not update the account directly. The resolver should
	// happen automatically.

	// Now make sure we can connect.
	c, cr, l = newClientForServer(s)

	// Sign Nonce
	json.Unmarshal([]byte(l[5:]), &info)
	sigraw, _ = nkp.Sign([]byte(info.Nonce))
	sig = base64.StdEncoding.EncodeToString(sigraw)

	// PING needed to flush the +OK/-ERR to us.
	// This should fail too since no account resolver is defined.
	cs = fmt.Sprintf("CONNECT {\"jwt\":%q,\"sig\":\"%s\",\"verbose\":true,\"pedantic\":true}\r\nPING\r\n", ujwt, sig)
	go c.parse([]byte(cs))
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "+OK") {
		t.Fatalf("Expected an OK, got: %v", l)
	}
}

func TestJWTAccountBasicImportExport(t *testing.T) {
	s := opTrustBasicSetup()
	defer s.Shutdown()
	buildMemAccResolver(s)

	okp, _ := nkeys.FromSeed(oSeed)

	// Create accounts and imports/exports.
	fooKP, _ := nkeys.CreateAccount()
	fooPub, _ := fooKP.PublicKey()
	fooAC := jwt.NewAccountClaims(string(fooPub))

	// Now create Exports.
	streamExport := &jwt.Export{Subject: "foo", Type: jwt.Stream}
	streamExport2 := &jwt.Export{Subject: "private", Type: jwt.Stream, TokenReq: true}
	serviceExport := &jwt.Export{Subject: "req.echo", Type: jwt.Service, TokenReq: true}
	serviceExport2 := &jwt.Export{Subject: "req.add", Type: jwt.Service, TokenReq: true}

	fooAC.Exports.Add(streamExport, streamExport2, serviceExport, serviceExport2)
	fooJWT, err := fooAC.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}

	addAccountToMemResolver(s, string(fooPub), fooJWT)

	acc := s.LookupAccount(string(fooPub))
	if acc == nil {
		t.Fatalf("Expected to retrieve the account")
	}

	// Check to make sure exports transferred over.
	if les := len(acc.exports.streams); les != 2 {
		t.Fatalf("Expected exports streams len of 2, got %d", les)
	}
	if les := len(acc.exports.services); les != 2 {
		t.Fatalf("Expected exports services len of 2, got %d", les)
	}
	_, ok := acc.exports.streams["foo"]
	if !ok {
		t.Fatalf("Expected to map a stream export")
	}
	se, ok := acc.exports.services["req.echo"]
	if !ok || se == nil {
		t.Fatalf("Expected to map a service export")
	}
	if !se.tokenReq {
		t.Fatalf("Expected the service export to require tokens")
	}

	barKP, _ := nkeys.CreateAccount()
	barPub, _ := barKP.PublicKey()
	barAC := jwt.NewAccountClaims(string(barPub))

	streamImport := &jwt.Import{Account: string(fooPub), Subject: "foo", To: "import.foo", Type: jwt.Stream}
	serviceImport := &jwt.Import{Account: string(fooPub), Subject: "req.echo", Type: jwt.Service}
	barAC.Imports.Add(streamImport, serviceImport)
	barJWT, err := barAC.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}
	addAccountToMemResolver(s, string(barPub), barJWT)

	acc = s.LookupAccount(string(barPub))
	if acc == nil {
		t.Fatalf("Expected to retrieve the account")
	}
	if les := len(acc.imports.streams); les != 1 {
		t.Fatalf("Expected imports streams len of 1, got %d", les)
	}
	// Our service import should have failed without a token.
	if les := len(acc.imports.services); les != 0 {
		t.Fatalf("Expected imports services len of 0, got %d", les)
	}

	// Now add in a bad activation token.
	barAC = jwt.NewAccountClaims(string(barPub))
	serviceImport = &jwt.Import{Account: string(fooPub), Subject: "req.echo", Token: "not a token", Type: jwt.Service}
	barAC.Imports.Add(serviceImport)
	barJWT, err = barAC.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}
	addAccountToMemResolver(s, string(barPub), barJWT)

	s.updateAccountClaims(acc, barAC)

	// Our service import should have failed with a bad token.
	if les := len(acc.imports.services); les != 0 {
		t.Fatalf("Expected imports services len of 0, got %d", les)
	}

	// Now make a correct one.
	barAC = jwt.NewAccountClaims(string(barPub))
	serviceImport = &jwt.Import{Account: string(fooPub), Subject: "req.echo", Type: jwt.Service}

	activation := jwt.NewActivationClaims(string(barPub))
	activation.ImportSubject = "req.echo"
	activation.ImportType = jwt.Service
	actJWT, err := activation.Encode(fooKP)
	if err != nil {
		t.Fatalf("Error generating activation token: %v", err)
	}
	serviceImport.Token = actJWT
	barAC.Imports.Add(serviceImport)
	barJWT, err = barAC.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}
	addAccountToMemResolver(s, string(barPub), barJWT)
	s.updateAccountClaims(acc, barAC)
	// Our service import should have succeeded.
	if les := len(acc.imports.services); les != 1 {
		t.Fatalf("Expected imports services len of 1, got %d", les)
	}

	// Now test url
	barAC = jwt.NewAccountClaims(string(barPub))
	serviceImport = &jwt.Import{Account: string(fooPub), Subject: "req.add", Type: jwt.Service}

	activation = jwt.NewActivationClaims(string(barPub))
	activation.ImportSubject = "req.add"
	activation.ImportType = jwt.Service
	actJWT, err = activation.Encode(fooKP)
	if err != nil {
		t.Fatalf("Error generating activation token: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(actJWT))
	}))
	defer ts.Close()

	serviceImport.Token = ts.URL
	barAC.Imports.Add(serviceImport)
	barJWT, err = barAC.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}
	addAccountToMemResolver(s, string(barPub), barJWT)
	s.updateAccountClaims(acc, barAC)
	// Our service import should have succeeded. Should be the only one since we reset.
	if les := len(acc.imports.services); les != 1 {
		t.Fatalf("Expected imports services len of 1, got %d", les)
	}

	// Now streams
	barAC = jwt.NewAccountClaims(string(barPub))
	streamImport = &jwt.Import{Account: string(fooPub), Subject: "private", To: "import.private", Type: jwt.Stream}

	barAC.Imports.Add(streamImport)
	barJWT, err = barAC.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}
	addAccountToMemResolver(s, string(barPub), barJWT)
	s.updateAccountClaims(acc, barAC)
	// Our stream import should have not succeeded.
	if les := len(acc.imports.streams); les != 0 {
		t.Fatalf("Expected imports services len of 0, got %d", les)
	}

	// Now add in activation.
	barAC = jwt.NewAccountClaims(string(barPub))
	streamImport = &jwt.Import{Account: string(fooPub), Subject: "private", To: "import.private", Type: jwt.Stream}

	activation = jwt.NewActivationClaims(string(barPub))
	activation.ImportSubject = "private"
	activation.ImportType = jwt.Stream
	actJWT, err = activation.Encode(fooKP)
	if err != nil {
		t.Fatalf("Error generating activation token: %v", err)
	}
	streamImport.Token = actJWT
	barAC.Imports.Add(streamImport)
	barJWT, err = barAC.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}
	addAccountToMemResolver(s, string(barPub), barJWT)
	s.updateAccountClaims(acc, barAC)
	// Our stream import should have not succeeded.
	if les := len(acc.imports.streams); les != 1 {
		t.Fatalf("Expected imports services len of 1, got %d", les)
	}
}

func TestJWTAccountImportExportUpdates(t *testing.T) {
	s := opTrustBasicSetup()
	defer s.Shutdown()
	buildMemAccResolver(s)

	okp, _ := nkeys.FromSeed(oSeed)

	// Create accounts and imports/exports.
	fooKP, _ := nkeys.CreateAccount()
	fooPub, _ := fooKP.PublicKey()
	fooAC := jwt.NewAccountClaims(string(fooPub))
	streamExport := &jwt.Export{Subject: "foo", Type: jwt.Stream}

	fooAC.Exports.Add(streamExport)
	fooJWT, err := fooAC.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}
	addAccountToMemResolver(s, string(fooPub), fooJWT)

	barKP, _ := nkeys.CreateAccount()
	barPub, _ := barKP.PublicKey()
	barAC := jwt.NewAccountClaims(string(barPub))
	streamImport := &jwt.Import{Account: string(fooPub), Subject: "foo", To: "import", Type: jwt.Stream}

	barAC.Imports.Add(streamImport)
	barJWT, err := barAC.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}
	addAccountToMemResolver(s, string(barPub), barJWT)

	// Create a client.
	nkp, _ := nkeys.CreateUser()
	pub, _ := nkp.PublicKey()
	nuc := jwt.NewUserClaims(string(pub))
	ujwt, err := nuc.Encode(barKP)
	if err != nil {
		t.Fatalf("Error generating user JWT: %v", err)
	}

	c, cr, l := newClientForServer(s)

	// Sign Nonce
	var info nonceInfo
	json.Unmarshal([]byte(l[5:]), &info)
	sigraw, _ := nkp.Sign([]byte(info.Nonce))
	sig := base64.StdEncoding.EncodeToString(sigraw)

	// PING needed to flush the +OK/-ERR to us.
	// This should fail too since no account resolver is defined.
	cs := fmt.Sprintf("CONNECT {\"jwt\":%q,\"sig\":\"%s\",\"verbose\":true,\"pedantic\":true}\r\nSUB import.foo 1\r\nPING\r\n", ujwt, sig)
	go c.parse([]byte(cs))
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "+OK") {
		t.Fatalf("Expected an OK, got: %v", l)
	}
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "+OK") {
		t.Fatalf("Expected an OK, got: %v", l)
	}
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "PONG\r\n") {
		t.Fatalf("PONG response incorrect: %q\n", l)
	}

	checkShadow := func(expected int) {
		t.Helper()
		c.mu.Lock()
		defer c.mu.Unlock()
		sub := c.subs["1"]
		if ls := len(sub.shadow); ls != expected {
			t.Fatalf("Expected shadows to be %d, got %d", expected, ls)
		}
	}

	// We created a SUB on foo which should create a shadow subscription.
	checkShadow(1)

	// Now update bar and remove the import which should make the shadow go away.
	barAC = jwt.NewAccountClaims(string(barPub))
	barJWT, err = barAC.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}
	addAccountToMemResolver(s, string(barPub), barJWT)
	acc := s.LookupAccount(string(barPub))
	s.updateAccountClaims(acc, barAC)

	checkShadow(0)

	// Now add it back and make sure the shadow comes back.
	streamImport = &jwt.Import{Account: string(fooPub), Subject: "foo", To: "import", Type: jwt.Stream}
	barAC.Imports.Add(streamImport)
	barJWT, err = barAC.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}
	addAccountToMemResolver(s, string(barPub), barJWT)
	s.updateAccountClaims(acc, barAC)

	checkShadow(1)

	// Now change export and make sure it goes away as well. So no exports anymore.
	fooAC = jwt.NewAccountClaims(string(fooPub))
	fooJWT, err = fooAC.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}
	addAccountToMemResolver(s, string(fooPub), fooJWT)
	s.updateAccountClaims(s.LookupAccount(string(fooPub)), fooAC)

	checkShadow(0)

	// Now add it in but with permission required.
	streamExport = &jwt.Export{Subject: "foo", Type: jwt.Stream, TokenReq: true}
	fooAC.Exports.Add(streamExport)
	fooJWT, err = fooAC.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}
	addAccountToMemResolver(s, string(fooPub), fooJWT)
	s.updateAccountClaims(s.LookupAccount(string(fooPub)), fooAC)

	checkShadow(0)

	// Now put it back as normal.
	fooAC = jwt.NewAccountClaims(string(fooPub))
	streamExport = &jwt.Export{Subject: "foo", Type: jwt.Stream}
	fooAC.Exports.Add(streamExport)
	fooJWT, err = fooAC.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}
	addAccountToMemResolver(s, string(fooPub), fooJWT)
	s.updateAccountClaims(s.LookupAccount(string(fooPub)), fooAC)

	checkShadow(1)
}

func TestJWTAccountImportActivationExpires(t *testing.T) {
	s := opTrustBasicSetup()
	defer s.Shutdown()
	buildMemAccResolver(s)

	okp, _ := nkeys.FromSeed(oSeed)

	// Create accounts and imports/exports.
	fooKP, _ := nkeys.CreateAccount()
	fooPub, _ := fooKP.PublicKey()
	fooAC := jwt.NewAccountClaims(string(fooPub))
	streamExport := &jwt.Export{Subject: "foo", Type: jwt.Stream, TokenReq: true}
	fooAC.Exports.Add(streamExport)

	fooJWT, err := fooAC.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}

	addAccountToMemResolver(s, string(fooPub), fooJWT)

	acc := s.LookupAccount(string(fooPub))
	if acc == nil {
		t.Fatalf("Expected to retrieve the account")
	}

	barKP, _ := nkeys.CreateAccount()
	barPub, _ := barKP.PublicKey()
	barAC := jwt.NewAccountClaims(string(barPub))
	streamImport := &jwt.Import{Account: string(fooPub), Subject: "foo", To: "import.", Type: jwt.Stream}

	activation := jwt.NewActivationClaims(string(barPub))
	activation.ImportSubject = "foo"
	activation.ImportType = jwt.Stream
	activation.IssuedAt = time.Now().Add(-10 * time.Second).Unix()
	activation.Expires = time.Now().Add(time.Second).Unix()
	actJWT, err := activation.Encode(fooKP)
	if err != nil {
		t.Fatalf("Error generating activation token: %v", err)
	}
	streamImport.Token = actJWT
	barAC.Imports.Add(streamImport)
	barJWT, err := barAC.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}
	addAccountToMemResolver(s, string(barPub), barJWT)

	// Create a client.
	nkp, _ := nkeys.CreateUser()
	pub, _ := nkp.PublicKey()
	nuc := jwt.NewUserClaims(string(pub))
	ujwt, err := nuc.Encode(barKP)
	if err != nil {
		t.Fatalf("Error generating user JWT: %v", err)
	}

	c, cr, l := newClientForServer(s)

	// Sign Nonce
	var info nonceInfo
	json.Unmarshal([]byte(l[5:]), &info)
	sigraw, _ := nkp.Sign([]byte(info.Nonce))
	sig := base64.StdEncoding.EncodeToString(sigraw)

	// PING needed to flush the +OK/-ERR to us.
	cs := fmt.Sprintf("CONNECT {\"jwt\":%q,\"sig\":\"%s\",\"verbose\":true,\"pedantic\":true}\r\nSUB import.foo 1\r\nPING\r\n", ujwt, sig)
	go c.parse([]byte(cs))
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "+OK") {
		t.Fatalf("Expected an OK, got: %v", l)
	}
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "+OK") {
		t.Fatalf("Expected an OK, got: %v", l)
	}
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "PONG\r\n") {
		t.Fatalf("PONG response incorrect: %q\n", l)
	}

	checkShadow := func(expected int) {
		t.Helper()
		c.mu.Lock()
		defer c.mu.Unlock()
		sub := c.subs["1"]
		if ls := len(sub.shadow); ls != expected {
			t.Fatalf("Expected shadows to be %d, got %d", expected, ls)
		}
	}

	// We created a SUB on foo which should create a shadow subscription.
	checkShadow(1)

	time.Sleep(2 * time.Second)

	// Should have expired and been removed.
	checkShadow(0)
}

func TestJWTAccountLimitsSubs(t *testing.T) {
	s := opTrustBasicSetup()
	defer s.Shutdown()
	buildMemAccResolver(s)

	okp, _ := nkeys.FromSeed(oSeed)

	// Create accounts and imports/exports.
	fooKP, _ := nkeys.CreateAccount()
	fooPub, _ := fooKP.PublicKey()
	fooAC := jwt.NewAccountClaims(string(fooPub))
	fooAC.Limits.Subs = 10
	fooJWT, err := fooAC.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}
	addAccountToMemResolver(s, string(fooPub), fooJWT)

	// Create a client.
	nkp, _ := nkeys.CreateUser()
	pub, _ := nkp.PublicKey()
	nuc := jwt.NewUserClaims(string(pub))
	ujwt, err := nuc.Encode(fooKP)
	if err != nil {
		t.Fatalf("Error generating user JWT: %v", err)
	}

	c, cr, l := newClientForServer(s)

	// Sign Nonce
	var info nonceInfo
	json.Unmarshal([]byte(l[5:]), &info)
	sigraw, _ := nkp.Sign([]byte(info.Nonce))
	sig := base64.StdEncoding.EncodeToString(sigraw)

	quit := make(chan bool)
	defer func() { quit <- true }()

	pab := make(chan []byte, 16)

	parseAsync := func(cs []byte) {
		pab <- cs
	}

	go func() {
		for {
			select {
			case cs := <-pab:
				c.parse(cs)
			case <-quit:
				return
			}
		}
	}()

	// PING needed to flush the +OK/-ERR to us.
	cs := fmt.Sprintf("CONNECT {\"jwt\":%q,\"sig\":\"%s\",\"verbose\":true,\"pedantic\":true}\r\nPING\r\n", ujwt, sig)
	parseAsync([]byte(cs))
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "+OK") {
		t.Fatalf("Expected an OK, got: %v", l)
	}
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "PONG") {
		t.Fatalf("Expected a PONG")
	}

	// Check to make sure we have the limit set.
	// Account first
	fooAcc := s.LookupAccount(string(fooPub))
	fooAcc.mu.RLock()
	if fooAcc.msubs != 10 {
		fooAcc.mu.RUnlock()
		t.Fatalf("Expected account to have msubs of 10, got %d", fooAcc.msubs)
	}
	fooAcc.mu.RUnlock()
	// Now test that the client has limits too.
	c.mu.Lock()
	if c.msubs != 10 {
		c.mu.Unlock()
		t.Fatalf("Expected client msubs to be 10, got %d", c.msubs)
	}
	c.mu.Unlock()

	// Now make sure its enforced.
	/// These should all work ok.
	for i := 0; i < 10; i++ {
		cs := fmt.Sprintf("SUB foo %d\r\nPING\r\n", i)
		parseAsync([]byte(cs))
		l, _ = cr.ReadString('\n')
		if !strings.HasPrefix(l, "+OK") {
			t.Fatalf("Expected an OK, got: %v", l)
		}
		l, _ = cr.ReadString('\n')
		if !strings.HasPrefix(l, "PONG") {
			t.Fatalf("Expected a PONG")
		}
	}

	// This one should fail.
	cs = fmt.Sprintf("SUB foo 22\r\n")
	parseAsync([]byte(cs))
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "-ERR") {
		t.Fatalf("Expected an ERR, got: %v", l)
	}
	if !strings.Contains(l, "Maximum Subscriptions Exceeded") {
		t.Fatalf("Expected an ERR for max subscriptions exceeded, got: %v", l)
	}

	// Now update the claims and expect if max is lower to be disconnected.
	fooAC.Limits.Subs = 5
	fooJWT, err = fooAC.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}
	addAccountToMemResolver(s, string(fooPub), fooJWT)
	s.updateAccountClaims(fooAcc, fooAC)
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "-ERR") {
		t.Fatalf("Expected an ERR, got: %v", l)
	}
	if !strings.Contains(l, "Maximum Subscriptions Exceeded") {
		t.Fatalf("Expected an ERR for max subscriptions exceeded, got: %v", l)
	}
}

func TestJWTAccountLimitsSubsButServerOverrides(t *testing.T) {
	s := opTrustBasicSetup()
	defer s.Shutdown()
	buildMemAccResolver(s)

	// override with server setting of 2.
	opts := s.getOpts()
	opts.MaxSubs = 2

	okp, _ := nkeys.FromSeed(oSeed)

	// Create accounts and imports/exports.
	fooKP, _ := nkeys.CreateAccount()
	fooPub, _ := fooKP.PublicKey()
	fooAC := jwt.NewAccountClaims(string(fooPub))
	fooAC.Limits.Subs = 10
	fooJWT, err := fooAC.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}
	addAccountToMemResolver(s, string(fooPub), fooJWT)
	fooAcc := s.LookupAccount(string(fooPub))
	fooAcc.mu.RLock()
	if fooAcc.msubs != 10 {
		fooAcc.mu.RUnlock()
		t.Fatalf("Expected account to have msubs of 10, got %d", fooAcc.msubs)
	}
	fooAcc.mu.RUnlock()

	// Create a client.
	nkp, _ := nkeys.CreateUser()
	pub, _ := nkp.PublicKey()
	nuc := jwt.NewUserClaims(string(pub))
	ujwt, err := nuc.Encode(fooKP)
	if err != nil {
		t.Fatalf("Error generating user JWT: %v", err)
	}

	c, cr, l := newClientForServer(s)

	// Sign Nonce
	var info nonceInfo
	json.Unmarshal([]byte(l[5:]), &info)
	sigraw, _ := nkp.Sign([]byte(info.Nonce))
	sig := base64.StdEncoding.EncodeToString(sigraw)

	cs := fmt.Sprintf("CONNECT {\"jwt\":%q,\"sig\":\"%s\"}\r\nSUB foo 1\r\nSUB bar 2\r\nSUB baz 3\r\nPING\r\n", ujwt, sig)
	go c.parse([]byte(cs))
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "-ERR ") {
		t.Fatalf("Expected an error")
	}
	if !strings.Contains(l, "Maximum Subscriptions Exceeded") {
		t.Fatalf("Expected an ERR for max subscriptions exceeded, got: %v", l)
	}
}

func TestJWTAccountLimitsMaxPayload(t *testing.T) {
	s := opTrustBasicSetup()
	defer s.Shutdown()
	buildMemAccResolver(s)

	okp, _ := nkeys.FromSeed(oSeed)

	// Create accounts and imports/exports.
	fooKP, _ := nkeys.CreateAccount()
	fooPub, _ := fooKP.PublicKey()
	fooAC := jwt.NewAccountClaims(string(fooPub))
	fooAC.Limits.Payload = 8
	fooJWT, err := fooAC.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}
	addAccountToMemResolver(s, string(fooPub), fooJWT)

	// Create a client.
	nkp, _ := nkeys.CreateUser()
	pub, _ := nkp.PublicKey()
	nuc := jwt.NewUserClaims(string(pub))
	ujwt, err := nuc.Encode(fooKP)
	if err != nil {
		t.Fatalf("Error generating user JWT: %v", err)
	}

	c, cr, l := newClientForServer(s)

	// Sign Nonce
	var info nonceInfo
	json.Unmarshal([]byte(l[5:]), &info)
	sigraw, _ := nkp.Sign([]byte(info.Nonce))
	sig := base64.StdEncoding.EncodeToString(sigraw)

	quit := make(chan bool)
	defer func() { quit <- true }()

	pab := make(chan []byte, 16)

	parseAsync := func(cs []byte) {
		pab <- cs
	}

	go func() {
		for {
			select {
			case cs := <-pab:
				c.parse(cs)
			case <-quit:
				return
			}
		}
	}()

	cs := fmt.Sprintf("CONNECT {\"jwt\":%q,\"sig\":\"%s\"}\r\nPING\r\n", ujwt, sig)
	parseAsync([]byte(cs))
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "PONG") {
		t.Fatalf("Expected a PONG")
	}

	// Check to make sure we have the limit set.
	// Account first
	fooAcc := s.LookupAccount(string(fooPub))
	fooAcc.mu.RLock()
	if fooAcc.mpay != 8 {
		fooAcc.mu.RUnlock()
		t.Fatalf("Expected account to have mpay of 8, got %d", fooAcc.mpay)
	}
	fooAcc.mu.RUnlock()
	// Now test that the client has limits too.
	c.mu.Lock()
	if c.mpay != 8 {
		c.mu.Unlock()
		t.Fatalf("Expected client to have mpay of 10, got %d", c.mpay)
	}
	c.mu.Unlock()

	cs = fmt.Sprintf("PUB foo 4\r\nXXXX\r\nPING\r\n")
	parseAsync([]byte(cs))
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "PONG") {
		t.Fatalf("Expected a PONG")
	}

	cs = fmt.Sprintf("PUB foo 10\r\nXXXXXXXXXX\r\nPING\r\n")
	parseAsync([]byte(cs))
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "-ERR ") {
		t.Fatalf("Expected an error")
	}
	if !strings.Contains(l, "Maximum Payload") {
		t.Fatalf("Expected an ERR for max payload violation, got: %v", l)
	}
}

func TestJWTAccountLimitsMaxPayloadButServerOverrides(t *testing.T) {
	s := opTrustBasicSetup()
	defer s.Shutdown()
	buildMemAccResolver(s)

	// override with server setting of 4.
	opts := s.getOpts()
	opts.MaxPayload = 4

	okp, _ := nkeys.FromSeed(oSeed)

	// Create accounts and imports/exports.
	fooKP, _ := nkeys.CreateAccount()
	fooPub, _ := fooKP.PublicKey()
	fooAC := jwt.NewAccountClaims(string(fooPub))
	fooAC.Limits.Payload = 8
	fooJWT, err := fooAC.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}
	addAccountToMemResolver(s, string(fooPub), fooJWT)

	// Create a client.
	nkp, _ := nkeys.CreateUser()
	pub, _ := nkp.PublicKey()
	nuc := jwt.NewUserClaims(string(pub))
	ujwt, err := nuc.Encode(fooKP)
	if err != nil {
		t.Fatalf("Error generating user JWT: %v", err)
	}

	c, cr, l := newClientForServer(s)

	// Sign Nonce
	var info nonceInfo
	json.Unmarshal([]byte(l[5:]), &info)
	sigraw, _ := nkp.Sign([]byte(info.Nonce))
	sig := base64.StdEncoding.EncodeToString(sigraw)

	cs := fmt.Sprintf("CONNECT {\"jwt\":%q,\"sig\":\"%s\"}\r\nPUB foo 6\r\nXXXXXX\r\nPING\r\n", ujwt, sig)
	go c.parse([]byte(cs))
	l, _ = cr.ReadString('\n')
	if !strings.HasPrefix(l, "-ERR ") {
		t.Fatalf("Expected an error")
	}
	if !strings.Contains(l, "Maximum Payload") {
		t.Fatalf("Expected an ERR for max payload violation, got: %v", l)
	}
}

// NOTE: For now this is single server, will change to adapt for network wide.
// TODO(dlc) - Make cluster/gateway aware.
func TestJWTAccountLimitsMaxConns(t *testing.T) {
	s := opTrustBasicSetup()
	defer s.Shutdown()
	buildMemAccResolver(s)

	okp, _ := nkeys.FromSeed(oSeed)

	// Create accounts and imports/exports.
	fooKP, _ := nkeys.CreateAccount()
	fooPub, _ := fooKP.PublicKey()
	fooAC := jwt.NewAccountClaims(string(fooPub))
	fooAC.Limits.Conn = 8
	fooJWT, err := fooAC.Encode(okp)
	if err != nil {
		t.Fatalf("Error generating account JWT: %v", err)
	}
	addAccountToMemResolver(s, string(fooPub), fooJWT)

	newClient := func(expPre string) {
		t.Helper()
		// Create a client.
		nkp, _ := nkeys.CreateUser()
		pub, _ := nkp.PublicKey()
		nuc := jwt.NewUserClaims(string(pub))
		ujwt, err := nuc.Encode(fooKP)
		if err != nil {
			t.Fatalf("Error generating user JWT: %v", err)
		}
		c, cr, l := newClientForServer(s)

		// Sign Nonce
		var info nonceInfo
		json.Unmarshal([]byte(l[5:]), &info)
		sigraw, _ := nkp.Sign([]byte(info.Nonce))
		sig := base64.StdEncoding.EncodeToString(sigraw)
		cs := fmt.Sprintf("CONNECT {\"jwt\":%q,\"sig\":\"%s\", \"verbose\":true}\r\nPING\r\n", ujwt, sig)
		go c.parse([]byte(cs))
		l, _ = cr.ReadString('\n')
		if !strings.HasPrefix(l, expPre) {
			t.Fatalf("Expected a response starting with %q", expPre)
		}
	}

	for i := 0; i < 8; i++ {
		newClient("+OK")
	}
	// Now this one should fail.
	newClient("-ERR ")
}
