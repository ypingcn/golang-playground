// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"html/template"
	"net/http"
	"runtime"
)

var editTemplate = template.Must(template.ParseFiles("edit.html"))

type editData struct {
	Snippet   *snippet
	GoVersion string
	Examples  []example
}

func (s *server) handleEdit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == "OPTIONS" {
		// This is likely a pre-flight CORS request.
		return
	}

	snip := &snippet{Body: []byte(s.examples.hello())}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := &editData{
		Snippet:   snip,
		GoVersion: runtime.Version(),
		Examples:  s.examples.examples,
	}
	if err := editTemplate.Execute(w, data); err != nil {
		s.log.Errorf("editTemplate.Execute(w, %+v): %v", data, err)
		return
	}
}
