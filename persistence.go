package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

// saveJSON atomically writes v as indented JSON to path.
// It writes to a sibling .tmp file first, then renames it, so a crash
// mid-write never leaves a corrupt file.
func saveJSON(path string, v interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// loadJSON reads JSON from path into v.
// Returns nil (not an error) when the file does not exist yet.
func loadJSON(path string, v interface{}) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(v)
}

// logPersistErr logs a non-fatal persistence failure.
func logPersistErr(resource string, err error) {
	if err != nil {
		log.Printf("persist [%s]: %v", resource, err)
	}
}
