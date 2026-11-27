package webserver

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"yaare/internal/adapters/cloudsync"
	"yaare/internal/adapters/configsync"
)

type objectStore struct {
	db    *sql.DB
	slots chan struct{}
}

func openObjects(root string) (*objectStore, error) {
	db, e := sql.Open("sqlite", filepath.Join(root, "sync-v2.sqlite"))
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	fail := func(err error) (*objectStore, error) { db.Close(); return nil, err }
	tx, e := db.Begin()
	if e != nil {
		return fail(e)
	}
	defer tx.Rollback()
	var version int
	if e = tx.QueryRow("PRAGMA user_version").Scan(&version); e == nil && version == 0 {
		var tables int
		e = tx.QueryRow("SELECT count(*) FROM sqlite_master WHERE name NOT LIKE 'sqlite_%'").Scan(&tables)
		if e == nil && tables != 0 {
			e = errors.New("无法识别同步数据库。")
		}
		if e == nil {
			_, e = tx.Exec("CREATE TABLE objects(name TEXT PRIMARY KEY, body BLOB NOT NULL, tag TEXT NOT NULL, created INTEGER NOT NULL); CREATE TABLE devices(id TEXT PRIMARY KEY,digest BLOB NOT NULL UNIQUE); PRAGMA user_version=1")
		}
	} else if e == nil && version != 1 {
		e = errors.New("同步数据库版本不受支持。")
	}
	if e == nil {
		_, e = tx.Exec("SELECT name,body,tag,created FROM objects LIMIT 0; SELECT id,digest FROM devices LIMIT 0")
	}
	if e != nil {
		tx.Rollback()
		return fail(e)
	}
	if e = tx.Commit(); e != nil {
		return fail(e)
	}
	return &objectStore{db: db, slots: make(chan struct{}, 4)}, nil
}
func (o *objectStore) authenticated(r *http.Request) bool {
	value, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || len(value) != 43 {
		return false
	}
	digest := sha256.Sum256([]byte(value))
	var one int
	return o.db.QueryRowContext(r.Context(), "SELECT 1 FROM devices WHERE digest=?", digest[:]).Scan(&one) == nil
}
func (s *Server) devices(w http.ResponseWriter, r *http.Request) {
	_, session := s.auth.session(r)
	if session == nil || (r.Method != "GET" && (!s.sameOrigin(r) || r.Header.Get("X-Yaare-CSRF") != session.csrf)) {
		w.WriteHeader(403)
		return
	}
	ctx := r.Context()
	db := s.objects.db
	switch r.Method {
	case "GET":
		rows, e := db.QueryContext(ctx, "SELECT id FROM devices ORDER BY rowid")
		if e != nil {
			w.WriteHeader(503)
			return
		}
		defer rows.Close()
		ids := []string{}
		for rows.Next() {
			var id string
			if rows.Scan(&id) != nil {
				w.WriteHeader(503)
				return
			}
			ids = append(ids, id)
		}
		if rows.Err() != nil {
			w.WriteHeader(503)
			return
		}
		writeJSON(w, ids)
	case "POST":
		id, token := randomToken(), randomToken()
		hash := sha256.Sum256([]byte(token))
		result, e := db.ExecContext(ctx, "INSERT INTO devices SELECT ?,? WHERE (SELECT count(*) FROM devices)<64", id, hash[:])
		if e != nil {
			w.WriteHeader(503)
			return
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			w.WriteHeader(409)
			return
		}
		writeJSON(w, map[string]string{"id": id, "token": token})
	case "DELETE":
		var q struct{ ID string }
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 512)).Decode(&q) != nil || len(q.ID) != 43 {
			w.WriteHeader(400)
			return
		}
		if _, e := db.ExecContext(ctx, "DELETE FROM devices WHERE id=?", q.ID); e != nil {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}
func (o *objectStore) serve(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/sync/v2/")
	ctx := r.Context()
	if name == "backups" && r.Method == "GET" {
		rows, e := o.db.QueryContext(ctx, "SELECT name,length(body) FROM objects WHERE name LIKE 'backups/%' ORDER BY name DESC")
		if e != nil {
			w.WriteHeader(503)
			return
		}
		defer rows.Close()
		out := []configsync.Object{}
		for rows.Next() {
			var item configsync.Object
			if rows.Scan(&item.Name, &item.Size) != nil {
				w.WriteHeader(503)
				return
			}
			if configsync.ValidBackupName(item.Name) {
				out = append(out, item)
			}
		}
		if rows.Err() != nil {
			w.WriteHeader(503)
			return
		}
		writeJSON(w, out)
		return
	}
	if !configsync.ValidName(name) {
		w.WriteHeader(404)
		return
	}
	select {
	case o.slots <- struct{}{}:
		defer func() { <-o.slots }()
	default:
		w.WriteHeader(503)
		return
	}
	switch r.Method {
	case "GET", "HEAD":
		var tag string
		var body []byte
		tx, e := o.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if e != nil {
			w.WriteHeader(503)
			return
		}
		defer tx.Rollback()
		e = tx.QueryRowContext(ctx, "SELECT tag FROM objects WHERE name=?", name).Scan(&tag)
		if errors.Is(e, sql.ErrNoRows) {
			w.WriteHeader(404)
			return
		}
		if e != nil {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("ETag", tag)
		if r.Header.Get("If-None-Match") == tag {
			w.WriteHeader(304)
			return
		}
		if r.Method == "HEAD" {
			return
		}
		if tx.QueryRowContext(ctx, "SELECT body FROM objects WHERE name=?", name).Scan(&body) != nil {
			w.WriteHeader(503)
			return
		}
		tx.Rollback()
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(body)
	case "PUT":
		limit := int64(cloudsync.MaxBlob)
		probe := strings.HasPrefix(name, "probe-")
		if probe {
			limit = 256
		}
		body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
		if e != nil {
			w.WriteHeader(413)
			return
		}
		if !probe && !cloudsync.ValidEnvelope(body) {
			w.WriteHeader(400)
			return
		}
		create := r.Header.Get("If-None-Match") == "*"
		previous := r.Header.Get("If-Match")
		if create == (previous != "") || !create && strings.HasPrefix(name, "backups/") {
			w.WriteHeader(412)
			return
		}
		tag := "\"" + randomToken() + "\""
		status := o.put(ctx, name, tag, previous, body, create)
		if status == 204 {
			w.Header().Set("ETag", tag)
		}
		w.WriteHeader(status)
	case "DELETE":
		if name == configsync.CurrentObject {
			w.WriteHeader(405)
			return
		}
		if _, e := o.db.ExecContext(ctx, "DELETE FROM objects WHERE name=?", name); e != nil {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}
func (o *objectStore) put(ctx context.Context, name, tag, previous string, body []byte, create bool) int {
	tx, e := o.db.BeginTx(ctx, nil)
	if e != nil {
		return 503
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	if _, e = tx.ExecContext(ctx, "DELETE FROM objects WHERE name LIKE 'probe-%' AND created<?", now-3600); e != nil {
		return 503
	}
	var res sql.Result
	if create {
		var count int
		if tx.QueryRowContext(ctx, "SELECT count(*) FROM objects").Scan(&count) != nil {
			return 503
		}
		if count >= 512 {
			return 507
		}
		res, e = tx.ExecContext(ctx, "INSERT INTO objects(name,tag,body,created) VALUES(?,?,?,?) ON CONFLICT(name) DO NOTHING", name, tag, body, now)
	} else {
		res, e = tx.ExecContext(ctx, "UPDATE objects SET body=?,tag=?,created=? WHERE name=? AND tag=?", body, tag, now, name, previous)
	}
	if e != nil {
		return 503
	}
	n, e := res.RowsAffected()
	if e != nil {
		return 503
	}
	if n != 1 {
		return 412
	}
	if tx.Commit() != nil {
		return 503
	}
	return 204
}
