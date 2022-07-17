// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package metrics provides a service for reporting metrics to
// Stackdriver, or locally during development.
package metrics

import (
	"fmt"
	"net/http"
	"time"

	"contrib.go.opencensus.io/exporter/prometheus"
	"contrib.go.opencensus.io/exporter/stackdriver"
	"go.opencensus.io/stats/view"
	mrpb "google.golang.org/genproto/googleapis/api/monitoredres"
)

// NewService initializes a *Service.
//
// The Service returned is configured to send metric data to
// StackDriver. When not running on GCE, it will host metrics through
// a prometheus HTTP handler.
//
// views will be passed to view.Register for export to the metric
// service.
func NewService(resource *MonitoredResource, views []*view.View) (*Service, error) {
	err := view.Register(views...)
	if err != nil {
		return nil, err
	}
	view.SetReportingPeriod(5 * time.Second)
	pe, err := prometheus.NewExporter(prometheus.Options{})
	if err != nil {
		return nil, fmt.Errorf("prometheus.NewExporter: %w", err)
	}
	view.RegisterExporter(pe)
	return &Service{pExporter: pe}, nil
}

// Service controls metric exporters.
type Service struct {
	sdExporter *stackdriver.Exporter
	pExporter  *prometheus.Exporter
}

func (m *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if m.pExporter != nil {
		m.pExporter.ServeHTTP(w, r)
		return
	}
	http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
}

// Stop flushes metrics and stops exporting. Stop should be called
// before exiting.
func (m *Service) Stop() {
	if sde := m.sdExporter; sde != nil {
		// Flush any unsent data before exiting.
		sde.Flush()

		sde.StopMetricsExporter()
	}
}

// MonitoredResource wraps a *mrpb.MonitoredResource to implement the
// monitoredresource.MonitoredResource interface.
type MonitoredResource mrpb.MonitoredResource

func (r *MonitoredResource) MonitoredResource() (resType string, labels map[string]string) {
	return r.Type, r.Labels
}
