// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
)

const (
	// This salt is not meant to be kept secret (it’s checked in after all). It’s
	// a tiny bit of paranoia to avoid whatever problems a collision may cause.
	salt = "Go playground salt\n"

	maxSnippetSize = 64 * 1024
)

type snippet struct {
	// Body []byte `datastore:",noindex"` // golang.org/issues/23253
	Body []byte
}

func (s *snippet) ID() string {
	h := sha256.New()
	io.WriteString(h, salt)
	h.Write(s.Body)
	sum := h.Sum(nil)
	b := make([]byte, base64.URLEncoding.EncodedLen(len(sum)))
	base64.URLEncoding.Encode(b, sum)
	// Web sites don’t always linkify a trailing underscore, making it seem like
	// the link is broken. If there is an underscore at the end of the substring,
	// extend it until there is not.
	hashLen := 11
	for hashLen <= len(b) && b[hashLen-1] == '_' {
		hashLen++
	}
	return string(b)[:hashLen]
}

func (s *server) handleShare(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if !allowShare(r) {
		w.WriteHeader(http.StatusUnavailableForLegalReasons)
		w.Write([]byte(`<h1>Unavailable For Legal Reasons</h1><p>Viewing and/or sharing code snippets is not available in your country for legal reasons. This message might also appear if your country is misdetected. If you believe this is an error, please <a href="https://golang.org/issue">file an issue</a>.</p>`))
		return
	}

	if r.Method == "OPTIONS" {
		// This is likely a pre-flight CORS request.
		return
	}

	// GET Method to get origin snippet
	if r.Method == "GET" {
		params := r.URL.Query()
		id := params.Get("id")

		snip := &snippet{Body: []byte("")}

		if err := s.db.GetSnippet(r.Context(), id, snip); err != nil {
			if err == ErrNoSuchEntity {
				http.Error(w, "Not found", http.StatusNotFound)
				return
			}
			s.log.Errorf("getting Snippet: %v", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, string(snip.Body))
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Requires POST", http.StatusMethodNotAllowed)
		return
	}

	var body bytes.Buffer
	_, err := io.Copy(&body, io.LimitReader(r.Body, maxSnippetSize+1))
	r.Body.Close()
	if err != nil {
		s.log.Errorf("reading Body: %v", err)
		http.Error(w, "Server Error", http.StatusInternalServerError)
		return
	}
	if body.Len() > maxSnippetSize {
		http.Error(w, "Snippet is too large", http.StatusRequestEntityTooLarge)
		return
	}

	snip := &snippet{Body: body.Bytes()}
	id := snip.ID()
	if err := s.db.PutSnippet(r.Context(), id, snip); err != nil {
		s.log.Errorf("putting Snippet: %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	fmt.Fprint(w, id)
}

func allowShare(r *http.Request) bool {
	// if r.Header.Get("X-AppEngine-Country") == "CN" {
	// 	return false
	// }
	return true
}
