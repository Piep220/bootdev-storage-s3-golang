package main

import (
	"os/exec"
)

func processVideoForFastStart(filepath string) (string, error) {
	filepathOutput := filepath + ".processing"
	cmd := exec.Command(
		"ffmpeg",
		"-i", filepath,
		"-c", "copy",
		"-movflags", "faststart",
		"-f", "mp4",
		filepathOutput,
	)
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return filepathOutput, nil
}