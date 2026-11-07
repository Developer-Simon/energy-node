package main

import "testing"

func TestChainSignalsGate(t *testing.T) {
	var g chainSignalsGate
	if g.Want(0) {
		t.Fatal("no chains and never published: want false")
	}
	if !g.Want(1) {
		t.Fatal("one chain: want true")
	}
	g.published = true // a successful publish with chains
	if !g.Want(0) {
		t.Fatal("chains gone after a publish: want one empty payload")
	}
	g.published = false // the empty payload went out
	if g.Want(0) {
		t.Fatal("after the empty payload: want false")
	}
	if !g.Want(1) {
		t.Fatal("chains back: want true")
	}
}
