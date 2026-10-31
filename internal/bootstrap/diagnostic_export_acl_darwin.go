//go:build darwin && cgo

package bootstrap

/*
#include <sys/acl.h>

// POSIX mode bits do not mask macOS extended allow ACLs. Clear every inherited
// entry on the retained inode before creating children or writing any bytes.
static int clear_diagnostic_export_acl(int fd) {
 acl_t empty = acl_init(0);
 if (empty == NULL) return -1;
 int result = acl_set_fd_np(fd, empty, ACL_TYPE_EXTENDED);
 acl_free(empty);
 return result;
}
*/
import "C"

import "os"

func privatizeDiagnosticExport(file *os.File) error {
	if C.clear_diagnostic_export_acl(C.int(file.Fd())) != 0 {
		return errPrivate
	}
	return nil
}
