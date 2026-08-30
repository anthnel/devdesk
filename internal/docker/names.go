package docker

import "log"

// ImageNames names the images present on this machine, and says whether it
// could find out.
//
// The second return is the whole point. "Docker is not running" and "the image
// is gone" are the same silence from a caller's side, and reading the first as
// the second would empty every cache reader of its images the moment the daemon
// stops. A failed enumeration keeps everything instead: showing a target that no
// longer exists is a stale row, hiding one that does is a lie.
//
// It is a **var**, and that is the test seam three packages were each declaring
// for themselves. A test cannot pull an image, so without it a reconciliation
// could only be asserted by checking that a fixture happens to be absent — which
// it would pass for the wrong reason. Production never reassigns it.
var ImageNames = func() (map[string]struct{}, bool) {
	list, err := ListImages()
	if err != nil {
		log.Printf("INFO [docker] image list unavailable, keeping every cached image: %v", err)
		return nil, false
	}
	names := make(map[string]struct{}, len(list))
	for _, img := range list {
		names[img.Name()] = struct{}{}
	}
	return names, true
}
