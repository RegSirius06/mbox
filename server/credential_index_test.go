package main

import (
	"bytes"
	"fmt"
	"testing"
)

func installCredentialTestPolicies(t *testing.T, policies map[string]clientPolicy) {
	t.Helper()
	clientAuthMu.Lock()
	oldPolicies, oldIndex := clientPolicies, policyIDsByAccount
	clientPolicies = policies
	policyIDsByAccount = buildPolicyAccountIndex(policies)
	clientAuthMu.Unlock()
	t.Cleanup(func() {
		clientAuthMu.Lock()
		clientPolicies, policyIDsByAccount = oldPolicies, oldIndex
		clientAuthMu.Unlock()
	})
}

func TestIncrementalCredentialUpdatePreservesUnrelatedSecrets(t *testing.T) {
	const n = 30000
	policies := make(map[string]clientPolicy, n+3)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("account-%06d", i)
		policies[id] = clientPolicy{AccountID: id, PSK: bytes.Repeat([]byte{byte(i)}, 32)}
	}
	handle := "00112233445566778899aabbccddeeff"
	policies[handle] = clientPolicy{AccountID: "target", PSK: bytes.Repeat([]byte{1}, 32)}
	policies["old-alias"] = clientPolicy{AccountID: "target", PSK: bytes.Repeat([]byte{1}, 32)}
	installCredentialTestPolicies(t, policies)

	// A full-map clone would copy this PSK and break pointer identity.
	unrelated := clientPolicies["account-029999"].PSK
	replacement := clientPolicy{AccountID: "target", PSK: bytes.Repeat([]byte{2}, 32)}
	if err := applyCredentialEntries(handle, map[string]clientPolicy{
		handle:      replacement,
		"new-alias": replacement,
	}, false); err != nil {
		t.Fatal(err)
	}

	clientAuthMu.RLock()
	defer clientAuthMu.RUnlock()
	if got := clientPolicies["account-029999"].PSK; &got[0] != &unrelated[0] {
		t.Fatal("update cloned unrelated PSK")
	}
	if _, ok := clientPolicies["old-alias"]; ok {
		t.Fatal("old alias survived rotation")
	}
	if p, ok := clientPolicies["new-alias"]; !ok || p.PSK[0] != 2 {
		t.Fatal("rotated alias not installed")
	}
	if len(clientPolicies) != n+2 {
		t.Fatalf("policy count=%d", len(clientPolicies))
	}
	if len(policyIDsByAccount["target"]) != 2 {
		t.Fatal("target index out of sync")
	}
}

func TestIncrementalCredentialCollisionIsAtomic(t *testing.T) {
	a := "00112233445566778899aabbccddeeff"
	b := "ffeeddccbbaa99887766554433221100"
	oldA := clientPolicy{AccountID: "alice", PSK: bytes.Repeat([]byte{1}, 32)}
	oldB := clientPolicy{AccountID: "bob", PSK: bytes.Repeat([]byte{2}, 32)}
	installCredentialTestPolicies(t, map[string]clientPolicy{a: oldA, "old": oldA, b: oldB})
	bad := clientPolicy{AccountID: "alice", PSK: bytes.Repeat([]byte{3}, 32)}
	if err := applyCredentialEntries(a, map[string]clientPolicy{a: bad, b: bad}, false); err == nil {
		t.Fatal("account collision accepted")
	}
	clientAuthMu.RLock()
	defer clientAuthMu.RUnlock()
	if clientPolicies[a].PSK[0] != 1 || clientPolicies[b].PSK[0] != 2 {
		t.Fatal("rejected transaction changed credentials")
	}
	if _, ok := clientPolicies["old"]; !ok {
		t.Fatal("rejected transaction removed old alias")
	}
	if len(policyIDsByAccount["alice"]) != 2 || len(policyIDsByAccount["bob"]) != 1 {
		t.Fatal("rejected transaction changed identity index")
	}
}

func TestIncrementalCredentialDeleteOnlyAffectedAccount(t *testing.T) {
	a := "00112233445566778899aabbccddeeff"
	b := "ffeeddccbbaa99887766554433221100"
	alice := clientPolicy{AccountID: "alice", PSK: bytes.Repeat([]byte{1}, 32)}
	bob := clientPolicy{AccountID: "bob", PSK: bytes.Repeat([]byte{2}, 32)}
	installCredentialTestPolicies(t, map[string]clientPolicy{a: alice, "alias": alice, b: bob})
	if err := applyCredentialEntries(a, nil, true); err != nil {
		t.Fatal(err)
	}
	clientAuthMu.RLock()
	defer clientAuthMu.RUnlock()
	if len(clientPolicies) != 1 || clientPolicies[b].AccountID != "bob" {
		t.Fatal("wrong policies after delete")
	}
	if _, found := policyIDsByAccount["alice"]; found {
		t.Fatal("index retained deleted account")
	}
	if len(policyIDsByAccount["bob"]) != 1 {
		t.Fatal("index lost unrelated account")
	}
}

func BenchmarkIncrementalCredentialUpdate100k(b *testing.B) {
	policies := make(map[string]clientPolicy, 100001)
	for i := 0; i < 100000; i++ {
		id := fmt.Sprintf("account-%06d", i)
		policies[id] = clientPolicy{AccountID: id, PSK: bytes.Repeat([]byte{1}, 32)}
	}
	handle := "00112233445566778899aabbccddeeff"
	policies[handle] = clientPolicy{AccountID: "target", PSK: bytes.Repeat([]byte{1}, 32)}
	clientAuthMu.Lock()
	oldPolicies, oldIndex := clientPolicies, policyIDsByAccount
	clientPolicies = policies
	policyIDsByAccount = buildPolicyAccountIndex(policies)
	clientAuthMu.Unlock()
	defer func() {
		clientAuthMu.Lock()
		clientPolicies, policyIDsByAccount = oldPolicies, oldIndex
		clientAuthMu.Unlock()
	}()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := clientPolicy{AccountID: "target", PSK: bytes.Repeat([]byte{byte(i)}, 32)}
		if err := applyCredentialEntries(handle, map[string]clientPolicy{handle: p}, false); err != nil {
			b.Fatal(err)
		}
	}
}

func TestCredentialAccountMoveRevokesOldSessionEvenIfPSKUnchanged(t *testing.T) {
	handle := "00112233445566778899aabbccddeeff"
	secret := bytes.Repeat([]byte{0x47}, 32)
	before := clientPolicy{PSK: secret, AccountID: "before"}
	after := clientPolicy{PSK: secret, AccountID: "after"}
	installCredentialTestPolicies(t, map[string]clientPolicy{handle: before})
	existing := testSessionForPolicy(handle, "before", secret)
	sessionsMu.Lock()
	oldSessions := sessions
	sessions = map[string]*session{"active-session": existing}
	sessionsMu.Unlock()
	t.Cleanup(func() {
		sessionsMu.Lock()
		sessions = oldSessions
		sessionsMu.Unlock()
	})
	if err := applyCredentialEntries(handle, map[string]clientPolicy{handle: after}, false); err != nil {
		t.Fatal(err)
	}
	sessionsMu.Lock()
	_, active := sessions["active-session"]
	sessionsMu.Unlock()
	if active {
		t.Fatal("old account session survived identity move")
	}
}
