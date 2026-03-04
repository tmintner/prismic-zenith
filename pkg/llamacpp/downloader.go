package llamacpp

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ProgressTracker wraps an io.Writer to log progress of the download
type ProgressTracker struct {
	Total      uint64
	Downloaded uint64
	LastLog    time.Time
}

func (pt *ProgressTracker) Write(p []byte) (int, error) {
	n := len(p)
	pt.Downloaded += uint64(n)

	// Log progress every 3 seconds
	if time.Since(pt.LastLog) > 3*time.Second {
		pt.LastLog = time.Now()
		if pt.Total > 0 {
			percent := float64(pt.Downloaded) / float64(pt.Total) * 100
			log.Printf("Downloading model: %.2f%% (%d / %d bytes)", percent, pt.Downloaded, pt.Total)
		} else {
			log.Printf("Downloading model: %d bytes...", pt.Downloaded)
		}
	}
	return n, nil
}

// DownloadModel downloads a model from a URL to the specified destination path.
// It will not download if the file already exists.
func DownloadModel(url, destPath string) error {
	// First check if it already exists
	if info, err := os.Stat(destPath); err == nil && info.Size() > 0 {
		return nil
	}

	log.Printf("Model not found at %s. Proceeding to auto-download from %s...", destPath, url)

	// Create directory if needed
	err := os.MkdirAll(filepath.Dir(destPath), 0755)
	if err != nil {
		return fmt.Errorf("failed to create directory for model: %v", err)
	}

	// Create temporary file to download into
	tmpPath := destPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}
	defer out.Close()

	// Download the file
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download model: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status connecting to %s: %s", url, resp.Status)
	}

	totalSize := uint64(resp.ContentLength)
	tracker := &ProgressTracker{
		Total:   totalSize,
		LastLog: time.Now(),
	}

	// Wrap writer with progress tracking
	tee := io.TeeReader(resp.Body, tracker)

	_, err = io.Copy(out, tee)
	if err != nil {
		os.Remove(tmpPath) // clean up on error
		return fmt.Errorf("failed during download copy: %v", err)
	}

	// Close the file so we can rename
	out.Close()

	// Rename temp to final
	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("failed to rename temp file: %v", err)
	}

	log.Printf("Successfully downloaded model to %s", destPath)
	return nil
}

// DefaultModelURL is the HuggingFace URL for the default model (TinyLlama-1.1B)
const DefaultModelURL = "https://huggingface.co/TheBloke/TinyLlama-1.1B-Chat-v1.0-GGUF/resolve/main/tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf"

// EnsureModel checks if the model exists at localModelPath.
// If not, it downloads the default model to that path.
// Returns an error if the model isn't there and couldn't be downloaded.
func EnsureModel(localModelPath string) error {
	if info, err := os.Stat(localModelPath); err == nil && info.Size() > 0 {
		return nil // Extant
	}

	log.Printf("Model missing: auto-downloading default TinyLlama GGUF...")
	return DownloadModel(DefaultModelURL, localModelPath)
}
