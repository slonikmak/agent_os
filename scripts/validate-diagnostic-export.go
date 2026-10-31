//go:build ignore

// Test-only archive inspection; this is not part of the native command surface.
package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	v1 "personaldesk/internal/contracts/v1"
)

func main() {
	for _, path := range os.Args[1:] {
		if err := validate(path); err != nil {
			fmt.Fprintln(os.Stderr, "Invalid diagnostic archive:", err)
			os.Exit(1)
		}
	}
	fmt.Printf("PASS frozen schema/budget validation: %d archives\n", len(os.Args)-1)
}
func validate(path string) error {
	z, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer z.Close()
	names := v1.ArchiveEntries()
	if len(z.File) != 3 {
		return fmt.Errorf("entry count")
	}
	var total uint64
	for i, f := range z.File {
		if f.Name != names[i] || !f.Mode().IsRegular() || f.Mode().Perm() != 0600 {
			return fmt.Errorf("entry policy")
		}
		total += f.UncompressedSize64
		if total > v1.MaxArchiveBytes {
			return fmt.Errorf("archive budget")
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(r, v1.MaxArchiveBytes+1))
		r.Close()
		if err != nil {
			return err
		}
		switch i {
		case 0:
			if _, err = v1.Validate("manifest", bytes.NewReader(data), v1.MaxDiagnosticRecordBytes); err != nil {
				return err
			}
		case 1:
			if len(data) > 0 && data[len(data)-1] != '\n' {
				return fmt.Errorf("partial log")
			}
			for _, line := range bytes.SplitAfter(data, []byte{'\n'}) {
				if len(line) == 0 {
					continue
				}
				if len(line) > v1.MaxDiagnosticRecordBytes {
					return fmt.Errorf("record budget")
				}
				if _, err = v1.Validate("diagnostic-event", bytes.NewReader(line), v1.MaxDiagnosticRecordBytes); err != nil {
					return err
				}
			}
		case 2:
			if len(data) != 0 {
				return fmt.Errorf("nonblank template")
			}
		}
	}
	return nil
}
