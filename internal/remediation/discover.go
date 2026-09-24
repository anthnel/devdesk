package remediation

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"github.com/anthnel/devdesk/internal/dockerfile"
)

// Entry is one base image of one Dockerfile, and what it could become.
type Entry struct {
	// File is the Dockerfile's path relative to the repository, with forward
	// slashes.
	File string
	// Stage is the FROM as parsed, with the byte range that spells the image.
	Stage dockerfile.Stage
	// StageLabel is what the table calls the stage: its AS name, or its position.
	StageLabel string
	// Image is the reference the stage resolves to, and Candidates the full
	// references it could move to, best first. Reason says why there are none —
	// the reference could not be resolved, has no version to move from, or is
	// already on the newest tag of its line — and is empty when there are some.
	Image      string
	Candidates []string
	Reason     string
	// Floating is an image whose tag names content that changes in place
	// (Ref.Floats): it has no candidates, and a result measured earlier is
	// never taken as current (§3.79).
	Floating bool
}

// TagLister returns the tags a repository holds. Discover takes it as a
// function so that the walk, the parsing and the tag policy can be exercised
// without a registry.
type TagLister func(Ref) ([]string, error)

// Discover finds the Dockerfiles under root and, for each base image, the
// candidates the tag policy allows among the tags list returns.
//
// It never fails on one image: a registry that cannot be reached, or a
// reference that cannot be resolved, is that entry's Reason and the rest carry
// on. Only an unreadable root is an error. FROM scratch and FROM <an earlier
// stage> are not images and produce no entry.
func Discover(root string, list TagLister, track Track, max int) (entries []Entry, truncated bool, err error) {
	files, truncated, err := dockerfile.Find(root)
	if err != nil {
		return nil, false, err
	}

	tagsByRepo := map[string]tagResult{}
	for _, rel := range files {
		content, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if readErr != nil {
			log.Printf("ERROR [remediation] read %s: %v", rel, readErr)
			continue
		}
		for i, stage := range dockerfile.Parse(content).Stages {
			if stage.Kind != dockerfile.KindImage {
				continue
			}
			entry := Entry{File: rel, Stage: stage, StageLabel: stageLabel(stage, i), Image: stage.Image}
			if stage.Unresolved != "" {
				entry.Reason = stage.Unresolved
			} else {
				ref := ParseRef(stage.Image)
				entry.Floating = ref.Floats()
				entry.Candidates, entry.Reason = candidatesFor(ref, list, tagsByRepo, track, max)
			}
			entries = append(entries, entry)
		}
	}
	return entries, truncated, nil
}

func stageLabel(stage dockerfile.Stage, index int) string {
	if stage.Name != "" {
		return stage.Name
	}
	return "#" + strconv.Itoa(index+1)
}

type tagResult struct {
	tags []string
	err  error
}

// candidatesFor lists a repository's tags once per run — several stages, and
// several Dockerfiles, share a base — and applies the policy.
func candidatesFor(ref Ref, list TagLister, seen map[string]tagResult, track Track, max int) ([]string, string) {
	// A tag with no version to move from is refused before any request: the
	// answer does not depend on what the registry holds.
	switch version, _ := SplitTag(ref.Tag); {
	case ref.Floats():
		return nil, ReasonFloating
	case ref.Tag == "" && ref.Digest != "":
		return nil, ReasonPinnedByDigest
	case version == "":
		_, reason := Candidates(ref.Tag, nil, track, max)
		return nil, reason
	}

	key := ref.Registry + "/" + ref.Repository
	res, ok := seen[key]
	if !ok {
		res.tags, res.err = list(ref)
		seen[key] = res
	}
	if res.err != nil {
		log.Printf("ERROR [remediation] list tags of %s: %v", key, res.err)
		return nil, fmt.Sprintf("could not list tags of %s — check logs", ref.Name)
	}

	tags, reason := Candidates(ref.Tag, res.tags, track, max)
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		out = append(out, ref.WithTag(t))
	}
	return out, reason
}
