package main

import (
	"os"
	"time"

	"github.com/rwcarlsen/goexif/exif"
)

// extractMetadata extracts EXIF and other metadata from media file
func extractMetadata(mf *MediaFile) {
	switch mf.Type {
	case TypePhoto:
		extractPhotoMetadata(mf)
	case TypeVideo, TypeMusic:
		// TODO: Add video/music metadata extraction
		fallbackToFileTime(mf)
	default:
		fallbackToFileTime(mf)
	}

	// Fallback to file modification time if no date found
	if mf.DateTaken == nil {
		fallbackToFileTime(mf)
	}
}

// extractPhotoMetadata extracts EXIF data from photos
func extractPhotoMetadata(mf *MediaFile) {
	f, err := os.Open(mf.Path)
	if err != nil {
		return
	}
	defer f.Close()

	x, err := exif.Decode(f)
	if err != nil {
		// No EXIF data or decode failed - will use file time fallback
		return
	}

	// Extract date - try DateTime first (works for most cameras)
	if tm, err := x.DateTime(); err == nil {
		mf.DateTaken = &tm
	}

	// Extract camera make
	if make, err := x.Get(exif.Make); err == nil {
		if makeStr, err := make.StringVal(); err == nil {
			mf.CameraMake = makeStr
		}
	}

	// Extract camera model
	if model, err := x.Get(exif.Model); err == nil {
		if modelStr, err := model.StringVal(); err == nil {
			mf.CameraModel = modelStr
		}
	}

	// Extract dimensions
	if width, err := x.Get(exif.PixelXDimension); err == nil {
		if w, err := width.Int(0); err == nil {
			mf.Width = w
		}
	}

	if height, err := x.Get(exif.PixelYDimension); err == nil {
		if h, err := height.Int(0); err == nil {
			mf.Height = h
		}
	}
}

// fallbackToFileTime uses file modification time as fallback
func fallbackToFileTime(mf *MediaFile) {
	if mf.DateTaken != nil {
		return
	}

	info, err := os.Stat(mf.Path)
	if err == nil {
		modTime := info.ModTime()
		mf.DateTaken = &modTime
	} else {
		// Ultimate fallback to current time
		now := time.Now()
		mf.DateTaken = &now
	}
}
