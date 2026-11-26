package esxi

import "testing"

func TestCleanRel(t *testing.T) {
	p, err := CleanRel("/RV1002/../x")
	if err == nil {
		t.Fatal("expected .. rejected")
	}
	p, err = CleanRel("/RV1002/a")
	if err != nil || p != "RV1002/a" {
		t.Fatalf("%q %v", p, err)
	}
}

func TestRenameDest(t *testing.T) {
	p, err := RenameDest("RV1002/old.vmx", "new.vmx")
	if err != nil || p != "RV1002/new.vmx" {
		t.Fatalf("%q %v", p, err)
	}
	if _, err := RenameDest("RV1002/old.vmx", "a/b"); err == nil {
		t.Fatal("slash name should fail")
	}
}

func TestTransferPathsNormalizeAndProtectRoot(t *testing.T) {
	for _, paths := range [][2]string{{"", "a"}, {"//", "a"}, {"a", "."}, {"a", "a/./"}, {"a", "a//child"}, {"a", "../b"}} {
		if _, _, err := NormalizeTransferPaths(paths[0], paths[1]); err == nil {
			t.Fatalf("unsafe transfer accepted: %v", paths)
		}
	}
	src, dst, err := NormalizeTransferPaths("/dir//./a", "\\dir\\b")
	if err != nil || src != "dir/a" || dst != "dir/b" {
		t.Fatal(src, dst, err)
	}
}
