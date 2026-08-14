//go:build windows

package main

import "testing"

func withUpdateChannel(t *testing.T, channel string) {
	t.Helper()
	old := powerPilotUpdateChannel
	powerPilotUpdateChannel = channel
	t.Cleanup(func() { powerPilotUpdateChannel = old })
}

func TestStableUpdateReleaseSource(t *testing.T) {
	withUpdateChannel(t, "stable")
	if got := powerPilotReleaseAPIURL(); got != "https://api.github.com/repos/MikellaLimited/PowerPilot/releases/latest" {
		t.Fatalf("unexpected stable release URL: %s", got)
	}
	v, err := powerPilotReleaseVersion(powerPilotRelease{TagName: "v0.8.2"})
	if err != nil || v != "0.8.2" {
		t.Fatalf("unexpected stable version: version=%q err=%v", v, err)
	}
}

func TestDevelopUpdateReleaseSource(t *testing.T) {
	withUpdateChannel(t, "develop")
	if got := powerPilotReleaseAPIURL(); got != "https://api.github.com/repos/MikellaLimited/PowerPilot/releases/tags/develop" {
		t.Fatalf("unexpected develop release URL: %s", got)
	}
	rel := powerPilotRelease{
		TagName: "develop",
		Body:    "Rolling test build\npowerpilot-develop-version: 0.8.2-dev.42\n",
		Assets:  []powerPilotReleaseAsset{{Name: "PowerPilot_Develop_Update.zip"}},
	}
	v, err := powerPilotReleaseVersion(rel)
	if err != nil || v != "0.8.2-dev.42" {
		t.Fatalf("unexpected develop version: version=%q err=%v", v, err)
	}
	asset, err := selectPowerPilotUpdateAsset(rel)
	if err != nil || asset.Name != "PowerPilot_Develop_Update.zip" {
		t.Fatalf("unexpected develop asset: asset=%q err=%v", asset.Name, err)
	}
}

func TestDevelopBuildOrdering(t *testing.T) {
	if compareVersions("0.8.2-dev.41", "0.8.2-dev.42") >= 0 {
		t.Fatal("newer develop build must compare greater")
	}
}
