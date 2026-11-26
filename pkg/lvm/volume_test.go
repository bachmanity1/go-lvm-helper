package lvm

import (
	"strings"
	"testing"
)

func TestIsValidLVMName(t *testing.T) {
	for name, want := range map[string]bool{
		"data-vg":                true,
		"longhorn-vg-0a1b2c3d":   true,
		"a.b_c+d":                true,
		"":                       false,
		".":                      false,
		"..":                     false,
		"-dashed":                false,
		"bad*name":               false,
		"with space":             false,
		"with/slash":             false,
		strings.Repeat("a", 127): true,
		strings.Repeat("a", 128): false,
	} {
		if got := isValidLVMName(name); got != want {
			t.Fatalf("isValidLVMName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestIsValidLVMUUID(t *testing.T) {
	for id, want := range map[string]bool{
		"hQ6DZ1-5oZ2-RSCx-gn7n-zmHf-DGLg-UL9gjO": true,
		"":                                       false,
		"-leading":                               false,
		"with space":                             false,
		"semi;colon":                             false,
		"under_score":                            false,
	} {
		if got := isValidLVMUUID(id); got != want {
			t.Fatalf("isValidLVMUUID(%q) = %v, want %v", id, got, want)
		}
	}
}
