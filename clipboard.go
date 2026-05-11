package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type ClipboardItem struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type ClipboardStore struct {
	Items []ClipboardItem `json:"items"`
}

var (
	clipboardMu    sync.RWMutex
	clipboardFile  = "clipboard_items.json"
	clipboardStore = &ClipboardStore{Items: []ClipboardItem{}}
)

func init() {
	loadClipboardItems()
}

func loadClipboardItems() {
	clipboardMu.Lock()
	defer clipboardMu.Unlock()

	data, err := os.ReadFile(clipboardFile)
	if err != nil {
		if os.IsNotExist(err) {
			clipboardStore.Items = []ClipboardItem{}
			return
		}
		log.Printf("Error reading clipboard file: %v", err)
		return
	}

	if err := json.Unmarshal(data, clipboardStore); err != nil {
		log.Printf("Error parsing clipboard file: %v", err)
		clipboardStore.Items = []ClipboardItem{}
	}
}

func saveClipboardItems() error {
	data, err := json.MarshalIndent(clipboardStore, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling clipboard data: %w", err)
	}

	dir := filepath.Dir(clipboardFile)
	tmp := filepath.Join(dir, filepath.Base(clipboardFile)+".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("error writing clipboard temp file: %w", err)
	}
	if err := os.Rename(tmp, clipboardFile); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("error replacing clipboard file: %w", err)
	}

	return nil
}

func handleClipboardTool(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(w, "clipboard.html", nil); err != nil {
		log.Printf("Template rendering error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func handleClipboardAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleGetClipboardItems(w, r)
	case http.MethodPost:
		handleAddClipboardItem(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleClipboardItemAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/clipboard/")
	if path == "" {
		respondJSON(w, http.StatusBadRequest, false, "Item ID required", "")
		return
	}

	parts := strings.Split(path, "/")
	id := parts[0]
	if id == "" {
		respondJSON(w, http.StatusBadRequest, false, "Item ID required", "")
		return
	}

	switch {
	case len(parts) == 1 && r.Method == http.MethodDelete:
		handleDeleteClipboardItem(w, r, id)
	case len(parts) == 2 && parts[1] == "move" && r.Method == http.MethodPatch:
		handleMoveClipboardItem(w, r, id)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleGetClipboardItems(w http.ResponseWriter, r *http.Request) {
	clipboardMu.RLock()
	defer clipboardMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"items":   clipboardStore.Items,
	})
}

type AddClipboardItemRequest struct {
	Label   string `json:"label"`
	Content string `json:"content"`
}

func handleAddClipboardItem(w http.ResponseWriter, r *http.Request) {
	var req AddClipboardItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, false, "Invalid JSON payload", "")
		return
	}

	if req.Content == "" {
		respondJSON(w, http.StatusBadRequest, false, "Content is required", "")
		return
	}

	clipboardMu.Lock()
	defer clipboardMu.Unlock()

	item := ClipboardItem{
		ID:        uuid.New().String(),
		Label:     strings.TrimSpace(req.Label),
		Content:   req.Content,
		CreatedAt: time.Now(),
	}

	clipboardStore.Items = append([]ClipboardItem{item}, clipboardStore.Items...)

	if err := saveClipboardItems(); err != nil {
		log.Printf("Error saving clipboard: %v", err)
		respondJSON(w, http.StatusInternalServerError, false, "Failed to save item", "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"item":    item,
	})
}

func handleDeleteClipboardItem(w http.ResponseWriter, r *http.Request, itemID string) {
	clipboardMu.Lock()
	defer clipboardMu.Unlock()

	found := false
	newItems := make([]ClipboardItem, 0, len(clipboardStore.Items))
	for _, item := range clipboardStore.Items {
		if item.ID == itemID {
			found = true
			continue
		}
		newItems = append(newItems, item)
	}

	if !found {
		respondJSON(w, http.StatusNotFound, false, "Item not found", "")
		return
	}

	clipboardStore.Items = newItems

	if err := saveClipboardItems(); err != nil {
		log.Printf("Error saving clipboard: %v", err)
		respondJSON(w, http.StatusInternalServerError, false, "Failed to delete item", "")
		return
	}

	respondJSON(w, http.StatusOK, true, "", "Item deleted successfully")
}

type MoveClipboardItemRequest struct {
	Direction string `json:"direction"`
}

func handleMoveClipboardItem(w http.ResponseWriter, r *http.Request, itemID string) {
	var req MoveClipboardItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, false, "Invalid JSON payload", "")
		return
	}

	direction := strings.ToLower(strings.TrimSpace(req.Direction))
	if direction != "up" && direction != "down" {
		respondJSON(w, http.StatusBadRequest, false, "direction must be 'up' or 'down'", "")
		return
	}

	clipboardMu.Lock()
	defer clipboardMu.Unlock()

	idx := -1
	for i, item := range clipboardStore.Items {
		if item.ID == itemID {
			idx = i
			break
		}
	}
	if idx == -1 {
		respondJSON(w, http.StatusNotFound, false, "Item not found", "")
		return
	}

	target := idx - 1
	if direction == "down" {
		target = idx + 1
	}

	if target >= 0 && target < len(clipboardStore.Items) {
		clipboardStore.Items[idx], clipboardStore.Items[target] = clipboardStore.Items[target], clipboardStore.Items[idx]
		if err := saveClipboardItems(); err != nil {
			log.Printf("Error saving clipboard: %v", err)
			respondJSON(w, http.StatusInternalServerError, false, "Failed to reorder item", "")
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"items":   clipboardStore.Items,
	})
}
