package video

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
)

func GetAspectRatio(filePath string) (string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	var out bytes.Buffer

	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("couldn't run ffprobe: %w", err)
	}

	var probe struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}

	if err := json.Unmarshal(out.Bytes(), &probe); err != nil {
		return "", fmt.Errorf("couldn't unmarshal ffprobe output: %w", err)
	}

	if len(probe.Streams) == 0 {
		return "", fmt.Errorf("no streams found in video")
	}

	width := probe.Streams[0].Width
	height := probe.Streams[0].Height

	const tolerance = 0.05
	ratio := float64(width) / float64(height)

	switch {
	case math.Abs(ratio-16.0/9.0) < tolerance:
		return "16:9", nil
	case math.Abs(ratio-9.0/16.0) < tolerance:
		return "9:16", nil
	default:
		return "other", nil
	}
}

func ProcessForFastStart(filePath string) (string, error) {
	outputFilePath := filePath + ".processing"

	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputFilePath)

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("couldn't run ffmpeg for fast start: %w", err)
	}

	return outputFilePath, nil
}
