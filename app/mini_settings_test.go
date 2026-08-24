//go:build windows

package main

import (
	"bytes"
	"image"
	_ "image/png"
	"testing"
)

func TestNormalizeMiniLayoutMigratesLegacyMetrics(t *testing.T) {
	old := app.settings
	t.Cleanup(func() { app.settings = old })

	app.settings = Settings{
		MiniShowTask:      true,
		MiniShowCountdown: true,
		MiniShowStep:      true,
		MiniShowMetrics:   true,
	}
	normalizeV040Settings()
	if !app.settings.MiniLayoutMigrated || !app.settings.MiniShowProgress {
		t.Fatal("mini layout migration was not recorded")
	}
	if !app.settings.MiniShowCPU || !app.settings.MiniShowGPU || !app.settings.MiniShowRAM || !app.settings.MiniShowNetwork || !app.settings.MiniShowDisk {
		t.Fatal("legacy metrics setting did not migrate to detailed metrics")
	}
}

func TestMiniDetailedMetricToggleUpdatesLegacyAggregate(t *testing.T) {
	old := app.settings
	t.Cleanup(func() { app.settings = old })

	app.settings = Settings{MiniLayoutMigrated: true}
	toggleMiniDetailedOption(4)
	if !app.settings.MiniShowCPU || !app.settings.MiniShowMetrics {
		t.Fatal("enabling CPU should enable the legacy aggregate metrics state")
	}
	toggleMiniDetailedOption(4)
	if app.settings.MiniShowCPU || app.settings.MiniShowMetrics {
		t.Fatal("disabling the last metric should clear the aggregate metrics state")
	}
}

func TestProcessNameHasNonASCII(t *testing.T) {
	if !processNameHasNonASCII("Яндекс Музыка.exe") {
		t.Fatal("Cyrillic process name was not recognized")
	}
	if processNameHasNonASCII("YandexMusic.exe") {
		t.Fatal("ASCII process name was incorrectly recognized as Unicode")
	}
}

func TestMiniPinAssetsDecode(t *testing.T) {
	for name, data := range map[string][]byte{"pin": captionPinPNGData, "unpin": captionUnpinPNGData} {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s icon does not decode: %v", name, err)
		}
		if cfg.Width != 50 || cfg.Height != 50 {
			t.Fatalf("%s icon is %dx%d, want 50x50", name, cfg.Width, cfg.Height)
		}
	}
}
