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

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename failed for %s: %w", path, err)
	}

	return nil
}
