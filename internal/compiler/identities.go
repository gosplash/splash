package compiler

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"path/filepath"
	"slices"
	"strings"
)

const (
	sourceIdentityPrefix   = "splash-source-v1:sha256:"
	analysisIdentityPrefix = "splash-analysis-v1:sha256:"

	// Bump when front-end semantics, import binding, or modeled call/effect
	// rules change. Source verification and backends have separate scopes.
	analysisRulesVersion = "2"
)

// AnalysisIdentity identifies captured source and the rules used to analyze it.
// These digests do not authenticate the source, results, or compiler.
type AnalysisIdentity struct {
	SourceID   string `json:"sourceId"`
	AnalysisID string `json:"analysisId"`
}

// Identity derives stable digests from captured inputs without reading files.
func (p *AnalyzedProgram) Identity() AnalysisIdentity {
	if p == nil || p.snapshot == nil {
		return AnalysisIdentity{}
	}
	a := p.snapshot
	a.identityOnce.Do(func() {
		type capturedFile struct {
			path, source string
		}
		files := make([]capturedFile, 0, len(a.sources))
		for path, source := range a.sources {
			files = append(files, capturedFile{a.sourcePath(path), source})
		}
		slices.SortFunc(files, func(a, b capturedFile) int {
			return strings.Compare(a.path, b.path)
		})
		sourceManifest := appendField(nil, sourceIdentityPrefix)
		for _, file := range files {
			sourceManifest = appendField(sourceManifest, file.path)
			digest := sha256.Sum256([]byte(file.source))
			sourceManifest = append(sourceManifest, digest[:]...)
		}
		sourceDigest := sha256.Sum256(sourceManifest)
		sourceID := sourceIdentityPrefix + hex.EncodeToString(sourceDigest[:])

		analysisManifest := appendField(nil, analysisIdentityPrefix)
		analysisManifest = appendField(analysisManifest, sourceID)
		analysisManifest = appendField(analysisManifest, a.sourcePath(a.entryPath))
		analysisManifest = appendField(analysisManifest, analysisRulesVersion)
		analysisDigest := sha256.Sum256(analysisManifest)
		a.identity = AnalysisIdentity{sourceID, analysisIdentityPrefix + hex.EncodeToString(analysisDigest[:])}
	})
	return a.identity
}

func (a *analysis) sourcePath(path string) string {
	if path == a.entryPath {
		name := filepath.Base(path)
		// The OS reads the entry spelling verbatim, while the loader cleans
		// import paths. A symlink followed by ".." can make those two paths
		// reach different files even when they clean to the same locator.
		if clean := filepath.Clean(path); clean != path {
			if _, captured := a.sources[clean]; captured {
				return "./" + name
			}
		}
		return name
	}
	// All captured paths share the entry directory as their lexical root.
	relative, _ := filepath.Rel(filepath.Dir(a.entryPath), path)
	return filepath.ToSlash(relative)
}

func appendField(manifest []byte, value string) []byte {
	manifest = binary.AppendUvarint(manifest, uint64(len(value)))
	return append(manifest, value...)
}
