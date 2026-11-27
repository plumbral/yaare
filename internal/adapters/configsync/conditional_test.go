package configsync

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestWebDAVPublicationPresentsTheCollectionLockForNewChildren(t *testing.T) {
	const token = "<opaquelocktoken:test>"
	const contents = "configuration"
	var published atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		root := "http://" + r.Host + "/team%20files/"
		switch r.Method {
		case "LOCK":
			w.Header().Set("Lock-Token", token)
		case "UNLOCK":
			if r.Header.Get("Lock-Token") != token {
				t.Error("wrong unlock token")
			}
		case "HEAD":
			if !published.Load() {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("ETag", `"saved"`)
		case "PUT", "MOVE":
			if r.Header.Get("If") != "<"+root+"> ("+token+")" {
				w.WriteHeader(http.StatusPreconditionFailed)
				return
			}
			if r.Method == "MOVE" {
				if r.Header.Get("Destination") != root+CurrentObject || r.Header.Get("Overwrite") != "F" {
					t.Error("unexpected publication destination or overwrite policy")
				}
				published.Store(true)
			}
			w.WriteHeader(http.StatusCreated)
		case "GET":
			_, _ = io.WriteString(w, contents)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()
	client, err := NewHTTP(Config{Provider: "webdav", Endpoint: server.URL, Prefix: "team files"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if tag, err := client.Put(t.Context(), CurrentObject, []byte(contents), ""); err != nil || tag != `"saved"` {
		t.Fatal("publish with a recursive collection lock", tag, err)
	}
}

func TestConditionalCurrentGet(t *testing.T) {
	const validator = `"verified"`
	for _, provider := range []string{"webdav", "s3"} {
		for _, test := range []struct {
			name          string
			status        int
			responseTag   string
			fallback      int
			wantUnchanged bool
			wantErr       error
		}{
			{"matching", 304, validator, 0, true, nil},
			{"changed", 200, `"different"`, 0, false, nil},
			{"condition ignored", 200, validator, 0, false, nil},
			{"missing", 404, "", 0, false, ErrMissing},
			{"no tag", 304, "", 200, false, nil},
			{"wrong tag", 304, `"different"`, 200, false, nil},
			{"weak tag", 304, `W/"verified"`, 200, false, nil},
			{"fallback missing", 304, "", 404, false, ErrMissing},
			{"unsolicited 304", 304, "", 304, false, errNotModified},
		} {
			t.Run(provider+"/"+test.name, func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					status, tag, expected := test.status, test.responseTag, validator
					if calls.Add(1) > 1 {
						status, tag, expected = test.fallback, `"fallback"`, ""
					}
					if status == 0 {
						t.Error("unexpected fallback request")
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/current.yaaresync") || r.Header.Get("If-None-Match") != expected {
						t.Errorf("unexpected GET or validator: %s %s %q", r.Method, r.URL.Path, r.Header.Get("If-None-Match"))
					}
					if provider == "s3" && strings.Contains(r.Header.Get("Authorization"), ";if-none-match;") != (expected != "") {
						t.Error("conditional header missing from S3 signature or signed on fallback")
					}
					if tag != "" {
						w.Header().Set("ETag", tag)
					}
					w.WriteHeader(status)
					if status == http.StatusOK {
						_, _ = io.WriteString(w, "encrypted bytes")
					}
				}))
				defer server.Close()
				client, err := NewHTTP(Config{Provider: provider, Endpoint: server.URL, Bucket: "bucket", Region: "us-east-1"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer client.Close()
				blob, tag, unchanged, err := client.GetCurrentIfChanged(t.Context(), validator)
				if !errors.Is(err, test.wantErr) || unchanged != test.wantUnchanged {
					t.Fatalf("unexpected outcome: tag=%q unchanged=%v err=%v", tag, unchanged, err)
				}
				expectedCalls := int32(1)
				if test.fallback != 0 {
					expectedCalls++
				}
				if calls.Load() != expectedCalls {
					t.Fatalf("got %d requests, want %d", calls.Load(), expectedCalls)
				}
				if unchanged && (blob != nil || tag != validator) || !unchanged && err == nil && string(blob) != "encrypted bytes" {
					t.Fatalf("incorrect read result: %q, %q", blob, tag)
				}
			})
		}
	}
}

func TestSharedTransportReusesConnectionWithRequestCredentials(t *testing.T) {
	for _, provider := range []string{"webdav", "s3"} {
		t.Run(provider, func(t *testing.T) {
			var accepted atomic.Int32
			var requests atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wantUser := "first"
				if requests.Add(1) == 2 {
					wantUser = "second"
				}
				if provider == "s3" {
					if !strings.Contains(r.Header.Get("Authorization"), "Credential="+wantUser+"/") {
						t.Error("S3 request reused earlier credentials")
					}
				} else if user, password, ok := r.BasicAuth(); !ok || user != wantUser || password != wantUser+"-secret" {
					t.Error("WebDAV request reused earlier credentials")
				}
				w.Header().Set("ETag", `"fixture"`)
				_, _ = io.WriteString(w, "content")
			}))
			server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					accepted.Add(1)
				}
			}
			server.Start()
			defer server.Close()
			transport := NewTransport()
			defer transport.CloseIdleConnections()
			for _, user := range []string{"first", "second"} {
				client, err := NewHTTPWithTransport(Config{Provider: provider, Endpoint: server.URL, Username: user, Bucket: "bucket", Region: "us-east-1"}, []byte(user+"-secret"), transport)
				if err != nil {
					t.Fatal(err)
				}
				_, _, err = client.Get(t.Context(), "current.yaaresync")
				_ = client.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			if accepted.Load() != 1 {
				t.Fatalf("expected one connection across both handles, got %d", accepted.Load())
			}
		})
	}
}
