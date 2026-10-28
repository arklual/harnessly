package design

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// PNG: a picture of every slide at its own size (at least 1920 px wide), in a zip named by the slide's place and title.

func init() {
	RegisterDeckExporter(DeckExportPNG, DeckExporter{Needs: []string{"chrome"}, Ext: "zip", Run: runDeckPNG})
}

var deckSlug = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// deckSlideFile is the name of a slide's picture: 01-title.png.
func deckSlideFile(i int, title, ext string) string {
	slug := strings.Trim(deckSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if r := []rune(slug); len(r) > 40 {
		slug = strings.Trim(string(r[:40]), "-")
	}
	if slug == "" {
		return fmt.Sprintf("%02d.%s", i+1, ext)
	}
	return fmt.Sprintf("%02d-%s.%s", i+1, slug, ext)
}

func runDeckPNG(ctx context.Context, run DeckExportRun) error {
	slides, err := run.L.deckSlides(run.Artifact)
	if err != nil {
		return err
	}
	sh, err := run.L.newDeckShooter(run.Artifact)
	if err != nil {
		return err
	}
	defer sh.close()
	tab, err := sh.tab()
	if err != nil {
		return err
	}
	defer tab.close()
	out, err := os.Create(run.Out)
	if err != nil {
		return err
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	for i, sl := range slides {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := tab.open(sl, deckScale(sl.W)); err != nil {
			return fmt.Errorf("%s: %w", sl.Title, err)
		}
		pic, err := tab.png()
		if err != nil {
			return fmt.Errorf("%s: %w", sl.Title, err)
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: deckSlideFile(i, sl.Title, "png"), Method: zip.Deflate, Modified: time.Now()})
		if err != nil {
			return err
		}
		if _, err := w.Write(pic); err != nil {
			return err
		}
		run.Progress(float64(i+1) / float64(len(slides)))
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return out.Close()
}
