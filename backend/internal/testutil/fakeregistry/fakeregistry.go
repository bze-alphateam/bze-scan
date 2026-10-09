// Package fakeregistry is a stand-in for the Cosmos chain registry: an
// httptest server answering the GitHub contents API's directory listings
// under APIURL and the raw files under RawURL from recorded chain.json and
// assetlist.json files (testdata/<chain>/, recorded by
// scripts/record-registry-fixtures.sh). The root listing names every
// recorded chain, the directories AddDirs adds, a testnets directory and an
// _IBC directory, as the real one does; testnets/ lists nothing.
package fakeregistry

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

//go:embed testdata
var testdata embed.FS

// Registry is a running fake registry.
type Registry struct {
	// APIURL is the contents API base (the DefaultAPIURL stand-in).
	APIURL string
	// RawURL is the raw files base (the DefaultRawURL stand-in).
	RawURL string

	files fs.FS
	mu    sync.Mutex
	extra []string
	down  bool
	reqs  map[string]int
}

// New starts a fake registry and stops it when the test ends.
func New(t testing.TB) *Registry {
	t.Helper()
	files, err := fs.Sub(testdata, "testdata")
	if err != nil {
		t.Fatalf("fakeregistry: %v", err)
	}
	r := &Registry{files: files, reqs: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(r.serve))
	t.Cleanup(srv.Close)
	r.APIURL, r.RawURL = srv.URL+"/api", srv.URL+"/raw"
	return r
}

// AddDirs lists more chain directories, which hold no files.
func (r *Registry) AddDirs(dirs ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.extra = append(r.extra, dirs...)
}

// SetDown makes every request fail with 503 (true) or answer again (false).
func (r *Registry) SetDown(down bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.down = down
}

// Requests returns how many requests path (/api/…, /raw/<chain>/chain.json)
// received.
func (r *Registry) Requests(path string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reqs[path]
}

// Total returns how many requests the registry received.
func (r *Registry) Total() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.reqs {
		n += c
	}
	return n
}

func (r *Registry) serve(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.reqs[req.URL.Path]++
	down, extra := r.down, slices.Clone(r.extra)
	r.mu.Unlock()
	if down {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	switch {
	case req.URL.Path == "/api/" || req.URL.Path == "/api":
		dirs, _ := fs.ReadDir(r.files, ".")
		names := append(extra, "testnets", "_IBC")
		for _, d := range dirs {
			names = append(names, d.Name())
		}
		listing(w, names)
	case req.URL.Path == "/api/testnets":
		listing(w, nil)
	case strings.HasPrefix(req.URL.Path, "/raw/"):
		body, err := fs.ReadFile(r.files, strings.TrimPrefix(req.URL.Path, "/raw/"))
		if err != nil {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(body)
	default:
		http.NotFound(w, req)
	}
}

// listing answers a contents API listing of directories, with a file
// thrown in as the real root has.
func listing(w http.ResponseWriter, dirs []string) {
	type entry struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
	}
	out := []entry{{Name: "README.md", Path: "README.md", Type: "file"}}
	slices.Sort(dirs)
	for _, d := range dirs {
		out = append(out, entry{Name: d, Path: d, Type: "dir"})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
