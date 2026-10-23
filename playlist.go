package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bjarneo/cliamp/ipc"
)

const maxText = 240

const (
	defaultWindow = 12
	maxWindow     = 40
	maxScan       = 2000
	maxHits       = 25
	scanWorkers   = 8
)

// tooLong reports the IPC frame limit. A playlist page that embeds cover art
// can exceed it; the caller then asks for fewer tracks.
func tooLong(err error) bool {
	return err != nil && strings.Contains(err.Error(), "token too long")
}

// unsupportedSearch reports a provider that has no catalog search. The loaded
// playlist is the list those callers can still filter.
func unsupportedSearch(err error) bool {
	return err != nil && strings.Contains(err.Error(), "does not support search")
}

// listOperation reports playlist reads that must be paged. One unbounded
// queue.list of a Telegram chat does not fit in an IPC frame.
func listOperation(name string) bool {
	switch name {
	case "queue.list", "playnext.list", "runtime.queue.list":
		return true
	}
	return false
}

// number reads a JSON number that arrived as float64.
func number(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}

// stripArt drops embedded covers. The model cannot use a data: URL, and one
// cover is enough to blow a tool result.
func stripArt(v any) any {
	switch value := v.(type) {
	case map[string]any:
		for key, item := range value {
			if key == "album_art_url" || key == "art" {
				if text, ok := item.(string); ok && text != "" {
					value[key] = "omitted"
				}
				continue
			}
			value[key] = stripArt(item)
		}
		return value
	case []any:
		for i, item := range value {
			value[i] = stripArt(item)
		}
		return value
	case string:
		if strings.HasPrefix(value, "data:") && len(value) > maxText {
			return fmt.Sprintf("omitted (%d bytes)", len(value))
		}
		if len(value) > maxText {
			return value[:maxText] + "…"
		}
		return value
	default:
		return v
	}
}

// matches reports whether query words all appear in the track's title, artist
// or album. Word order does not matter.
func matches(track map[string]any, query string) bool {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return true
	}
	blob := strings.ToLower(fmt.Sprint(track["title"], " ", track["artist"], " ", track["album"]))
	if strings.Contains(blob, query) {
		return true
	}
	for word := range strings.FieldsSeq(query) {
		if len(word) < 2 {
			continue
		}
		if !strings.Contains(blob, word) {
			return false
		}
	}
	return true
}

// resultOf waits for a submitted operation and returns its result object.
func (c client) resultOf(ctx context.Context, request ipc.V2Request) (map[string]any, error) {
	response, err := c.exchange(request, 15*time.Second)
	if err != nil {
		return nil, err
	}
	if response.Job != nil && !terminal(response.Job.State) {
		response, err = c.waitJob(ctx, response.Job.ID)
		if err != nil {
			return nil, err
		}
	}
	if response.Job != nil && (response.Job.State == ipc.JobFailed || response.Job.State == ipc.JobCanceled) {
		return nil, jobError(response.Job)
	}
	raw := json.RawMessage(`{}`)
	if response.Job != nil && len(response.Job.Result) > 0 {
		raw = response.Job.Result
	} else if len(response.Result) > 0 {
		raw = response.Result
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode result: %w", err)
	}
	if result == nil {
		result = map[string]any{}
	}
	return result, nil
}

// listPage reads one page. A page that does not fit is split until each
// request is a single track.
func (c client) listPage(ctx context.Context, operation string, offset, limit int) (map[string]any, error) {
	if limit < 1 {
		limit = 1
	}
	params, err := json.Marshal(map[string]int{"offset": offset, "limit": limit})
	if err != nil {
		return nil, err
	}
	result, err := c.resultOf(ctx, ipc.V2Request{
		ID:        id("list"),
		Method:    "operation.submit",
		Operation: operation,
		Params:    params,
	})
	if err == nil || limit == 1 || !tooLong(err) {
		return result, err
	}
	merged := map[string]any{}
	var tracks []any
	for i := 0; i < limit; i++ {
		page, pageErr := c.listPage(ctx, operation, offset+i, 1)
		if pageErr != nil {
			return nil, pageErr
		}
		if total, ok := number(page["total"]); ok {
			merged["total"] = total
		}
		if index, ok := number(page["index"]); ok {
			merged["index"] = index
		}
		for _, track := range tracksOf(page) {
			tracks = append(tracks, track)
		}
	}
	merged["ok"] = true
	merged["tracks"] = tracks
	return merged, nil
}

func tracksOf(result map[string]any) []map[string]any {
	raw, _ := result["tracks"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		track, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, track)
	}
	return out
}

// window reads a compact slice of a playlist. Covers are removed.
func (c client) window(ctx context.Context, operation string, offset, limit int) (map[string]any, error) {
	if limit < 1 {
		limit = defaultWindow
	}
	if limit > maxWindow {
		limit = maxWindow
	}
	page, err := c.listPage(ctx, operation, offset, limit)
	if err != nil {
		return nil, err
	}
	tracks := tracksOf(page)
	for i, track := range tracks {
		if _, ok := number(track["index"]); !ok {
			track["index"] = offset + i
		}
		stripArt(track)
	}
	out := map[string]any{
		"ok":     true,
		"offset": offset,
		"tracks": tracks,
	}
	if total, ok := number(page["total"]); ok {
		out["total"] = total
	}
	if index, ok := number(page["index"]); ok {
		out["current"] = index
	}
	return out, nil
}

// currentIndex reads the live playlist cursor from the runtime snapshot.
func (c client) currentIndex(ctx context.Context) (int, error) {
	response, err := c.exchange(ipc.V2Request{ID: id("state"), Method: "state.get"}, 5*time.Second)
	if err != nil {
		return 0, err
	}
	if response.Snapshot == nil {
		return 0, nil
	}
	if response.Snapshot.Track != nil && response.Snapshot.Track.Index > 0 {
		return response.Snapshot.Track.Index, nil
	}
	return response.Snapshot.Index, nil
}

// findInPlaylist filters the loaded playlist. Providers such as Telegram have
// no catalog search; the tracks already queued are the list to scan.
func (c client) findInPlaylist(ctx context.Context, query string, limit int) (map[string]any, error) {
	if limit < 1 || limit > maxHits {
		limit = maxHits
	}
	first, err := c.listPage(ctx, "queue.list", 0, 1)
	if err != nil {
		return nil, err
	}
	total, ok := number(first["total"])
	if !ok {
		total = len(tracksOf(first))
	}
	if total > maxScan {
		total = maxScan
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	hits := make([]map[string]any, 0)
	var mu sync.Mutex
	var failed error
	offsets := make(chan int)
	var wg sync.WaitGroup
	worker := func() {
		defer wg.Done()
		for offset := range offsets {
			page, pageErr := c.listPage(ctx, "queue.list", offset, 1)
			if pageErr != nil {
				mu.Lock()
				if failed == nil {
					failed = pageErr
				}
				mu.Unlock()
				continue
			}
			for i, track := range tracksOf(page) {
				if _, ok := number(track["index"]); !ok {
					track["index"] = offset + i
				}
				if !matches(track, query) {
					continue
				}
				stripArt(track)
				mu.Lock()
				hits = append(hits, track)
				mu.Unlock()
			}
		}
	}
	for range scanWorkers {
		wg.Add(1)
		go worker()
	}
	for offset := 0; offset < total; offset++ {
		select {
		case <-ctx.Done():
			close(offsets)
			wg.Wait()
			return nil, ctx.Err()
		case offsets <- offset:
		}
	}
	close(offsets)
	wg.Wait()
	if len(hits) == 0 && failed != nil {
		return nil, failed
	}
	sort.Slice(hits, func(i, j int) bool {
		ai, _ := number(hits[i]["index"])
		bi, _ := number(hits[j]["index"])
		return ai < bi
	})
	truncated := false
	if len(hits) > limit {
		hits = hits[:limit]
		truncated = true
	}
	return map[string]any{
		"source":         "live playlist",
		"query":          query,
		"playlist_total": total,
		"matches":        hits,
		"truncated":      truncated,
	}, nil
}
