// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package metrics_test

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	maxDescsPerCollector = 64
	maxMetricNames       = 256
)

// recordingRegisterer records the collectors registered through it and forwards operations to a registry.
type recordingRegisterer struct {
	registry   *prometheus.Registry
	mu         sync.Mutex
	collectors []prometheus.Collector
}

// newRecordingRegisterer creates a registerer that retains successfully registered collectors.
func newRecordingRegisterer(registry *prometheus.Registry) *recordingRegisterer {
	return &recordingRegisterer{registry: registry}
}

// Register forwards registration and retains the collector after success.
func (r *recordingRegisterer) Register(collector prometheus.Collector) error {
	if err := r.registry.Register(collector); err != nil {
		return err
	}
	r.mu.Lock()
	r.collectors = append(r.collectors, collector)
	r.mu.Unlock()
	return nil
}

// MustRegister forwards registration and retains collectors after success.
func (r *recordingRegisterer) MustRegister(collectors ...prometheus.Collector) {
	r.registry.MustRegister(collectors...)
	r.mu.Lock()
	r.collectors = append(r.collectors, collectors...)
	r.mu.Unlock()
}

// Unregister forwards removal to the wrapped registry.
func (r *recordingRegisterer) Unregister(collector prometheus.Collector) bool {
	return r.registry.Unregister(collector)
}

// metricNames returns every descriptor name retained from successful registrations.
func (r *recordingRegisterer) metricNames() (map[string]struct{}, error) {
	r.mu.Lock()
	collectors := append([]prometheus.Collector(nil), r.collectors...)
	r.mu.Unlock()
	if len(collectors) > maxMetricNames {
		return nil, fmt.Errorf("registerer retained more than %d collectors", maxMetricNames)
	}

	names := make(map[string]struct{})
	for i := 0; i < len(collectors) && i < maxMetricNames; i++ {
		descs := make(chan *prometheus.Desc, maxDescsPerCollector)
		collectors[i].Describe(descs)
		close(descs)
		for range maxDescsPerCollector {
			desc, ok := <-descs
			if !ok {
				break
			}
			name, err := descName(desc.String())
			if err != nil {
				return nil, fmt.Errorf("read descriptor name: %w", err)
			}
			names[name] = struct{}{}
		}
	}
	return names, nil
}

// descName parses the fqName from the stable leading portion of Desc.String.
func descName(value string) (string, error) {
	quoted, ok := strings.CutPrefix(value, "Desc{fqName: ")
	if !ok {
		return "", fmt.Errorf("descriptor %q lacks fqName prefix", value)
	}
	quoted, err := strconv.QuotedPrefix(quoted)
	if err != nil {
		return "", fmt.Errorf("descriptor fqName is not quoted: %w", err)
	}
	name, err := strconv.Unquote(quoted)
	if err != nil {
		return "", fmt.Errorf("unquote descriptor fqName: %w", err)
	}
	return name, nil
}

// compareMetricNameSets reports every declared name absent from gathering and every undeclared gathered name.
func compareMetricNameSets(declared, gathered map[string]struct{}) error {
	var differences []string
	count := 0
	for name := range declared {
		if count >= maxMetricNames {
			break
		}
		count++
		if _, ok := gathered[name]; !ok {
			differences = append(differences, fmt.Sprintf("missing gathered metric %q", name))
		}
	}
	count = 0
	for name := range gathered {
		if count >= maxMetricNames {
			break
		}
		count++
		if _, ok := declared[name]; !ok {
			differences = append(differences, fmt.Sprintf("unexpected gathered metric %q", name))
		}
	}
	if len(differences) == 0 {
		return nil
	}
	return fmt.Errorf("metric name sets differ: %s", strings.Join(differences, "; "))
}
