//go:build unix

package files

import (
	"archive/tar"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

func deviceOf(info os.FileInfo) (uint64, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(st.Dev), true
}

func hardlinkKey(info os.FileInfo) (string, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Nlink < 2 {
		return "", false
	}
	return strconv.FormatUint(uint64(st.Dev), 10) + ":" + strconv.FormatUint(uint64(st.Ino), 10), true
}

func fillOwner(hdr *tar.Header, info os.FileInfo) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return
	}
	hdr.Uid = int(st.Uid)
	hdr.Gid = int(st.Gid)
	if hdr.Uname == "" {
		if u, err := user.LookupId(strconv.FormatUint(uint64(st.Uid), 10)); err == nil {
			hdr.Uname = u.Username
		}
	}
	if hdr.Gname == "" {
		if g, err := user.LookupGroupId(strconv.FormatUint(uint64(st.Gid), 10)); err == nil {
			hdr.Gname = g.Name
		}
	}
}
