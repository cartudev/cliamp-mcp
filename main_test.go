package main

import (
	"strings"
	"testing"
)

func TestShortenDropsEmbeddedArt(t *testing.T) {
	art := "data:image/jpeg;base64," + string(make([]byte, 400))
	got, ok := stripArt(map[string]any{"album_art_url": art}).(map[string]any)
	if !ok {
		t.Fatal("shape changed")
	}
	text, _ := got["album_art_url"].(string)
	if text != "omitted" {
		t.Fatalf("art = %q, want omitted", text)
	}
}

func TestReadFrameDropsCover(t *testing.T) {
	cover := "data:image/jpeg;base64," + strings.Repeat("A", 2<<20)
	raw := `{"ok":true,"album_art_url":"` + cover + `","title":"Disappear"}` + "\n"
	got, err := readFrame(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "base64") {
		t.Fatal("cover leaked into the frame")
	}
	if !strings.Contains(string(got), `"omitted"`) || !strings.Contains(string(got), "Disappear") {
		t.Fatalf("frame = %s", got)
	}
}

func TestMatchesIgnoresOrder(t *testing.T) {
	track := map[string]any{"title": "White", "artist": "The Crow Is White"}
	if !matches(track, "crow white") {
		t.Fatal("expected a match")
	}
	if matches(track, "daft punk") {
		t.Fatal("unexpected match")
	}
}
