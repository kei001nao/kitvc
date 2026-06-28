package library

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func WriteAudioTags(path string, tags map[string]string) error {
	ext := strings.ToLower(filepath.Ext(path))
	tmpPath := path + ".tmp"

	args := []string{"-i", path, "-map", "0", "-codec", "copy"}
	for k, v := range tags {
		args = append(args, "-metadata", fmt.Sprintf("%s=%s", k, v))
	}
	if ext == ".mp3" {
		args = append(args, "-write_id3v2", "1")
	}
	args = append(args, "-y", tmpPath)

	cmd := exec.Command("ffmpeg", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("ffmpeg failed for %s: %s: %w", path, string(out), err)
	}

	if err := replaceFile(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename failed for %s: %w", path, err)
	}

	return nil
}

func replaceFile(src, dst string) error {
	bak := dst + ".bak"
	os.Remove(bak)

	hasBak := false
	if err := os.Rename(dst, bak); err == nil {
		hasBak = true
	} else if !os.IsNotExist(err) {
		// If dst exists but rename to bak failed, try remove directly
		if removeErr := os.Remove(dst); removeErr != nil {
			return fmt.Errorf("failed to remove target file %s: %w", dst, err)
		}
	}

	if err := os.Rename(src, dst); err != nil {
		if hasBak {
			os.Rename(bak, dst)
		}
		return err
	}

	if hasBak {
		os.Remove(bak)
	}
	return nil
}
