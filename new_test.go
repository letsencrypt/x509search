package x509search

import "testing"

func TestNew(t *testing.T) {
	if New() == nil {
		t.Fatal("New must not return nil")
	}
}
