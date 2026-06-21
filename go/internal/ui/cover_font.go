package ui

import (
	"log"

	"github.com/blacktop/go-termimg"
)

var cachedFontRatio float64 = -1

func fontAspectRatio() float64 {
	if cachedFontRatio >= 0 {
		return cachedFontRatio
	}
	f := termimg.QueryTerminalFeatures()
	if f.FontWidth > 0 && f.FontHeight > 0 {
		cachedFontRatio = float64(f.FontWidth) / float64(f.FontHeight)
		log.Printf("[DEBUG] fontAspectRatio: detected FontWidth=%d FontHeight=%d ratio=%.4f (%.1f:1)",
			f.FontWidth, f.FontHeight, cachedFontRatio,
			float64(f.FontHeight)/float64(f.FontWidth))
	} else {
		cachedFontRatio = 0.5
		log.Printf("[DEBUG] fontAspectRatio: detection FAILED (FontWidth=%d FontHeight=%d), fallback to 0.5 (2:1)",
			f.FontWidth, f.FontHeight)
	}
	return cachedFontRatio
}
