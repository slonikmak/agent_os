package materials

import (
	"golang.org/x/sys/unix"
	"os"
)

func alias(f *os.File) bool {
	var finder [32]byte
	n, err := unix.Fgetxattr(int(f.Fd()), "com.apple.FinderInfo", finder[:])
	if err == unix.ENOATTR {
		return false
	}
	if err != nil {
		return true
	}
	return n < 10 || finder[8]&0x80 != 0
}
