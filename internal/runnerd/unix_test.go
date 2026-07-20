package runnerd

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestTokenFileRequiresPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runnerd.token")
	if err := os.WriteFile(path, []byte(testToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := ReadTokenFile(path)
	if err != nil || string(token) != testToken {
		t.Fatalf("private token returned %q, %v", token, err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTokenFile(path); err == nil {
		t.Fatal("group-readable token was accepted")
	}
}

func TestUnixListenerIsPrivateAndNeverReplacesRegularFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "runnerd.sock")
	listener, err := ListenUnix(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode is %o", info.Mode().Perm())
	}
	client, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	client.Close()
	listener.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("closed socket remained: %v", err)
	}
	if err := os.WriteFile(path, []byte("do not replace"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ListenUnix(path); err == nil {
		t.Fatal("regular file was replaced")
	}
}
