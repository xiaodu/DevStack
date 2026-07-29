package tx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type Compensator struct{}

func NewCompensator() *Compensator {
	return &Compensator{}
}

func (c *Compensator) DeleteFile(path string) StepFunc {
	return func(ctx context.Context) error {
		if err := os.Remove(path); err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("delete file %s: %w", path, err)
		}
		return nil
	}
}

func (c *Compensator) DeleteDir(path string) StepFunc {
	return func(ctx context.Context) error {
		if err := os.RemoveAll(path); err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("delete dir %s: %w", path, err)
		}
		return nil
	}
}

func (c *Compensator) RenameFile(src, dest string) StepFunc {
	return func(ctx context.Context) error {
		if err := os.Rename(src, dest); err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("rename %s -> %s: %w", src, dest, err)
		}
		return nil
	}
}

func (c *Compensator) RestoreBackup(backupPath, targetPath string) StepFunc {
	return func(ctx context.Context) error {
		if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove target %s: %w", targetPath, err)
		}
		if err := os.Rename(backupPath, targetPath); err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("restore backup %s -> %s: %w", backupPath, targetPath, err)
		}
		return nil
	}
}

func (c *Compensator) CleanTempFiles(pattern string) StepFunc {
	return func(ctx context.Context) error {
		files, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("glob %s: %w", pattern, err)
		}
		for _, f := range files {
			if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("delete temp file %s: %w", f, err)
			}
		}
		return nil
	}
}

func (c *Compensator) ReleasePort(port int, portPool interface{ Release(port int) error }) StepFunc {
	return func(ctx context.Context) error {
		return portPool.Release(port)
	}
}

func (c *Compensator) Noop() StepFunc {
	return func(ctx context.Context) error {
		return nil
	}
}