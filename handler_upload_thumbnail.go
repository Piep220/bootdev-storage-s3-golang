package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
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

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	const maxMemory = 10 << 20
	r.ParseMultipartForm(maxMemory)

	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	fileType := header.Header.Get("Content-Type")
	if fileType != "image/png" && fileType != "image/jpeg" {
		respondWithError(w, http.StatusBadRequest, "Unsupported file type. Only PNG and JPEG are allowed.", nil)
		return
	}


	videoMetadata, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to get video metadata", err)
		return
	}
	if videoMetadata.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "You do not have permission to upload thumbnail for this video", nil)
		return
	}

	
	fileExtension := "png"
	if fileType == "image/jpeg" {
		fileExtension = "jpg"
	}
	saveLocation := filepath.Join(cfg.assetsRoot, fmt.Sprintf("%s.%s", videoID.String(), fileExtension))
	newFile, err := os.Create(saveLocation)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create file on server", err)
		return
	}
	defer newFile.Close()

	_, err = io.Copy(newFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to save file on server", err)
		return
	}

	dataURL := fmt.Sprintf("http://localhost:%s/assets/%s.%s", cfg.port, videoID.String(), fileExtension)
	
	updatedVideo := database.Video{
		ID:           videoMetadata.ID,
		CreatedAt:    videoMetadata.CreatedAt,
		UpdatedAt:    time.Now(),
		ThumbnailURL: &dataURL,
		VideoURL:     videoMetadata.VideoURL,
		CreateVideoParams: videoMetadata.CreateVideoParams,
	}
	err = cfg.db.UpdateVideo(updatedVideo)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update video metadata with thumbnail URL", err)
		return
	}

	respondWithJSON(w, http.StatusOK, videoMetadata)
}