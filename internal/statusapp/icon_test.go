package statusapp

import (
	"image/color"
	"os"
	"testing"
)

func TestStatusPreferences(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("AppData", dir)
	for _, name := range []string{"colored-icon", "notifications"} {
		if !preferenceEnabled(name) {
			t.Fatalf("missing %s preference should default to enabled", name)
		}
		for _, enabled := range []bool{false, true, false} {
			if err := savePreference(name, enabled); err != nil {
				t.Fatal(err)
			}
			if got := preferenceEnabled(name); got != enabled {
				t.Fatalf("reloaded %s = %v, want %v", name, got, enabled)
			}
		}
	}
	if preferenceEnabled("colored-icon") || preferenceEnabled("notifications") {
		t.Fatal("preferences should persist independently")
	}
	path, err := preferencePath("colored-icon")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !preferenceEnabled("colored-icon") {
		t.Fatal("invalid preference should use default")
	}
}

func TestStatusIconGrayscalePreservesAlpha(t *testing.T) {
	colored, err := statusIcon(true)
	if err != nil {
		t.Fatal(err)
	}
	gray, err := statusIcon(false)
	if err != nil {
		t.Fatal(err)
	}
	if gray.Bounds() != colored.Bounds() {
		t.Fatal("icon dimensions changed")
	}
	changed := false
	for y := gray.Bounds().Min.Y; y < gray.Bounds().Max.Y; y++ {
		for x := gray.Bounds().Min.X; x < gray.Bounds().Max.X; x++ {
			c := color.NRGBAModel.Convert(colored.At(x, y)).(color.NRGBA)
			g := color.NRGBAModel.Convert(gray.At(x, y)).(color.NRGBA)
			if g.R != g.G || g.G != g.B || g.A != c.A {
				t.Fatalf("invalid grayscale/alpha at (%d,%d): %v -> %v", x, y, c, g)
			}
			changed = changed || c != g
		}
	}
	if !changed {
		t.Fatal("colored and grayscale icons are identical")
	}
}
