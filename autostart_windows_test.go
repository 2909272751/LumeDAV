package main

import "testing"

func TestAutoStartCommandQuotesExecutablePath(t *testing.T) {
	got := autoStartCommand(`D:\Program Files\LumeDAV\LumeDAV.exe`)
	want := `"D:\Program Files\LumeDAV\LumeDAV.exe" --autostart`
	if got != want {
		t.Fatalf("autoStartCommand() = %q, want %q", got, want)
	}
}

func TestAutoStartCommandMatchesIgnoresCaseAndOuterSpace(t *testing.T) {
	if !autoStartCommandMatches(`  "d:\apps\lumedav\LumeDAV.exe" --autostart  `, `D:\Apps\LumeDAV\LumeDAV.exe`) {
		t.Fatal("expected equivalent Windows command paths to match")
	}
}

func TestHasLaunchArgFindsFlagInAnyPosition(t *testing.T) {
	if !hasLaunchArg([]string{"--quiet", "--autostart"}, "--autostart") {
		t.Fatal("expected launch flag to be found")
	}
}
