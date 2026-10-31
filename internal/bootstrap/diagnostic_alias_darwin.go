package bootstrap

import (
	"golang.org/x/sys/unix"
	"os"
)

func diagnosticAlias(f *os.File) bool {
	var finder [32]byte
	n, err := unix.Fgetxattr(int(f.Fd()), "com.apple.FinderInfo", finder[:])
	if err == unix.ENOATTR {
		return false
	}
	return err != nil || n < 10 || finder[8]&0x80 != 0
}
