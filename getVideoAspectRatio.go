package main

import (
	"os/exec"
	"bytes"
	"encoding/json"
	"fmt"
)

type ffprobeOutput struct {
	Streams []struct {
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		DisplayAspectRatio string `json:"display_aspect_ratio"`
	} `json:"streams"`
}

func getVideoAspectRatio(videoPath string) (string, error) {
	cmdReturn := exec.Command(
		"ffprobe", 
		"-v", "error", 
		"-print_format", "json", 
		"-show_streams", videoPath)
	
	buffer := &bytes.Buffer{}
	cmdReturn.Stdout = buffer
	err := cmdReturn.Run()
	if err != nil {
		return "", err
	}

	var output ffprobeOutput
	err = json.Unmarshal(buffer.Bytes(), &output)
	if err != nil {
		return "", err
	}

	if len(output.Streams) == 0 {
		return "", fmt.Errorf("no streams found in video")
	}

	if output.Streams[0].DisplayAspectRatio == "16:9" || output.Streams[0].DisplayAspectRatio == "9:16" {
		return output.Streams[0].DisplayAspectRatio, nil
	}

	// Aspect width:height 
	aspectErrorMargin := 0.02
	horizontalAspect := 16.0 / 9.0
	verticalAspect := 9.0 / 16.0
	aspect := float64(output.Streams[0].Width) / float64(output.Streams[0].Height)
	if (aspect > horizontalAspect - horizontalAspect * aspectErrorMargin ) && (aspect < horizontalAspect + horizontalAspect * aspectErrorMargin) {
		return "16:9", nil
	} else if (aspect > verticalAspect - verticalAspect * aspectErrorMargin) && (aspect < verticalAspect + verticalAspect * aspectErrorMargin) {
		return "9:16", nil
	} else {
		return "other", nil
	}

}