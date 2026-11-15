// SPDX-License-Identifier: Apache-2.0
package pluginsdk

// CatalogIndex is data published by a registry, not plugin implementation code.
// Developers keep executable sources/releases in their own repositories.
type CatalogSource struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}
type CatalogAsset struct {
	Manifest string `json:"manifest_asset"`
	SHA256   string `json:"manifest_sha256"`
	Binary   string `json:"binary_asset"`
}
type CatalogEntry struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Repository  string                  `json:"repository"`
	Version     string                  `json:"version"`
	Tag         string                  `json:"tag"`
	Assets      map[string]CatalogAsset `json:"assets"`
}
type CatalogIndex struct {
	Version string         `json:"schema_version"`
	Plugins []CatalogEntry `json:"plugins"`
}
type CatalogMatch struct {
	CatalogEntry
	Source     string `json:"source"`
	Compatible bool   `json:"compatible"`
}
type CatalogFailure struct {
	Source string `json:"source"`
	Code   string `json:"code"`
}
type CatalogSearch struct {
	Truncated bool             `json:"truncated,omitempty"`
	Plugins   []CatalogMatch   `json:"plugins"`
	Failures  []CatalogFailure `json:"failures"`
}
type CatalogOffer struct {
	Entry    CatalogMatch `json:"entry"`
	Manifest Manifest     `json:"manifest"`
	SHA256   string       `json:"sha256"`
}
type InstallStatus struct {
	ID       string  `json:"id"`
	Plugin   string  `json:"plugin"`
	Source   string  `json:"source"`
	State    string  `json:"state"`
	Progress float64 `json:"progress"`
	Bytes    int64   `json:"bytes"`
	Error    *Error  `json:"error,omitempty"`
}
