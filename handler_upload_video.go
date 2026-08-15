package main

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/video"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	http.MaxBytesReader(w, r.Body, 1<<30)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)

	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	dbVideo, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't get video from database", err)
		return
	}

	if dbVideo.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "You don't have permission to upload a video for this video", nil)
		return
	}

	// Read the uploaded video file into memory.
	r.ParseMultipartForm(10 << 30) // 1GB
	file, _, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't get video file", err)
		return
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't read video file", err)
		return
	}

	contentType := http.DetectContentType(fileBytes)
	fileExtension, err := videoExtension(contentType)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid file type", err)
		return
	}

	// Prepare the video for streaming and pick an S3 key prefix based on its
	// aspect ratio.
	processedVideoPath, keyPrefix, err := processVideoForUpload(fileBytes, fileExtension)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't process video", err)
		return
	}
	defer os.Remove(processedVideoPath)

	processedVideoFile, err := os.Open(processedVideoPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't open processed video file", err)
		return
	}
	defer processedVideoFile.Close()

	// Store the video in S3 and save its reference in the database.
	videoURL, err := cfg.uploadVideoToS3(r.Context(), processedVideoFile, contentType, keyPrefix, fileExtension)
	if err != nil {
		fmt.Println(err)
		respondWithError(w, http.StatusInternalServerError, "Couldn't upload file to S3", err)
		return
	}

	dbVideo.VideoURL = &videoURL

	err = cfg.db.UpdateVideo(dbVideo)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video in database", err)
		return
	}

	signedURLVideo, err := cfg.dbVideoToSignedVideo(&dbVideo)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't generate signed URL for video", err)
		return
	}
	fmt.Print(signedURLVideo.VideoURL)
	respondWithJSON(w, http.StatusOK, signedURLVideo)
}

// videoExtension returns the file extension for the given content type.
func videoExtension(contentType string) (string, error) {
	switch contentType {
	case "video/mp4":
		return ".mp4", nil
	default:
		return "", fmt.Errorf("unsupported content type: %s", contentType)
	}
}

// processVideoForUpload writes the uploaded bytes to a temporary file, runs
// the fast-start processing, and returns the path to the processed file along
// with the S3 key prefix determined by the video's aspect ratio.
func processVideoForUpload(fileBytes []byte, fileExtension string) (string, string, error) {
	tempFile, err := os.CreateTemp("", "upload-*-video"+fileExtension)
	if err != nil {
		return "", "", fmt.Errorf("couldn't create temp file: %w", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	if _, err := tempFile.Write(fileBytes); err != nil {
		return "", "", fmt.Errorf("couldn't write file to disk: %w", err)
	}

	if _, err := tempFile.Seek(0, io.SeekStart); err != nil {
		return "", "", fmt.Errorf("couldn't seek file to beginning: %w", err)
	}

	processedPath, err := video.ProcessForFastStart(tempFile.Name())
	if err != nil {
		return "", "", fmt.Errorf("couldn't process video for fast start: %w", err)
	}

	aspectRatio, err := video.GetAspectRatio(processedPath)
	if err != nil {
		return "", "", fmt.Errorf("couldn't get aspect ratio: %w", err)
	}

	var keyPrefix string
	switch aspectRatio {
	case "16:9":
		keyPrefix = "landscape"
	case "9:16":
		keyPrefix = "portrait"
	default:
		keyPrefix = "other"
	}

	return processedPath, keyPrefix, nil
}
