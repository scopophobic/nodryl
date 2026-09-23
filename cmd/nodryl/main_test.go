package main

import "testing"

func TestMapAndObserveArguments(t *testing.T) {
	path, jsonOutput, err := mapArgs([]string{"--json", "../other"})
	if err != nil || path != "../other" || !jsonOutput {
		t.Fatalf("map args = %q, %v, %v", path, jsonOutput, err)
	}
	if _, _, err := mapArgs([]string{".", "../other"}); err == nil {
		t.Fatal("accepted two map paths")
	}
	path, address, err := observeArgs([]string{"../other", "--listen", "127.0.0.1:4320"})
	if err != nil || path != "../other" || address != "127.0.0.1:4320" {
		t.Fatalf("observe args = %q, %q, %v", path, address, err)
	}
	if _, _, err := observeArgs([]string{"--listen"}); err == nil {
		t.Fatal("accepted missing listen address")
	}
}
