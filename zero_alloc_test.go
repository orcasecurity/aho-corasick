package aho_corasick

import "testing"

func TestAhoCorasick_FindAllByteTo_MatchesFindAll(t *testing.T) {
	builder := NewAhoCorasickBuilder(Opts{
		MatchKind: LeftMostLongestMatch,
		DFA:       true,
	})
	ac := builder.Build([]string{"he", "she", "his", "hers"})

	haystack := []byte("ushers and his sheep")
	want := ac.FindAll(string(haystack))
	got := ac.FindAllByteTo(haystack, make([]Match, 0, len(want)))

	if len(got) != len(want) {
		t.Fatalf("expected %d matches, got %d", len(want), len(got))
	}

	for i := range want {
		if got[i].pattern != want[i].pattern || got[i].len != want[i].len || got[i].end != want[i].end {
			t.Fatalf("unexpected match at index %d: got=%+v want=%+v", i, got[i], want[i])
		}
	}
}

func TestAhoCorasick_FindAllByteTo_ZeroAllocsWithPreallocatedDst(t *testing.T) {
	builder := NewAhoCorasickBuilder(Opts{
		MatchKind: StandardMatch,
		DFA:       true,
	})
	ac := builder.Build([]string{"foo", "bar", "baz"})
	haystack := []byte("foo and bar and baz and foo and baz")

	state := ac.NewMatchState()
	expected := ac.FindAllByteToWithState(haystack, nil, &state)
	dst := make([]Match, 0, len(expected))

	allocs := testing.AllocsPerRun(1000, func() {
		dst = dst[:0]
		dst = ac.FindAllByteToWithState(haystack, dst, &state)
	})

	if allocs != 0 {
		t.Fatalf("expected 0 allocations per run, got %.2f", allocs)
	}

	if len(dst) != len(expected) {
		t.Fatalf("expected %d matches, got %d", len(expected), len(dst))
	}
}

func TestAhoCorasick_ContainsByteWithState_ZeroAllocs(t *testing.T) {
	builder := NewAhoCorasickBuilder(Opts{
		MatchKind: StandardMatch,
		DFA:       true,
	})
	ac := builder.Build([]string{"foo", "bar", "baz"})
	haystack := []byte("some data with foo token")
	state := ac.NewMatchState()

	allocs := testing.AllocsPerRun(1000, func() {
		if !ac.ContainsByteWithState(haystack, &state) {
			t.Fatal("expected match")
		}
	})

	if allocs != 0 {
		t.Fatalf("expected 0 allocations per run, got %.2f", allocs)
	}
}
