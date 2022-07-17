// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// examplesHandler serves example content out of the examples directory.
type examplesHandler struct {
	modtime  time.Time
	examples []example
}

type example struct {
	Title   string
	Path    string
	Content string
}

func (h *examplesHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	for _, e := range h.examples {
		if e.Path == req.URL.Path {
			http.ServeContent(w, req, e.Path, h.modtime, strings.NewReader(e.Content))
			return
		}
	}
	http.NotFound(w, req)
}

// hello returns the hello text for this instance, which depends on the Go version.
func (h *examplesHandler) hello() string {
	return h.examples[0].Content
}

// newExamplesHandler reads from the examples directory, returning a handler to
// serve their content.
//
// Examples must start with a line beginning "// Title:" that sets their title.
//
// modtime is used for content caching headers.
func newExamplesHandler(modtime time.Time) (*examplesHandler, error) {
	const dir = "examples"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var examples []example
	for _, entry := range entries {
		name := entry.Name()

		// Read examples ending in .txt
		prefix := "" // if non-empty, this is a relevant example file
		if strings.HasSuffix(name, ".txt") {
			prefix = strings.TrimSuffix(name, ".txt")
		}

		if prefix == "" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		content := string(data)

		// Extract the magic "// Title:" comment specifying the example's title.
		nl := strings.IndexByte(content, '\n')
		const titlePrefix = "// Title:"
		if nl == -1 || !strings.HasPrefix(content, titlePrefix) {
			return nil, fmt.Errorf("malformed example for %q: must start with a title line beginning %q", name, titlePrefix)
		}
		title := strings.TrimPrefix(content[:nl], titlePrefix)
		title = strings.TrimSpace(title)

		examples = append(examples, example{
			Title:   title,
			Path:    name,
			Content: content[nl+1:],
		})
	}

	// Sort by title, before prepending the hello example (we always want Hello
	// to be first).
	sort.Slice(examples, func(i, j int) bool {
		return examples[i].Title < examples[j].Title
	})

	examples = append([]example{
		{"Hello, 世界！", "hello.txt", hello},
	}, examples...)
	return &examplesHandler{
		modtime:  modtime,
		examples: examples,
	}, nil
}

const hello = `package main

import (
	"fmt"
)

func main() {
	fmt.Println("Hello, 世界")
}
`
