package ipc

import (
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"yaare/internal/adapters/configsync"
)

const storageTestSecret = "Yaare-Storage-Test-Only!"
const storageTestPassword = "Yaare-Storage-Encryption-Test!"

func fixtureS3(t *testing.T, interrupt func(http.ResponseWriter, *http.Request, http.Handler)) configsync.Config {
	t.Helper()
	const bucket = "fixture-bucket"
	type objectInfo struct {
		Key  string
		Size int
	}
	objects := map[string][]byte{}
	var mu sync.Mutex
	tagFor := func(data []byte) string { return fmt.Sprintf("%q", fmt.Sprintf("%x", sha256.Sum256(data))) }
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		key := strings.TrimPrefix(r.URL.Path, "/"+bucket+"/")
		data, exists := objects[key]
		switch r.Method {
		case http.MethodHead:
			if !exists {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("ETag", tagFor(data))
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			if r.URL.Query().Get("list-type") == "2" {
				result := struct {
					XMLName  xml.Name `xml:"ListBucketResult"`
					Contents []objectInfo
				}{}
				for name, blob := range objects {
					if strings.HasPrefix(name, r.URL.Query().Get("prefix")) {
						result.Contents = append(result.Contents, objectInfo{name, len(blob)})
					}
				}
				_ = xml.NewEncoder(w).Encode(result)
				return
			}
			if !exists {
				http.NotFound(w, r)
				return
			}
			etag := tagFor(data)
			w.Header().Set("ETag", etag)
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			_, _ = w.Write(data)
		case http.MethodPut:
			match := r.Header.Get("If-Match")
			if exists && r.Header.Get("If-None-Match") == "*" || match != "" && (!exists || tagFor(data) != match) {
				w.WriteHeader(http.StatusPreconditionFailed)
				return
			}
			blob, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			objects[key] = blob
			w.Header().Set("ETag", tagFor(blob))
		case http.MethodDelete:
			delete(objects, key)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if interrupt == nil {
			handler.ServeHTTP(w, r)
			return
		}
		interrupt(w, r, handler)
	}))
	t.Cleanup(server.Close)
	return configsync.Config{Provider: "s3", Endpoint: server.URL, Username: "fixture-access", Bucket: bucket, Region: "us-east-1"}
}
