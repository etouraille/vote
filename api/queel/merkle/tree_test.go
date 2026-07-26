package merkle_test

import (
	"testing"

	"github.com/etouraille/queel/merkle"
)

func leaves(n int) []merkle.Hash {
	out := make([]merkle.Hash, n)
	for i := range out {
		out[i] = merkle.HashBytes([]byte{byte(i)})
	}
	return out
}

func TestBuildRejectsNonPowerOfTwoLeafCounts(t *testing.T) {
	for _, n := range []int{0, 3, 5, 6, 7, 9} {
		if _, err := merkle.Build(leaves(n)); err == nil {
			t.Fatalf("expected an error building a tree with %d leaves (not a power of two)", n)
		}
	}
}

func TestBuildAcceptsPowerOfTwoLeafCounts(t *testing.T) {
	for _, n := range []int{1, 2, 4, 8, 16, 4096} {
		if _, err := merkle.Build(leaves(n)); err != nil {
			t.Fatalf("expected %d leaves to build fine, got %v", n, err)
		}
	}
}

func TestSameLeavesProduceTheSameRoot(t *testing.T) {
	a, err := merkle.Build(leaves(16))
	if err != nil {
		t.Fatal(err)
	}
	b, err := merkle.Build(leaves(16))
	if err != nil {
		t.Fatal(err)
	}
	if a.Root() != b.Root() {
		t.Fatal("expected two trees built from identical leaves to have the same root")
	}
}

func TestChangingOneLeafChangesTheRoot(t *testing.T) {
	original := leaves(16)
	a, err := merkle.Build(original)
	if err != nil {
		t.Fatal(err)
	}

	changed := append([]merkle.Hash(nil), original...)
	changed[7] = merkle.HashBytes([]byte("something else entirely"))
	b, err := merkle.Build(changed)
	if err != nil {
		t.Fatal(err)
	}

	if a.Root() == b.Root() {
		t.Fatal("expected changing one leaf to change the root")
	}
}

func TestDiffFindsExactlyTheChangedLeaves(t *testing.T) {
	original := leaves(64)
	a, err := merkle.Build(original)
	if err != nil {
		t.Fatal(err)
	}

	changed := append([]merkle.Hash(nil), original...)
	changed[3] = merkle.HashBytes([]byte("changed"))
	changed[40] = merkle.HashBytes([]byte("also changed"))
	b, err := merkle.Build(changed)
	if err != nil {
		t.Fatal(err)
	}

	diff, err := merkle.Diff(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff) != 2 {
		t.Fatalf("Diff = %v, want exactly 2 divergent leaves", diff)
	}
	want := map[int]bool{3: true, 40: true}
	for _, i := range diff {
		if !want[i] {
			t.Fatalf("Diff reported unexpected leaf %d, want only %v", i, want)
		}
	}
}

func TestDiffOfIdenticalTreesIsEmpty(t *testing.T) {
	a, err := merkle.Build(leaves(32))
	if err != nil {
		t.Fatal(err)
	}
	b, err := merkle.Build(leaves(32))
	if err != nil {
		t.Fatal(err)
	}
	diff, err := merkle.Diff(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff) != 0 {
		t.Fatalf("expected no divergent leaves between identical trees, got %v", diff)
	}
}

func TestDiffRejectsMismatchedLeafCounts(t *testing.T) {
	a, err := merkle.Build(leaves(16))
	if err != nil {
		t.Fatal(err)
	}
	b, err := merkle.Build(leaves(32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := merkle.Diff(a, b); err == nil {
		t.Fatal("expected an error diffing trees with different leaf counts")
	}
}

func TestDiffEveryLeafChangedReturnsAll(t *testing.T) {
	const n = 8
	a, err := merkle.Build(leaves(n))
	if err != nil {
		t.Fatal(err)
	}
	allDifferent := make([]merkle.Hash, n)
	for i := range allDifferent {
		allDifferent[i] = merkle.HashBytes([]byte{byte(i), 0xff})
	}
	b, err := merkle.Build(allDifferent)
	if err != nil {
		t.Fatal(err)
	}

	diff, err := merkle.Diff(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff) != n {
		t.Fatalf("expected all %d leaves to be reported divergent, got %d: %v", n, len(diff), diff)
	}
}
