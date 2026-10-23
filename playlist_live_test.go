package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bjarneo/cliamp/ipc"
)

func TestLivePlayReply(t *testing.T) {
	if os.Getenv("CLIAMP_LIVE") == "" {
		t.Skip("set CLIAMP_LIVE=1 to replay the current track")
	}
	cli := client{socket: ipc.DefaultSocketPath()}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	index, err := cli.currentIndex(ctx)
	if err != nil {
		t.Fatal(err)
	}
	body, err := cli.call(ctx, ipc.V2Request{
		ID:        id("play"),
		Method:    "operation.submit",
		Operation: "queue.play",
		Params:    []byte(fmt.Sprintf(`{"index":%d}`, index)),
	}, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "base64") {
		t.Fatal("cover leaked into the play reply")
	}
	t.Logf("play reply %d bytes", len(body))
}

func TestLiveLargePage(t *testing.T) {
	if os.Getenv("CLIAMP_LIVE") == "" {
		t.Skip("set CLIAMP_LIVE=1 to read a fat IPC frame")
	}
	cli := client{socket: ipc.DefaultSocketPath()}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	page, err := cli.window(ctx, "queue.list", 234, 8)
	if err != nil {
		t.Fatal(err)
	}
	tracks, _ := page["tracks"].([]map[string]any)
	if len(tracks) == 0 {
		t.Fatal("empty page")
	}
	if art, _ := tracks[0]["album_art_url"].(string); art != "" && art != "omitted" && len(art) > 80 {
		t.Fatalf("art leaked: %d bytes", len(art))
	}
	t.Logf("page tracks=%d art=%v", len(tracks), tracks[0]["album_art_url"])
}

func TestLivePlaylistFilter(t *testing.T) {
	if os.Getenv("CLIAMP_LIVE") == "" {
		t.Skip("set CLIAMP_LIVE=1 to scan the running player")
	}
	cli := client{socket: os.Getenv("CLIAMP_SOCKET")}
	if cli.socket == "" {
		cli.socket = ipc.DefaultSocketPath()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	found, err := cli.findInPlaylist(ctx, "crow", 10)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("total=%v matches=%d", found["playlist_total"], len(found["matches"].([]map[string]any)))
	for _, track := range found["matches"].([]map[string]any) {
		t.Logf("%v %v — %v", track["index"], track["artist"], track["title"])
	}
}
