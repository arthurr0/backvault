//go:build !unix

package files

import (
	"archive/tar"
	"os"
)

func deviceOf(info os.FileInfo) (uint64, bool) { return 0, false }

func hardlinkKey(info os.FileInfo) (string, bool) { return "", false }

func fillOwner(hdr *tar.Header, info os.FileInfo) {}
