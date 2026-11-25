package main

import (
	"reflect"
	"testing"
)

func TestMediaHelperParsesItsFlags(t *testing.T) {
	files, name, rest, err := parseMediaHelperArgs([]string{"--log", "a", "--cancel", "b", "--", "x", "y", "z"})
	if err != nil {
		t.Fatal(err)
	}
	if files.Log != "a" || files.Cancel != "b" || files.Status != "" {
		t.Fatalf("files = %+v", files)
	}
	if name != "x" || !reflect.DeepEqual(rest, []string{"y", "z"}) {
		t.Fatalf("name = %q, rest = %v", name, rest)
	}

	if _, _, _, err := parseMediaHelperArgs([]string{"--log", "a", "--cancel", "b", "x"}); err == nil {
		t.Fatal("missing -- must fail")
	}
	if _, _, _, err := parseMediaHelperArgs([]string{"--log", "a", "--cancel", "b", "--"}); err == nil {
		t.Fatal("missing program name must fail")
	}
	if _, _, _, err := parseMediaHelperArgs([]string{"--cancel", "b", "--", "x"}); err == nil {
		t.Fatal("missing --log must fail")
	}
}
