// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"net/http"
	"os"
)

var log = newStdLogger()

var (
	runtests   = flag.Bool("runtests", false, "Run integration tests instead of Playground server.")
	backendURL = flag.String("backend-url", "", "URL for sandbox backend that runs Go binaries.")
)

func main() {
	flag.Parse()
	s, err := newServer(func(s *server) error {
		s.db = &inMemStore{}
		if caddr := os.Getenv("MEMCACHED_ADDR"); caddr != "" {
			s.cache = newGobCache(caddr)
			log.Printf("Use Memcached caching results")
		} else {
			s.cache = (*gobCache)(nil) // Use a no-op cache implementation.
			log.Printf("NOT caching calc results")
		}
		s.log = log
		execpath, _ := os.Executable()
		if execpath != "" {
			if fi, _ := os.Stat(execpath); fi != nil {
				s.modtime = fi.ModTime()
			}
		}
		eh, err := newExamplesHandler(s.modtime)
		if err != nil {
			return err
		}
		s.examples = eh
		return nil
	})
	if err != nil {
		log.Fatalf("Error creating server: %v", err)
	}

	if *runtests {
		s.test()
		return
	}
	if *backendURL != "" {
		// TODO(golang.org/issue/25224) - Remove environment variable and use a flag.
		os.Setenv("SANDBOX_BACKEND_URL", *backendURL)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Get the backend dialer warmed up. This starts
	// RegionInstanceGroupDialer queries and health checks.
	go sandboxBackendClient()

	log.Printf("Listening on :%v ...", port)
	log.Fatalf("Error listening on :%v: %v", port, http.ListenAndServe(":"+port, s))
}
