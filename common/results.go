package common

import (
	"encoding/json"
	"sync"
)

type SectionStatus string

const (
	SectionPending  SectionStatus = "pending"
	SectionRunning  SectionStatus = "running"
	SectionComplete SectionStatus = "complete"

	// Section title constants shared across packages.
	SectionDNSInternal = "DNS Networking Tests (Internal)"
	SectionDNSExternal = "DNS Networking Tests (External)"
	SectionOverlay     = "Overlay Networking Tests"
	SectionNIC         = "NIC Information"
	SectionSpeedTest   = "Overlay Network Speed Tests"
)

// ResultRow is a single row of string-formatted values for HTML rendering.
type ResultRow []string

// TestSection holds the header, rows, and status for one test table.
type TestSection struct {
	Title   string        `json:"title"`
	Headers []string      `json:"headers"`
	Rows    []ResultRow   `json:"rows"`
	Status  SectionStatus `json:"status"`
}

// ResultsStore is a thread-safe container for all test section results.
type ResultsStore struct {
	mu          sync.RWMutex
	sections    []*TestSection
	allComplete bool
}

func NewResultsStore() *ResultsStore {
	return &ResultsStore{}
}

// AddSection registers a new named section with given column headers.
func (r *ResultsStore) AddSection(title string, headers []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sections = append(r.sections, &TestSection{
		Title:   title,
		Headers: headers,
		Rows:    []ResultRow{},
		Status:  SectionPending,
	})
}

// MarkRunning transitions a section to the running state.
func (r *ResultsStore) MarkRunning(title string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sections {
		if s.Title == title {
			s.Status = SectionRunning
			return
		}
	}
}

// MarkComplete transitions a section to the complete state.
func (r *ResultsStore) MarkComplete(title string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sections {
		if s.Title == title {
			s.Status = SectionComplete
			return
		}
	}
}

// AddRow appends a row to the named section.
func (r *ResultsStore) AddRow(title string, row ResultRow) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sections {
		if s.Title == title {
			s.Rows = append(s.Rows, row)
			return
		}
	}
}

// SetAllComplete marks the overall run as finished.
func (r *ResultsStore) SetAllComplete() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.allComplete = true
}

type resultsSnapshot struct {
	Sections    []*TestSection `json:"sections"`
	AllComplete bool           `json:"allComplete"`
}

// MarshalJSON serialises the store safely for the HTTP handler.
func (r *ResultsStore) MarshalJSON() ([]byte, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return json.Marshal(resultsSnapshot{
		Sections:    r.sections,
		AllComplete: r.allComplete,
	})
}
