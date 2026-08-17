package protocol

import (
	"encoding/json"
	"testing"
)

func TestSessionIDRoundTrip(t *testing.T) {
	const uuid = "0191d2c0-1234-7abc-89de-0123456789ab"
	id := NewSessionID(uuid)
	b, err := json.Marshal(id)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `"`+uuid+`"` {
		t.Fatalf("unexpected JSON: %s", b)
	}
	var got SessionID
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != id {
		t.Fatalf("round-trip mismatch: %v != %v", got, id)
	}
}

func TestSessionThreadIDConversion(t *testing.T) {
	const uuid = "0191d2c0-1234-7abc-89de-0123456789ab"
	s := NewSessionID(uuid)
	if s.ToThreadID().ToSessionID() != s {
		t.Fatalf("session<->thread round-trip lost the uuid")
	}
	if s.ToThreadID().String() != uuid {
		t.Fatalf("thread id string mismatch: %q", s.ToThreadID().String())
	}
}

func TestAgentPathValidationAndJSON(t *testing.T) {
	cases := []struct {
		in    string
		valid bool
	}{
		{"/root", true},
		{"/morpheus", true},
		{"/root/researcher", true},
		{"/root/researcher/worker", true},
		{"/not-root", false},
		{"/root/BadName", false},
		{"/root/", false},
		{"relative", false},
	}
	for _, tc := range cases {
		_, err := NewAgentPath(tc.in)
		if tc.valid && err != nil {
			t.Errorf("NewAgentPath(%q) unexpected error: %v", tc.in, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("NewAgentPath(%q) expected error, got none", tc.in)
		}
	}

	p, err := NewAgentPath("/root/researcher")
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `"/root/researcher"` {
		t.Fatalf("unexpected JSON: %s", b)
	}
	var got AgentPath
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != p {
		t.Fatalf("round-trip mismatch: %v != %v", got, p)
	}
}

func TestAgentPathName(t *testing.T) {
	root := AgentPathRootValue()
	if root.Name() != "root" || !root.IsRoot() {
		t.Fatalf("root name/isRoot wrong: %q %v", root.Name(), root.IsRoot())
	}
	child, err := root.Join("researcher")
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if child.String() != "/root/researcher" || child.Name() != "researcher" {
		t.Fatalf("child wrong: %q %q", child.String(), child.Name())
	}
}

func TestToolNameJSON(t *testing.T) {
	plain := PlainToolName("read")
	b, err := json.Marshal(plain)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `{"name":"read","namespace":null}` {
		t.Fatalf("plain tool name JSON mismatch: %s", b)
	}

	ns := NamespacedToolName("mcp/", "search")
	if ns.String() != "mcp/search" {
		t.Fatalf("namespaced display mismatch: %q", ns.String())
	}
	b2, err := json.Marshal(ns)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ToolName
	if err := json.Unmarshal(b2, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Name != ns.Name || got.Namespace == nil || *got.Namespace != *ns.Namespace {
		t.Fatalf("round-trip mismatch: %+v != %+v", got, ns)
	}
}
